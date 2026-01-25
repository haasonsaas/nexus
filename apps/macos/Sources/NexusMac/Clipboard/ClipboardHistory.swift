import AppKit
import Foundation
import OSLog

/// Tracks clipboard history for AI agent context.
/// Provides access to recent clipboard contents.
@MainActor
@Observable
final class ClipboardHistory {
    static let shared = ClipboardHistory()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "clipboard.history")

    private(set) var history: [ClipboardEntry] = []
    private(set) var isTracking = false

    private var lastChangeCount: Int = 0
    private var pollTimer: Timer?

    let maxHistorySize = 50

    struct ClipboardEntry: Identifiable {
        let id: String
        let content: ClipboardContent
        let timestamp: Date
        let sourceApp: String?

        enum ClipboardContent {
            case text(String)
            case image(NSImage)
            case files([URL])
            case html(String)
            case rtf(Data)
            case unknown
        }

        var textPreview: String? {
            switch content {
            case .text(let text):
                return String(text.prefix(200))
            case .html(let html):
                return String(html.prefix(200))
            case .files(let urls):
                return urls.map { $0.lastPathComponent }.joined(separator: ", ")
            default:
                return nil
            }
        }
    }

    // MARK: - Tracking

    /// Start tracking clipboard changes
    func startTracking() {
        guard !isTracking else { return }

        lastChangeCount = NSPasteboard.general.changeCount
        pollTimer = Timer.scheduledTimer(withTimeInterval: 0.5, repeats: true) { [weak self] _ in
            Task { @MainActor in
                self?.checkClipboard()
            }
        }
        isTracking = true
        logger.info("clipboard tracking started")
    }

    /// Stop tracking clipboard changes
    func stopTracking() {
        pollTimer?.invalidate()
        pollTimer = nil
        isTracking = false
        logger.info("clipboard tracking stopped")
    }

    private func checkClipboard() {
        let pasteboard = NSPasteboard.general
        let currentCount = pasteboard.changeCount

        guard currentCount != lastChangeCount else { return }
        lastChangeCount = currentCount

        // Capture new clipboard content
        let entry = captureCurrentClipboard()
        if let entry {
            addEntry(entry)
        }
    }

    // MARK: - Capture

    /// Capture current clipboard content
    func captureCurrentClipboard() -> ClipboardEntry? {
        let pasteboard = NSPasteboard.general
        let types = pasteboard.types ?? []

        let content: ClipboardEntry.ClipboardContent

        if types.contains(.fileURL), let urls = pasteboard.readObjects(forClasses: [NSURL.self]) as? [URL], !urls.isEmpty {
            content = .files(urls)
        } else if types.contains(.png) || types.contains(.tiff),
                  let imageData = pasteboard.data(forType: .tiff),
                  let image = NSImage(data: imageData) {
            content = .image(image)
        } else if types.contains(.html), let html = pasteboard.string(forType: .html) {
            content = .html(html)
        } else if types.contains(.rtf), let rtfData = pasteboard.data(forType: .rtf) {
            content = .rtf(rtfData)
        } else if types.contains(.string), let text = pasteboard.string(forType: .string) {
            content = .text(text)
        } else {
            content = .unknown
        }

        // Get source app if available
        let sourceApp = NSWorkspace.shared.frontmostApplication?.localizedName

        return ClipboardEntry(
            id: UUID().uuidString,
            content: content,
            timestamp: Date(),
            sourceApp: sourceApp
        )
    }

    // MARK: - History Management

    private func addEntry(_ entry: ClipboardEntry) {
        // Check for duplicate text content
        if case .text(let newText) = entry.content {
            if let lastEntry = history.first, case .text(let lastText) = lastEntry.content, lastText == newText {
                return // Skip duplicate
            }
        }

        history.insert(entry, at: 0)

        // Trim to max size
        if history.count > maxHistorySize {
            history.removeLast()
        }

        logger.debug("clipboard entry added type=\(String(describing: entry.content))")
    }

    /// Clear clipboard history
    func clearHistory() {
        history.removeAll()
        logger.info("clipboard history cleared")
    }

    /// Remove specific entry
    func removeEntry(id: String) {
        history.removeAll { $0.id == id }
    }

    // MARK: - Paste Operations

    /// Paste a specific entry from history
    func paste(entryId: String) {
        guard let entry = history.first(where: { $0.id == entryId }) else { return }
        paste(content: entry.content)
    }

    /// Paste specific content to clipboard
    func paste(content: ClipboardEntry.ClipboardContent) {
        let pasteboard = NSPasteboard.general
        pasteboard.clearContents()

        switch content {
        case .text(let text):
            pasteboard.setString(text, forType: .string)
        case .image(let image):
            if let tiffData = image.tiffRepresentation {
                pasteboard.setData(tiffData, forType: .tiff)
            }
        case .files(let urls):
            pasteboard.writeObjects(urls as [NSURL])
        case .html(let html):
            pasteboard.setString(html, forType: .html)
        case .rtf(let data):
            pasteboard.setData(data, forType: .rtf)
        case .unknown:
            break
        }

        logger.debug("content pasted from history")
    }

    // MARK: - Search

    /// Search history for text
    func search(query: String) -> [ClipboardEntry] {
        let lowercased = query.lowercased()
        return history.filter { entry in
            switch entry.content {
            case .text(let text):
                return text.lowercased().contains(lowercased)
            case .html(let html):
                return html.lowercased().contains(lowercased)
            case .files(let urls):
                return urls.contains { $0.path.lowercased().contains(lowercased) }
            default:
                return false
            }
        }
    }

    /// Get text entries only
    func textEntries() -> [ClipboardEntry] {
        history.filter { entry in
            if case .text = entry.content { return true }
            return false
        }
    }
}
