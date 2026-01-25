import AppKit
import OSLog
import SwiftUI

/// Injects active sessions into the menu bar menu.
/// Acts as a delegate wrapper to preserve SwiftUI's menu handling.
@MainActor
final class MenuSessionsInjector: NSObject, NSMenuDelegate {
    static let shared = MenuSessionsInjector()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "menu.injector")
    private let sessionTag = 9_415_557
    private let menuWidth: CGFloat = 320

    private weak var originalDelegate: NSMenuDelegate?
    private weak var statusItem: NSStatusItem?
    private var isMenuOpen = false

    private override init() {
        super.init()
    }

    /// Install the injector on a status item
    func install(into statusItem: NSStatusItem) {
        self.statusItem = statusItem
        guard let menu = statusItem.menu else { return }

        // Preserve SwiftUI's internal delegate
        if menu.delegate !== self {
            originalDelegate = menu.delegate
            menu.delegate = self
        }

        logger.debug("menu injector installed")
    }

    // MARK: - NSMenuDelegate

    func menuWillOpen(_ menu: NSMenu) {
        originalDelegate?.menuWillOpen?(menu)
        isMenuOpen = true

        // Inject sessions
        injectSessions(into: menu)
    }

    func menuDidClose(_ menu: NSMenu) {
        originalDelegate?.menuDidClose?(menu)
        isMenuOpen = false
    }

    func menuNeedsUpdate(_ menu: NSMenu) {
        originalDelegate?.menuNeedsUpdate?(menu)
    }

    // MARK: - Injection

    private func injectSessions(into menu: NSMenu) {
        // Remove any previous injected items
        for item in menu.items where item.tag == sessionTag {
            menu.removeItem(item)
        }

        // Find insert position (after first separator or at position 1)
        let insertIndex = findInsertIndex(in: menu)

        // Get active sessions
        let sessions = SessionBridge.shared.activeSessions.prefix(5)

        var cursor = insertIndex

        // Header
        let headerItem = makeHeaderItem()
        menu.insertItem(headerItem, at: cursor)
        cursor += 1

        if sessions.isEmpty {
            // No sessions message
            let emptyItem = makeMessageItem(text: "No active sessions", symbol: "minus")
            menu.insertItem(emptyItem, at: cursor)
            cursor += 1
        } else {
            // Session rows
            for session in sessions {
                let sessionItem = makeSessionItem(session)
                menu.insertItem(sessionItem, at: cursor)
                cursor += 1
            }
        }

        // Divider after sessions
        let divider = NSMenuItem.separator()
        divider.tag = sessionTag
        menu.insertItem(divider, at: cursor)
    }

    // MARK: - Menu Items

    private func makeHeaderItem() -> NSMenuItem {
        let item = NSMenuItem()
        item.tag = sessionTag
        item.isEnabled = false

        let view = NSHostingView(rootView: SessionMenuHeaderView(
            sessionCount: SessionBridge.shared.activeSessions.count,
            isConnected: ControlChannel.shared.isConnected
        ))
        view.frame.size.width = menuWidth
        let size = view.fittingSize
        view.frame = NSRect(origin: .zero, size: NSSize(width: menuWidth, height: size.height))
        item.view = view

        return item
    }

    private func makeSessionItem(_ session: SessionBridge.Session) -> NSMenuItem {
        let item = NSMenuItem()
        item.tag = sessionTag
        item.isEnabled = true
        item.target = self
        item.action = #selector(openSession(_:))
        item.representedObject = session.id

        let view = NSHostingView(rootView: SessionMenuRowView(session: session, width: menuWidth))
        view.frame.size.width = menuWidth
        let size = view.fittingSize
        view.frame = NSRect(origin: .zero, size: NSSize(width: menuWidth, height: size.height))
        item.view = view

        // Submenu with session actions
        item.submenu = makeSessionSubmenu(for: session)

        return item
    }

    private func makeMessageItem(text: String, symbol: String) -> NSMenuItem {
        let item = NSMenuItem()
        item.tag = sessionTag
        item.isEnabled = false

        let view = NSHostingView(rootView: HStack(spacing: 8) {
            Image(systemName: symbol)
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(text)
                .font(.caption)
                .foregroundStyle(.secondary)
            Spacer()
        }
        .padding(.horizontal, 18)
        .padding(.vertical, 6)
        .frame(width: menuWidth, alignment: .leading))

        view.frame.size.width = menuWidth
        let size = view.fittingSize
        view.frame = NSRect(origin: .zero, size: NSSize(width: menuWidth, height: size.height))
        item.view = view

        return item
    }

    private func makeSessionSubmenu(for session: SessionBridge.Session) -> NSMenu {
        let menu = NSMenu()

        let openItem = NSMenuItem(title: "Open Chat", action: #selector(openSession(_:)), keyEquivalent: "")
        openItem.target = self
        openItem.representedObject = session.id
        menu.addItem(openItem)

        menu.addItem(NSMenuItem.separator())

        let deleteItem = NSMenuItem(title: "Delete Session", action: #selector(deleteSession(_:)), keyEquivalent: "")
        deleteItem.target = self
        deleteItem.representedObject = session.id
        menu.addItem(deleteItem)

        return menu
    }

    // MARK: - Actions

    @objc private func openSession(_ sender: NSMenuItem) {
        guard let sessionId = sender.representedObject as? String else { return }
        WebChatManager.shared.openChat(for: sessionId)
        logger.debug("opened session: \(sessionId)")
    }

    @objc private func deleteSession(_ sender: NSMenuItem) {
        guard let sessionId = sender.representedObject as? String else { return }

        Task {
            await SessionBridge.shared.deleteSession(id: sessionId)
            logger.info("deleted session: \(sessionId)")
        }
    }

    // MARK: - Helpers

    private func findInsertIndex(in menu: NSMenu) -> Int {
        // Insert after the first separator, or at position 1
        if let sepIdx = menu.items.firstIndex(where: { $0.isSeparatorItem }) {
            return sepIdx + 1
        }
        return min(1, menu.items.count)
    }
}

// MARK: - Supporting Views

struct SessionMenuHeaderView: View {
    let sessionCount: Int
    let isConnected: Bool

    var body: some View {
        HStack {
            Image(systemName: "bubble.left.and.bubble.right")
                .font(.caption)
                .foregroundStyle(.secondary)

            Text("Sessions (\(sessionCount))")
                .font(.caption.weight(.semibold))
                .foregroundStyle(.secondary)

            Spacer()

            if !isConnected {
                Image(systemName: "bolt.slash")
                    .font(.caption)
                    .foregroundStyle(.orange)
            }
        }
        .padding(.horizontal, 18)
        .padding(.vertical, 6)
    }
}

struct SessionMenuRowView: View {
    let session: SessionBridge.Session
    let width: CGFloat

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: sessionIcon)
                .font(.caption)
                .foregroundStyle(.secondary)
                .frame(width: 16)

            VStack(alignment: .leading, spacing: 2) {
                Text(session.metadata.title ?? "Session")
                    .font(.callout)
                    .lineLimit(1)

                Text(session.lastActiveAt, style: .relative)
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            statusIndicator
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 6)
        .frame(width: width, alignment: .leading)
    }

    private var sessionIcon: String {
        switch session.type {
        case .chat: return "message"
        case .voice: return "mic"
        case .agent: return "cpu"
        case .computerUse: return "desktopcomputer"
        case .mcp: return "puzzlepiece"
        }
    }

    @ViewBuilder
    private var statusIndicator: some View {
        switch session.status {
        case .idle:
            Circle()
                .fill(.secondary.opacity(0.3))
                .frame(width: 6, height: 6)
        case .processing:
            Circle()
                .fill(.blue)
                .frame(width: 6, height: 6)
        case .waiting:
            Circle()
                .fill(.orange)
                .frame(width: 6, height: 6)
        case .error:
            Circle()
                .fill(.red)
                .frame(width: 6, height: 6)
        }
    }
}
