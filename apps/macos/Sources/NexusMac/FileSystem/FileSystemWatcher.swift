import Foundation
import OSLog

/// Watches file system for changes relevant to AI agents.
/// Provides context about file modifications and project changes.
@MainActor
@Observable
final class FileSystemWatcher {
    static let shared = FileSystemWatcher()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "fs.watcher")

    private(set) var watchedPaths: [WatchedPath] = []
    private(set) var recentEvents: [FileEvent] = []
    private var streams: [String: FSEventStreamRef] = [:]

    var onFileEvent: ((FileEvent) -> Void)?

    struct WatchedPath: Identifiable {
        let id: String
        let path: String
        let recursive: Bool
        let filter: EventFilter?

        struct EventFilter {
            var includePatterns: [String]?
            var excludePatterns: [String]?
            var eventTypes: Set<FileEvent.EventType>?
        }
    }

    struct FileEvent: Identifiable {
        let id: String
        let path: String
        let eventType: EventType
        let timestamp: Date
        let flags: FSEventStreamEventFlags

        enum EventType: String {
            case created
            case modified
            case deleted
            case renamed
            case ownerChanged
            case unknown
        }
    }

    // MARK: - Watch Management

    /// Start watching a path
    func watch(path: String, recursive: Bool = true, filter: WatchedPath.EventFilter? = nil) {
        let watchId = UUID().uuidString
        let watched = WatchedPath(id: watchId, path: path, recursive: recursive, filter: filter)
        watchedPaths.append(watched)

        startStream(for: watched)
        logger.info("watching path=\(path) recursive=\(recursive)")
    }

    /// Stop watching a path
    func unwatch(path: String) {
        guard let index = watchedPaths.firstIndex(where: { $0.path == path }) else { return }
        let watched = watchedPaths[index]

        stopStream(for: watched.id)
        watchedPaths.remove(at: index)
        logger.info("unwatching path=\(path)")
    }

    /// Stop all watches
    func unwatchAll() {
        for watched in watchedPaths {
            stopStream(for: watched.id)
        }
        watchedPaths.removeAll()
        logger.info("all watches stopped")
    }

    // MARK: - Event Access

    /// Get events for a specific path
    func events(forPath path: String) -> [FileEvent] {
        recentEvents.filter { $0.path.hasPrefix(path) }
    }

    /// Get events of specific type
    func events(ofType type: FileEvent.EventType) -> [FileEvent] {
        recentEvents.filter { $0.eventType == type }
    }

    /// Clear event history
    func clearEvents() {
        recentEvents.removeAll()
    }

    // MARK: - Private

    private func startStream(for watched: WatchedPath) {
        let pathsToWatch = [watched.path] as CFArray

        var context = FSEventStreamContext()
        context.info = Unmanaged.passUnretained(self).toOpaque()

        let callback: FSEventStreamCallback = { streamRef, clientCallBackInfo, numEvents, eventPaths, eventFlags, eventIds in
            guard let info = clientCallBackInfo else { return }
            let watcher = Unmanaged<FileSystemWatcher>.fromOpaque(info).takeUnretainedValue()
            let paths = unsafeBitCast(eventPaths, to: NSArray.self)

            for i in 0..<numEvents {
                let path = paths[i] as! String
                let flags = eventFlags[i]

                Task { @MainActor in
                    watcher.handleEvent(path: path, flags: flags)
                }
            }
        }

        guard let stream = FSEventStreamCreate(
            nil,
            callback,
            &context,
            pathsToWatch,
            FSEventStreamEventId(kFSEventStreamEventIdSinceNow),
            1.0, // latency
            FSEventStreamCreateFlags(kFSEventStreamCreateFlagFileEvents | kFSEventStreamCreateFlagUseCFTypes)
        ) else {
            logger.error("failed to create FSEventStream for path=\(watched.path)")
            return
        }

        FSEventStreamSetDispatchQueue(stream, DispatchQueue.global())
        FSEventStreamStart(stream)
        streams[watched.id] = stream
    }

    private func stopStream(for watchId: String) {
        guard let stream = streams[watchId] else { return }
        FSEventStreamStop(stream)
        FSEventStreamInvalidate(stream)
        FSEventStreamRelease(stream)
        streams.removeValue(forKey: watchId)
    }

    private func handleEvent(path: String, flags: FSEventStreamEventFlags) {
        let eventType = parseEventType(flags)

        let event = FileEvent(
            id: UUID().uuidString,
            path: path,
            eventType: eventType,
            timestamp: Date(),
            flags: flags
        )

        // Check filters
        for watched in watchedPaths where path.hasPrefix(watched.path) {
            if let filter = watched.filter {
                // Check event type filter
                if let allowedTypes = filter.eventTypes, !allowedTypes.contains(eventType) {
                    continue
                }

                // Check exclude patterns
                if let excludes = filter.excludePatterns {
                    let shouldExclude = excludes.contains { pattern in
                        path.contains(pattern) || path.hasSuffix(pattern)
                    }
                    if shouldExclude { continue }
                }

                // Check include patterns
                if let includes = filter.includePatterns {
                    let shouldInclude = includes.contains { pattern in
                        path.contains(pattern) || path.hasSuffix(pattern)
                    }
                    if !shouldInclude { continue }
                }
            }

            // Event passed filters
            addEvent(event)
            return
        }

        // No filter matched, add if any watch covers the path
        if watchedPaths.contains(where: { path.hasPrefix($0.path) }) {
            addEvent(event)
        }
    }

    private func parseEventType(_ flags: FSEventStreamEventFlags) -> FileEvent.EventType {
        if flags & UInt32(kFSEventStreamEventFlagItemCreated) != 0 {
            return .created
        } else if flags & UInt32(kFSEventStreamEventFlagItemRemoved) != 0 {
            return .deleted
        } else if flags & UInt32(kFSEventStreamEventFlagItemRenamed) != 0 {
            return .renamed
        } else if flags & UInt32(kFSEventStreamEventFlagItemModified) != 0 {
            return .modified
        } else if flags & UInt32(kFSEventStreamEventFlagItemChangeOwner) != 0 {
            return .ownerChanged
        }
        return .unknown
    }

    private func addEvent(_ event: FileEvent) {
        // Keep last 500 events
        if recentEvents.count >= 500 {
            recentEvents.removeFirst()
        }
        recentEvents.append(event)
        onFileEvent?(event)
        logger.debug("file event type=\(event.eventType.rawValue) path=\(event.path)")
    }
}
