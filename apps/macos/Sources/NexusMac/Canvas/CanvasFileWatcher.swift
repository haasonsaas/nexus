import Foundation
import OSLog

/// FSEvents-based file watcher for canvas session directories.
/// Monitors file changes and triggers debounced reload notifications.
@MainActor
@Observable
final class CanvasFileWatcher {
    private let logger = Logger(subsystem: "com.nexus.mac", category: "canvas.watcher")

    private var stream: FSEventStreamRef?
    private var watchedPath: String?
    private var debounceTask: Task<Void, Never>?

    /// The debounce interval for file change notifications
    var debounceInterval: TimeInterval = 0.3

    /// Callback invoked when files change (after debouncing)
    var onFilesChanged: (() -> Void)?

    /// Whether the watcher is currently active
    private(set) var isWatching = false

    /// The path being watched
    var currentPath: String? { watchedPath }

    deinit {
        Task { @MainActor [weak self] in
            self?.stop()
        }
    }

    // MARK: - Watch Control

    /// Start watching a directory for changes
    /// - Parameter path: The directory path to watch
    func watch(path: String) {
        // Stop existing watch if any
        stop()

        watchedPath = path
        startStream(path: path)
        isWatching = true

        logger.info("started watching path=\(path)")
    }

    /// Stop watching the current directory
    func stop() {
        debounceTask?.cancel()
        debounceTask = nil

        if let stream {
            FSEventStreamStop(stream)
            FSEventStreamInvalidate(stream)
            FSEventStreamRelease(stream)
            self.stream = nil
        }

        if let path = watchedPath {
            logger.info("stopped watching path=\(path)")
        }

        watchedPath = nil
        isWatching = false
    }

    // MARK: - Private

    private func startStream(path: String) {
        let pathsToWatch = [path] as CFArray

        // Create context with reference to self
        var context = FSEventStreamContext()
        let unmanagedSelf = Unmanaged.passUnretained(self)
        context.info = unmanagedSelf.toOpaque()

        let callback: FSEventStreamCallback = { streamRef, clientCallBackInfo, numEvents, eventPaths, eventFlags, eventIds in
            guard let info = clientCallBackInfo else { return }
            let watcher = Unmanaged<CanvasFileWatcher>.fromOpaque(info).takeUnretainedValue()

            // Dispatch to main actor for processing
            Task { @MainActor in
                watcher.handleEvents(numEvents: numEvents, paths: eventPaths, flags: eventFlags)
            }
        }

        guard let stream = FSEventStreamCreate(
            nil,
            callback,
            &context,
            pathsToWatch,
            FSEventStreamEventId(kFSEventStreamEventIdSinceNow),
            debounceInterval / 2, // Use half the debounce interval as latency
            FSEventStreamCreateFlags(
                kFSEventStreamCreateFlagFileEvents |
                kFSEventStreamCreateFlagUseCFTypes |
                kFSEventStreamCreateFlagNoDefer
            )
        ) else {
            logger.error("failed to create FSEventStream for path=\(path)")
            return
        }

        FSEventStreamSetDispatchQueue(stream, DispatchQueue.global(qos: .utility))
        FSEventStreamStart(stream)
        self.stream = stream
    }

    private func handleEvents(numEvents: Int, paths: UnsafeMutableRawPointer, flags: UnsafePointer<FSEventStreamEventFlags>) {
        let pathArray = unsafeBitCast(paths, to: NSArray.self)

        var hasRelevantChanges = false

        for i in 0..<numEvents {
            let path = pathArray[i] as! String
            let flag = flags[i]

            // Filter out irrelevant events
            if shouldIgnore(path: path, flags: flag) {
                continue
            }

            logger.debug("file event path=\(path) flags=\(flag)")
            hasRelevantChanges = true
        }

        if hasRelevantChanges {
            scheduleNotification()
        }
    }

    private func shouldIgnore(path: String, flags: FSEventStreamEventFlags) -> Bool {
        // Ignore hidden files and directories
        let filename = (path as NSString).lastPathComponent
        if filename.hasPrefix(".") {
            return true
        }

        // Ignore scratch files
        if filename.hasSuffix("~") || filename.hasSuffix(".swp") || filename.hasSuffix(".tmp") {
            return true
        }

        // Ignore node_modules and other common build directories
        let ignoredDirs = ["node_modules", ".git", "__pycache__", ".cache", "dist", "build"]
        for dir in ignoredDirs {
            if path.contains("/\(dir)/") {
                return true
            }
        }

        // Ignore directory-only events (we care about file content)
        if flags & UInt32(kFSEventStreamEventFlagItemIsDir) != 0 {
            // Only ignore if it's not a removal
            if flags & UInt32(kFSEventStreamEventFlagItemRemoved) == 0 {
                return true
            }
        }

        return false
    }

    private func scheduleNotification() {
        // Cancel existing debounce task
        debounceTask?.cancel()

        // Schedule new debounced notification
        debounceTask = Task { @MainActor [weak self] in
            guard let self else { return }

            do {
                try await Task.sleep(for: .milliseconds(Int(self.debounceInterval * 1000)))

                // Check if task was cancelled during sleep
                try Task.checkCancellation()

                self.logger.debug("triggering reload after debounce")
                self.onFilesChanged?()
            } catch {
                // Task was cancelled, do nothing
            }
        }
    }
}

// MARK: - File Change Event

extension CanvasFileWatcher {
    /// Represents a file change event
    struct FileChangeEvent {
        let path: String
        let type: ChangeType
        let timestamp: Date

        enum ChangeType {
            case created
            case modified
            case deleted
            case renamed
        }
    }

    /// Parse event flags into a change type
    static func parseChangeType(from flags: FSEventStreamEventFlags) -> FileChangeEvent.ChangeType {
        if flags & UInt32(kFSEventStreamEventFlagItemCreated) != 0 {
            return .created
        } else if flags & UInt32(kFSEventStreamEventFlagItemRemoved) != 0 {
            return .deleted
        } else if flags & UInt32(kFSEventStreamEventFlagItemRenamed) != 0 {
            return .renamed
        } else {
            return .modified
        }
    }
}
