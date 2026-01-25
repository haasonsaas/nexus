import AppKit
import OSLog
import SwiftUI

#if DEBUG

// MARK: - DebugActionsPanel (Window Controller)

/// Floating panel for the debug actions interface.
@MainActor
final class DebugActionsPanelController {
    static let shared = DebugActionsPanelController()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "debug")

    private var panel: NSPanel?

    private init() {}

    /// Show the debug actions panel.
    func show() {
        if panel == nil {
            createPanel()
        }

        guard let panel else { return }

        // Center on screen
        if let screen = NSScreen.main {
            let screenFrame = screen.visibleFrame
            let panelSize = NSSize(width: 720, height: 560)
            let origin = NSPoint(
                x: screenFrame.midX - panelSize.width / 2,
                y: screenFrame.midY - panelSize.height / 2
            )
            panel.setFrame(NSRect(origin: origin, size: panelSize), display: true)
        }

        panel.alphaValue = 0
        panel.orderFront(nil)
        panel.makeKey()

        NSAnimationContext.runAnimationGroup { context in
            context.duration = 0.2
            context.timingFunction = CAMediaTimingFunction(name: .easeOut)
            panel.animator().alphaValue = 1
        }

        logger.debug("debug actions panel shown")
    }

    /// Hide the debug actions panel.
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

    /// Toggle the debug actions panel visibility.
    func toggle() {
        if let panel, panel.isVisible {
            hide()
        } else {
            show()
        }
    }

    private func createPanel() {
        let content = DebugActionsView(onDismiss: { [weak self] in
            self?.hide()
        })

        let hosting = NSHostingController(rootView: content)

        let panel = NSPanel(contentViewController: hosting)
        panel.styleMask = [.titled, .closable, .resizable, .nonactivatingPanel]
        panel.title = "Debug Actions"
        panel.level = .floating
        panel.isMovableByWindowBackground = true
        panel.hasShadow = true
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
        panel.minSize = NSSize(width: 500, height: 400)

        self.panel = panel
    }
}

// MARK: - DebugActionsView

/// Main view for the debug actions interface.
struct DebugActionsView: View {
    let onDismiss: () -> Void

    @State private var manager = DebugActionsManager.shared
    @State private var searchText = ""
    @State private var selectedCategory: DebugActionCategory?
    @State private var showingConfirmation = false
    @State private var pendingAction: DebugAction?
    @State private var showingOutput = true

    private var filteredActions: [DebugAction] {
        var actions = manager.actions

        // Filter by category
        if let category = selectedCategory {
            actions = actions.filter { $0.category == category }
        }

        // Filter by search
        if !searchText.isEmpty {
            let lowercased = searchText.lowercased()
            actions = actions.filter {
                $0.name.lowercased().contains(lowercased) ||
                $0.description.lowercased().contains(lowercased)
            }
        }

        return actions
    }

    var body: some View {
        VStack(spacing: 0) {
            // Header with search
            headerView

            Divider()

            // Category tabs
            categoryTabsView
                .padding(.horizontal)
                .padding(.vertical, 8)

            Divider()

            // Main content
            HSplitView {
                // Actions grid
                actionsGridView
                    .frame(minWidth: 300)

                if showingOutput {
                    Divider()

                    // Output log
                    outputLogView
                        .frame(minWidth: 200)
                }
            }

            Divider()

            // Footer
            footerView
        }
        .frame(minWidth: 500, minHeight: 400)
        .background(Color(NSColor.windowBackgroundColor))
        .onKeyPress(.escape) {
            onDismiss()
            return .handled
        }
        .alert("Confirm Destructive Action", isPresented: $showingConfirmation) {
            Button("Cancel", role: .cancel) {
                pendingAction = nil
            }
            Button("Execute", role: .destructive) {
                if let action = pendingAction {
                    executeAction(action)
                }
                pendingAction = nil
            }
        } message: {
            if let action = pendingAction {
                Text("Are you sure you want to execute \"\(action.name)\"?\n\n\(action.description)")
            }
        }
    }

    // MARK: - Header View

    private var headerView: some View {
        HStack(spacing: 12) {
            Image(systemName: "wrench.and.screwdriver")
                .font(.system(size: 18))
                .foregroundStyle(.secondary)

            Text("Debug Actions")
                .font(.headline)

            Spacer()

            // Search field
            HStack(spacing: 6) {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(.tertiary)
                TextField("Search actions...", text: $searchText)
                    .textFieldStyle(.plain)
                    .frame(width: 180)
                if !searchText.isEmpty {
                    Button {
                        searchText = ""
                    } label: {
                        Image(systemName: "xmark.circle.fill")
                            .foregroundStyle(.tertiary)
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(
                RoundedRectangle(cornerRadius: 6, style: .continuous)
                    .fill(Color(NSColor.controlBackgroundColor))
            )

            // Toggle output button
            Button {
                withAnimation(.easeInOut(duration: 0.2)) {
                    showingOutput.toggle()
                }
            } label: {
                Image(systemName: showingOutput ? "sidebar.right" : "sidebar.left")
            }
            .buttonStyle(.plain)
            .help(showingOutput ? "Hide output log" : "Show output log")

            // Close button
            Button {
                onDismiss()
            } label: {
                Image(systemName: "xmark")
                    .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)
            .keyboardShortcut(.escape, modifiers: [])
        }
        .padding()
    }

    // MARK: - Category Tabs

    private var categoryTabsView: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                CategoryTab(
                    title: "All",
                    icon: "square.grid.2x2",
                    isSelected: selectedCategory == nil
                ) {
                    withAnimation(.easeInOut(duration: 0.15)) {
                        selectedCategory = nil
                    }
                }

                ForEach(DebugActionCategory.allCases) { category in
                    CategoryTab(
                        title: category.rawValue,
                        icon: category.icon,
                        isSelected: selectedCategory == category
                    ) {
                        withAnimation(.easeInOut(duration: 0.15)) {
                            selectedCategory = category
                        }
                    }
                }
            }
        }
    }

    // MARK: - Actions Grid

    private var actionsGridView: some View {
        ScrollView {
            LazyVGrid(
                columns: [
                    GridItem(.adaptive(minimum: 200, maximum: 300), spacing: 12)
                ],
                spacing: 12
            ) {
                ForEach(filteredActions) { action in
                    ActionCard(
                        action: action,
                        isExecuting: manager.currentActionId == action.id
                    ) {
                        if action.isDestructive {
                            pendingAction = action
                            showingConfirmation = true
                        } else {
                            executeAction(action)
                        }
                    }
                }
            }
            .padding()
        }
        .overlay {
            if filteredActions.isEmpty {
                VStack(spacing: 12) {
                    Image(systemName: "magnifyingglass")
                        .font(.system(size: 40))
                        .foregroundStyle(.tertiary)
                    Text("No matching actions")
                        .font(.headline)
                        .foregroundStyle(.secondary)
                    if !searchText.isEmpty {
                        Text("Try a different search term")
                            .font(.subheadline)
                            .foregroundStyle(.tertiary)
                    }
                }
            }
        }
    }

    // MARK: - Output Log

    private var outputLogView: some View {
        VStack(spacing: 0) {
            HStack {
                Text("Output Log")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)

                Spacer()

                if !manager.results.isEmpty {
                    Button("Clear") {
                        manager.clearResults()
                    }
                    .font(.caption)
                    .buttonStyle(.plain)
                    .foregroundStyle(.secondary)
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            .background(Color(NSColor.controlBackgroundColor))

            Divider()

            if manager.results.isEmpty {
                VStack(spacing: 8) {
                    Image(systemName: "text.alignleft")
                        .font(.system(size: 24))
                        .foregroundStyle(.tertiary)
                    Text("No output yet")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 8) {
                        ForEach(manager.results) { result in
                            OutputLogEntry(result: result)
                        }
                    }
                    .padding(12)
                }
            }
        }
    }

    // MARK: - Footer

    private var footerView: some View {
        HStack {
            Text("\(filteredActions.count) actions")
                .font(.caption)
                .foregroundStyle(.tertiary)

            Spacer()

            if manager.isExecuting {
                HStack(spacing: 6) {
                    ProgressView()
                        .scaleEffect(0.6)
                    Text("Executing...")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }

            Spacer()

            Text("Press Esc to close")
                .font(.caption)
                .foregroundStyle(.tertiary)
        }
        .padding(.horizontal)
        .padding(.vertical, 8)
    }

    // MARK: - Actions

    private func executeAction(_ action: DebugAction) {
        Task {
            await manager.run(action: action)
        }
    }
}

// MARK: - Category Tab

private struct CategoryTab: View {
    let title: String
    let icon: String
    let isSelected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                Image(systemName: icon)
                    .font(.system(size: 12))
                Text(title)
                    .font(.subheadline)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
            .background(
                RoundedRectangle(cornerRadius: 6, style: .continuous)
                    .fill(isSelected ? Color.accentColor.opacity(0.15) : Color.clear)
            )
            .foregroundStyle(isSelected ? Color.accentColor : Color.secondary)
        }
        .buttonStyle(.plain)
    }
}

// MARK: - Action Card

private struct ActionCard: View {
    let action: DebugAction
    let isExecuting: Bool
    let onExecute: () -> Void

    @State private var isHovered = false

    var body: some View {
        Button(action: onExecute) {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Image(systemName: action.category.icon)
                        .font(.system(size: 14))
                        .foregroundStyle(action.isDestructive ? Color.red : Color.accentColor)

                    Text(action.name)
                        .font(.headline)
                        .foregroundStyle(action.isDestructive ? Color.red : Color.primary)
                        .lineLimit(1)

                    Spacer()

                    if isExecuting {
                        ProgressView()
                            .scaleEffect(0.6)
                    } else if let shortcut = action.keyboardShortcut {
                        Text(shortcut.displayString)
                            .font(.system(size: 10, design: .monospaced))
                            .foregroundStyle(.tertiary)
                            .padding(.horizontal, 4)
                            .padding(.vertical, 2)
                            .background(
                                RoundedRectangle(cornerRadius: 3)
                                    .fill(Color(NSColor.controlBackgroundColor))
                            )
                    }
                }

                Text(action.description)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
                    .multilineTextAlignment(.leading)

                HStack {
                    Text(action.category.rawValue)
                        .font(.system(size: 10))
                        .foregroundStyle(.tertiary)

                    Spacer()

                    if action.isDestructive {
                        HStack(spacing: 2) {
                            Image(systemName: "exclamationmark.triangle.fill")
                                .font(.system(size: 10))
                            Text("Destructive")
                                .font(.system(size: 10))
                        }
                        .foregroundStyle(.orange)
                    }
                }
            }
            .padding(12)
            .background(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .fill(Color(NSColor.controlBackgroundColor))
            )
            .overlay(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .strokeBorder(
                        isHovered ? Color.accentColor.opacity(0.5) : Color.gray.opacity(0.2),
                        lineWidth: 1
                    )
            )
            .scaleEffect(isHovered ? 1.02 : 1.0)
        }
        .buttonStyle(.plain)
        .disabled(isExecuting)
        .onHover { hovering in
            withAnimation(.easeInOut(duration: 0.15)) {
                isHovered = hovering
            }
        }
    }
}

// MARK: - Output Log Entry

private struct OutputLogEntry: View {
    let result: DebugActionResult

    private var statusIcon: String {
        result.success ? "checkmark.circle.fill" : "xmark.circle.fill"
    }

    private var statusColor: Color {
        result.success ? .green : .red
    }

    private var formattedTime: String {
        let formatter = DateFormatter()
        formatter.timeStyle = .medium
        return formatter.string(from: result.timestamp)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Image(systemName: statusIcon)
                    .font(.system(size: 12))
                    .foregroundStyle(statusColor)

                Text(result.actionName)
                    .font(.caption)
                    .fontWeight(.medium)

                Spacer()

                Text(formattedTime)
                    .font(.system(size: 10, design: .monospaced))
                    .foregroundStyle(.tertiary)
            }

            Text(result.message)
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
                .lineLimit(10)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(8)
        .background(
            RoundedRectangle(cornerRadius: 6, style: .continuous)
                .fill(statusColor.opacity(0.05))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 6, style: .continuous)
                .strokeBorder(statusColor.opacity(0.2), lineWidth: 1)
        )
    }
}

// MARK: - Preview

#Preview {
    DebugActionsView(onDismiss: {})
        .frame(width: 720, height: 560)
}

#endif
