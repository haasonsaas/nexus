import AppKit
import Foundation
import OSLog

/// Manages context for AI agents.
/// Gathers and provides relevant context from the user's environment.
@MainActor
@Observable
final class ContextManager {
    static let shared = ContextManager()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "context")

    private(set) var currentContext: ContextSnapshot?
    private(set) var isGathering = false

    struct ContextSnapshot: Codable {
        var timestamp: Date
        var activeApp: AppContext?
        var frontmostWindow: WindowContext?
        var selection: SelectionContext?
        var clipboard: ClipboardContext?
        var screen: ScreenContext?
        var custom: [String: String]

        struct AppContext: Codable {
            let bundleId: String
            let name: String
            let isActive: Bool
        }

        struct WindowContext: Codable {
            let windowId: UInt32
            let title: String
            let ownerName: String
            let bounds: CGRect
        }

        struct SelectionContext: Codable {
            let text: String?
            let isFile: Bool
            let filePaths: [String]?
        }

        struct ClipboardContext: Codable {
            let hasText: Bool
            let textPreview: String?
            let hasImage: Bool
            let hasFiles: Bool
            let fileCount: Int
        }

        struct ScreenContext: Codable {
            let mainDisplayBounds: CGRect
            let displayCount: Int
            let mousePosition: CGPoint
        }
    }

    // MARK: - Context Gathering

    /// Gather full context snapshot
    func gatherContext() async -> ContextSnapshot {
        isGathering = true
        defer { isGathering = false }

        logger.debug("gathering context")

        let snapshot = ContextSnapshot(
            timestamp: Date(),
            activeApp: gatherAppContext(),
            frontmostWindow: gatherWindowContext(),
            selection: await gatherSelectionContext(),
            clipboard: gatherClipboardContext(),
            screen: gatherScreenContext(),
            custom: [:]
        )

        currentContext = snapshot
        return snapshot
    }

    /// Gather specific context type
    func gatherAppContext() -> ContextSnapshot.AppContext? {
        guard let frontApp = NSWorkspace.shared.frontmostApplication else { return nil }

        return ContextSnapshot.AppContext(
            bundleId: frontApp.bundleIdentifier ?? "unknown",
            name: frontApp.localizedName ?? "Unknown",
            isActive: frontApp.isActive
        )
    }

    func gatherWindowContext() -> ContextSnapshot.WindowContext? {
        let windows = ScreenCaptureService.shared.listWindows()
        guard let frontWindow = windows.first(where: { $0.layer == 0 }) else { return nil }

        return ContextSnapshot.WindowContext(
            windowId: frontWindow.windowID,
            title: frontWindow.title ?? "",
            ownerName: frontWindow.ownerName,
            bounds: frontWindow.bounds
        )
    }

    func gatherSelectionContext() async -> ContextSnapshot.SelectionContext? {
        // Try to get selected text via accessibility or clipboard
        let pasteboard = NSPasteboard.general

        // Check for file selection
        if let urls = pasteboard.readObjects(forClasses: [NSURL.self]) as? [URL], !urls.isEmpty {
            return ContextSnapshot.SelectionContext(
                text: nil,
                isFile: true,
                filePaths: urls.map { $0.path }
            )
        }

        // Check for text selection
        if let text = pasteboard.string(forType: .string) {
            return ContextSnapshot.SelectionContext(
                text: text,
                isFile: false,
                filePaths: nil
            )
        }

        return nil
    }

    func gatherClipboardContext() -> ContextSnapshot.ClipboardContext {
        let pasteboard = NSPasteboard.general
        let types = pasteboard.types ?? []

        var textPreview: String?
        if let text = pasteboard.string(forType: .string) {
            textPreview = String(text.prefix(200))
        }

        let files = pasteboard.readObjects(forClasses: [NSURL.self]) as? [URL] ?? []

        return ContextSnapshot.ClipboardContext(
            hasText: types.contains(.string),
            textPreview: textPreview,
            hasImage: types.contains(.tiff) || types.contains(.png),
            hasFiles: !files.isEmpty,
            fileCount: files.count
        )
    }

    func gatherScreenContext() -> ContextSnapshot.ScreenContext {
        let displays = ScreenCaptureService.shared.listDisplays()
        let mainDisplay = displays.first(where: { $0.isMain }) ?? displays.first

        return ContextSnapshot.ScreenContext(
            mainDisplayBounds: mainDisplay?.bounds ?? .zero,
            displayCount: displays.count,
            mousePosition: MouseController.shared.currentPosition()
        )
    }

    // MARK: - Context Enrichment

    /// Add custom context key-value
    func addCustomContext(key: String, value: String) {
        if currentContext == nil {
            currentContext = ContextSnapshot(
                timestamp: Date(),
                activeApp: nil,
                frontmostWindow: nil,
                selection: nil,
                clipboard: nil,
                screen: nil,
                custom: [:]
            )
        }
        currentContext?.custom[key] = value
    }

    /// Clear custom context
    func clearCustomContext() {
        currentContext?.custom.removeAll()
    }

    // MARK: - Serialization

    /// Export context as JSON for agent consumption
    func exportJSON() throws -> Data {
        guard let context = currentContext else {
            throw ContextError.noContext
        }

        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        return try encoder.encode(context)
    }

    /// Export context as markdown summary
    func exportMarkdown() -> String {
        guard let context = currentContext else {
            return "No context available"
        }

        var md = "# Context Snapshot\n\n"
        md += "**Timestamp:** \(context.timestamp)\n\n"

        if let app = context.activeApp {
            md += "## Active Application\n"
            md += "- **Name:** \(app.name)\n"
            md += "- **Bundle ID:** \(app.bundleId)\n\n"
        }

        if let window = context.frontmostWindow {
            md += "## Frontmost Window\n"
            md += "- **Title:** \(window.title)\n"
            md += "- **App:** \(window.ownerName)\n"
            md += "- **Size:** \(Int(window.bounds.width))x\(Int(window.bounds.height))\n\n"
        }

        if let clipboard = context.clipboard {
            md += "## Clipboard\n"
            md += "- **Has Text:** \(clipboard.hasText)\n"
            md += "- **Has Image:** \(clipboard.hasImage)\n"
            md += "- **Has Files:** \(clipboard.hasFiles) (\(clipboard.fileCount) files)\n"
            if let preview = clipboard.textPreview {
                md += "- **Preview:** \(preview.prefix(100))...\n"
            }
            md += "\n"
        }

        if let screen = context.screen {
            md += "## Screen\n"
            md += "- **Displays:** \(screen.displayCount)\n"
            md += "- **Main Display:** \(Int(screen.mainDisplayBounds.width))x\(Int(screen.mainDisplayBounds.height))\n"
            md += "- **Mouse Position:** (\(Int(screen.mousePosition.x)), \(Int(screen.mousePosition.y)))\n\n"
        }

        if !context.custom.isEmpty {
            md += "## Custom Context\n"
            for (key, value) in context.custom {
                md += "- **\(key):** \(value)\n"
            }
        }

        return md
    }
}

enum ContextError: LocalizedError {
    case noContext
    case gatherFailed(String)

    var errorDescription: String? {
        switch self {
        case .noContext:
            return "No context available"
        case .gatherFailed(let reason):
            return "Failed to gather context: \(reason)"
        }
    }
}
