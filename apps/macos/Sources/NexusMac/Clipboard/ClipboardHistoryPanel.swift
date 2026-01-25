import AppKit
import OSLog
import SwiftUI

/// Floating panel for clipboard history access.
@MainActor
final class ClipboardHistoryPanel {
    static let shared = ClipboardHistoryPanel()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "clipboard-panel")

    private var panel: NSPanel?

    private init() {}

    /// Show the clipboard history panel
    func show() {
        if panel == nil {
            createPanel()
        }

        guard let panel else { return }

        // Position at mouse location
        let mouseLocation = NSEvent.mouseLocation
        var frame = panel.frame
        frame.origin = NSPoint(
            x: mouseLocation.x - frame.width / 2,
            y: mouseLocation.y - frame.height - 20
        )

        // Ensure within screen bounds
        if let screen = NSScreen.main {
            let screenFrame = screen.visibleFrame
            if frame.minX < screenFrame.minX {
                frame.origin.x = screenFrame.minX + 10
            }
            if frame.maxX > screenFrame.maxX {
                frame.origin.x = screenFrame.maxX - frame.width - 10
            }
            if frame.minY < screenFrame.minY {
                frame.origin.y = screenFrame.minY + 10
            }
        }

        panel.setFrame(frame, display: true)
        panel.alphaValue = 0
        panel.orderFront(nil)
        panel.makeKey()

        NSAnimationContext.runAnimationGroup { context in
            context.duration = 0.2
            context.timingFunction = CAMediaTimingFunction(name: .easeOut)
            panel.animator().alphaValue = 1
        }

        logger.debug("clipboard history panel shown")
    }

    /// Hide the panel
    func hide() {
        guard let panel, panel.isVisible else { return }

        NSAnimationContext.runAnimationGroup { context in
            context.duration = 0.15
            context.timingFunction = CAMediaTimingFunction(name: .easeIn)
            panel.animator().alphaValue = 0
        } completionHandler: {
            panel.orderOut(nil)
        }
    }

    private func createPanel() {
        let content = ClipboardHistoryView(onSelect: { [weak self] item in
            self?.pasteItem(item)
            self?.hide()
        }, onDismiss: { [weak self] in
            self?.hide()
        })

        let hosting = NSHostingController(rootView: content)

        let panel = NSPanel(contentViewController: hosting)
        panel.styleMask = [.borderless, .nonactivatingPanel]
        panel.level = .floating
        panel.backgroundColor = .clear
        panel.isOpaque = false
        panel.hasShadow = true
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]

        self.panel = panel
    }

    private func pasteItem(_ item: ClipboardHistory.Item) {
        // Set to pasteboard
        NSPasteboard.general.clearContents()

        switch item.type {
        case .text:
            if let text = item.textContent {
                NSPasteboard.general.setString(text, forType: .string)
            }
        case .image:
            if let data = item.data, let image = NSImage(data: data) {
                NSPasteboard.general.writeObjects([image])
            }
        case .file:
            if let urls = item.fileURLs {
                NSPasteboard.general.writeObjects(urls as [NSURL])
            }
        case .rtf:
            if let data = item.data {
                NSPasteboard.general.setData(data, forType: .rtf)
            }
        case .html:
            if let data = item.data {
                NSPasteboard.general.setData(data, forType: .html)
            }
        }

        // Simulate paste
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.1) {
            let src = CGEventSource(stateID: .hidSystemState)
            let pasteDown = CGEvent(keyboardEventSource: src, virtualKey: 0x09, keyDown: true) // V key
            let pasteUp = CGEvent(keyboardEventSource: src, virtualKey: 0x09, keyDown: false)
            pasteDown?.flags = .maskCommand
            pasteUp?.flags = .maskCommand
            pasteDown?.post(tap: .cghidEventTap)
            pasteUp?.post(tap: .cghidEventTap)
        }

        logger.debug("pasted item from history: \(item.id)")
    }
}

// MARK: - Clipboard History View

struct ClipboardHistoryView: View {
    let onSelect: (ClipboardHistory.Item) -> Void
    let onDismiss: () -> Void

    @State private var clipboardHistory = ClipboardHistory.shared
    @State private var searchText = ""
    @State private var selectedIndex: Int? = nil

    private var filteredItems: [ClipboardHistory.Item] {
        if searchText.isEmpty {
            return clipboardHistory.items
        }
        return clipboardHistory.items.filter { item in
            if let text = item.textContent {
                return text.localizedCaseInsensitiveContains(searchText)
            }
            return false
        }
    }

    var body: some View {
        VStack(spacing: 0) {
            // Search bar
            HStack(spacing: 8) {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(.secondary)
                TextField("Search clipboard history...", text: $searchText)
                    .textFieldStyle(.plain)
                    .font(.system(size: 13))

                if !searchText.isEmpty {
                    Button {
                        searchText = ""
                    } label: {
                        Image(systemName: "xmark.circle.fill")
                            .foregroundStyle(.secondary)
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(10)
            .background(Color(NSColor.textBackgroundColor))

            Divider()

            // Items list
            if filteredItems.isEmpty {
                VStack(spacing: 8) {
                    Image(systemName: "clipboard")
                        .font(.system(size: 32))
                        .foregroundStyle(.tertiary)
                    Text(searchText.isEmpty ? "No clipboard history" : "No matches found")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .padding(20)
            } else {
                ScrollView {
                    LazyVStack(spacing: 2) {
                        ForEach(Array(filteredItems.enumerated()), id: \.element.id) { index, item in
                            ClipboardItemRow(item: item, isSelected: selectedIndex == index)
                                .onTapGesture {
                                    onSelect(item)
                                }
                                .onHover { hovering in
                                    if hovering {
                                        selectedIndex = index
                                    }
                                }
                        }
                    }
                    .padding(4)
                }
            }

            Divider()

            // Footer
            HStack {
                Text("\(filteredItems.count) items")
                    .font(.caption)
                    .foregroundStyle(.tertiary)

                Spacer()

                Text("Enter to paste")
                    .font(.caption)
                    .foregroundStyle(.tertiary)
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
        }
        .frame(width: 320, height: 400)
        .background(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .fill(.ultraThinMaterial)
        )
        .overlay(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .strokeBorder(Color.white.opacity(0.1), lineWidth: 1)
        )
        .shadow(color: .black.opacity(0.3), radius: 20, x: 0, y: 10)
        .onKeyPress(.escape) {
            onDismiss()
            return .handled
        }
        .onKeyPress(.return) {
            if let index = selectedIndex, index < filteredItems.count {
                onSelect(filteredItems[index])
            }
            return .handled
        }
        .onKeyPress(.upArrow) {
            if let current = selectedIndex, current > 0 {
                selectedIndex = current - 1
            } else {
                selectedIndex = filteredItems.count - 1
            }
            return .handled
        }
        .onKeyPress(.downArrow) {
            if let current = selectedIndex, current < filteredItems.count - 1 {
                selectedIndex = current + 1
            } else {
                selectedIndex = 0
            }
            return .handled
        }
    }
}

struct ClipboardItemRow: View {
    let item: ClipboardHistory.Item
    let isSelected: Bool

    var body: some View {
        HStack(spacing: 10) {
            // Type icon
            Image(systemName: iconForType)
                .font(.system(size: 12))
                .foregroundStyle(.secondary)
                .frame(width: 20)

            // Content preview
            VStack(alignment: .leading, spacing: 2) {
                Text(previewText)
                    .font(.system(size: 12))
                    .lineLimit(2)
                    .truncationMode(.tail)

                Text(item.timestamp, style: .relative)
                    .font(.system(size: 10))
                    .foregroundStyle(.tertiary)
            }

            Spacer()

            // Size indicator for images/files
            if item.type == .image || item.type == .file {
                Text(sizeText)
                    .font(.system(size: 10))
                    .foregroundStyle(.tertiary)
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 8)
        .background(
            RoundedRectangle(cornerRadius: 6, style: .continuous)
                .fill(isSelected ? Color.accentColor.opacity(0.2) : Color.clear)
        )
        .contentShape(Rectangle())
    }

    private var iconForType: String {
        switch item.type {
        case .text: return "doc.text"
        case .image: return "photo"
        case .file: return "doc"
        case .rtf: return "doc.richtext"
        case .html: return "globe"
        }
    }

    private var previewText: String {
        switch item.type {
        case .text, .rtf, .html:
            return item.textContent ?? "Empty"
        case .image:
            return "Image"
        case .file:
            if let urls = item.fileURLs {
                return urls.map { $0.lastPathComponent }.joined(separator: ", ")
            }
            return "File"
        }
    }

    private var sizeText: String {
        guard let data = item.data else { return "" }
        let bytes = data.count
        if bytes < 1024 {
            return "\(bytes) B"
        } else if bytes < 1024 * 1024 {
            return "\(bytes / 1024) KB"
        } else {
            return "\(bytes / 1024 / 1024) MB"
        }
    }
}

#Preview {
    ClipboardHistoryView(onSelect: { _ in }, onDismiss: {})
        .padding()
}
