import AppKit
import SwiftUI

private struct ComputerUseLogEntry: Identifiable {
    let id = UUID()
    let timestamp: Date
    let action: String
    let detail: String
    let isError: Bool
}

private enum ClickAction: String, CaseIterable, Identifiable {
    case left = "left_click"
    case right = "right_click"
    case middle = "middle_click"
    case double = "double_click"
    case triple = "triple_click"

    var id: String { rawValue }

    var label: String {
        switch self {
        case .left: return "Left"
        case .right: return "Right"
        case .middle: return "Middle"
        case .double: return "Double"
        case .triple: return "Triple"
        }
    }
}

private enum ScrollDirection: String, CaseIterable, Identifiable {
    case up
    case down
    case left
    case right

    var id: String { rawValue }
}

private enum PermissionLevel {
    case ok
    case warn
    case bad

    var color: Color {
        switch self {
        case .ok: return .green
        case .warn: return .orange
        case .bad: return .red
        }
    }
}

private struct PermissionItem: Identifiable {
    let id = UUID()
    let label: String
    let value: String
    let level: PermissionLevel
}

struct ComputerUseView: View {
    @EnvironmentObject var model: AppModel
    @StateObject private var hotkeyManager = HotkeyManager.shared
    @State private var selectedNode: NodeSummary?
    @State private var approved: Bool = true
    @State private var autoRefresh: Bool = false
    @State private var refreshInterval: Double = 2.0
    @State private var lastScreenshot: NSImage?
    @State private var lastScreenshotAt: Date?
    @State private var statusMessage: String?
    @State private var actionLog: [ComputerUseLogEntry] = []
    @State private var isBusy: Bool = false
    @State private var showHotkeyHints: Bool = true

    @State private var coordinateX: Int = 0
    @State private var coordinateY: Int = 0
    @State private var startX: Int = 0
    @State private var startY: Int = 0
    @State private var endX: Int = 0
    @State private var endY: Int = 0
    @State private var clickAction: ClickAction = .left
    @State private var scrollDirection: ScrollDirection = .down
    @State private var scrollAmount: Int = 3
    @State private var typeText: String = ""
    @State private var keyCombo: String = ""
    @State private var holdDurationMs: Int = 200
    @State private var waitSeconds: Double = 0.5
    @State private var invertY: Bool = false
    @State private var selectedPoint: CGPoint?

    private static let intFormatter: NumberFormatter = {
        let formatter = NumberFormatter()
        formatter.numberStyle = .none
        return formatter
    }()

    private static let floatFormatter: NumberFormatter = {
        let formatter = NumberFormatter()
        formatter.numberStyle = .decimal
        formatter.maximumFractionDigits = 2
        return formatter
    }()

    private var computerNodes: [NodeSummary] {
        model.nodes.filter { $0.tools.contains("nodes.computer_use") }
    }

    var body: some View {
        HSplitView {
            VStack(alignment: .leading, spacing: 12) {
                HStack {
                    Text("Computer Use")
                        .font(.title2)
                    Spacer()
                    Button("Refresh") {
                        Task { await model.refreshNodes() }
                    }
                }

                List(computerNodes, selection: $selectedNode) { node in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(node.name)
                            .font(.headline)
                        Text("\(node.edgeId) - \(node.status)")
                            .font(.caption)
                            .foregroundColor(.secondary)
                    }
                }

                if computerNodes.isEmpty {
                    Text("No connected nodes with computer_use.")
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
            }
            .frame(minWidth: 240)
            .padding()

            VStack(alignment: .leading, spacing: 12) {
                if let node = selectedNode {
                    HStack {
                        VStack(alignment: .leading, spacing: 4) {
                            Text("Remote Control")
                                .font(.title2)
                            Text("\(node.name) - \(node.edgeId)")
                                .font(.caption)
                                .foregroundColor(.secondary)
                        }
                        Spacer()
                        Toggle("Hotkey Hints", isOn: $showHotkeyHints)
                            .toggleStyle(.switch)
                        Toggle("Approved", isOn: $approved)
                            .toggleStyle(.switch)
                    }

                    // Hotkey hints bar
                    if showHotkeyHints && hotkeyManager.globalHotkeysEnabled {
                        HotkeyHintsBar(hotkeyManager: hotkeyManager)
                    }

                    HStack(alignment: .top, spacing: 12) {
                        GroupBox("Display") {
                            VStack(alignment: .leading, spacing: 6) {
                                Text(displayLine(for: node))
                                    .font(.caption)
                                if let scale = displayScale(for: node) {
                                    Text("Scale: \(scale)")
                                        .font(.caption2)
                                        .foregroundColor(.secondary)
                                }
                                if let count = displayCount(for: node) {
                                    Text("Displays: \(count)")
                                        .font(.caption2)
                                        .foregroundColor(.secondary)
                                }
                            }
                            .frame(maxWidth: .infinity, alignment: .leading)
                        }

                        GroupBox("Permissions") {
                            VStack(alignment: .leading, spacing: 6) {
                                ForEach(permissionItems(for: node)) { item in
                                    HStack(spacing: 6) {
                                        Circle()
                                            .fill(item.level.color)
                                            .frame(width: 8, height: 8)
                                        Text("\(item.label): \(item.value)")
                                            .font(.caption2)
                                            .foregroundColor(.secondary)
                                    }
                                }
                                if permissionItems(for: node).isEmpty {
                                    Text("No permission data")
                                        .font(.caption2)
                                        .foregroundColor(.secondary)
                                }
                            }
                            .frame(maxWidth: .infinity, alignment: .leading)
                        }

                        GroupBox("Status") {
                            VStack(alignment: .leading, spacing: 6) {
                                Text(node.status)
                                    .font(.caption)
                                if let last = lastScreenshotAt {
                                    Text("Last capture: \(last.formatted(date: .numeric, time: .standard))")
                                        .font(.caption2)
                                        .foregroundColor(.secondary)
                                }
                                if let status = statusMessage {
                                    Text(status)
                                        .font(.caption2)
                                        .foregroundColor(.secondary)
                                }
                            }
                            .frame(maxWidth: .infinity, alignment: .leading)
                        }
                    }

                    HSplitView {
                        GroupBox("Preview") {
                            VStack(alignment: .leading, spacing: 8) {
                                if let image = lastScreenshot {
                                    ScreenshotPreview(
                                        image: image,
                                        displaySize: displaySize(for: node, image: image),
                                        invertY: invertY,
                                        selectedPoint: $selectedPoint
                                    ) { point in
                                        coordinateX = Int(point.x)
                                        coordinateY = Int(point.y)
                                        selectedPoint = point
                                    }
                                    .frame(minHeight: 260)
                                } else {
                                    ZStack {
                                        Rectangle()
                                            .fill(Color(NSColor.windowBackgroundColor))
                                            .cornerRadius(8)
                                            .overlay(
                                                RoundedRectangle(cornerRadius: 8)
                                                    .stroke(Color.gray.opacity(0.2))
                                            )
                                        Text("No preview yet")
                                            .foregroundColor(.secondary)
                                    }
                                    .frame(minHeight: 260)
                                }

                                HStack {
                                    Button("Screenshot") {
                                        Task { await performAction("screenshot") }
                                    }
                                    .disabled(isBusy)

                                    Button("Cursor Position") {
                                        Task { await performAction("cursor_position") }
                                    }
                                    .disabled(isBusy)

                                    Toggle("Auto Refresh", isOn: $autoRefresh)
                                        .toggleStyle(.switch)
                                        .disabled(isBusy)

                                    TextField("Interval", value: $refreshInterval, formatter: Self.floatFormatter)
                                        .frame(width: 60)
                                    Text("s")
                                        .font(.caption2)
                                        .foregroundColor(.secondary)

                                    Toggle("Flip Y", isOn: $invertY)
                                        .toggleStyle(.switch)
                                }
                                Text("Click on the preview to set coordinates.")
                                    .font(.caption2)
                                    .foregroundColor(.secondary)
                            }
                            .padding(6)
                        }

                        ScrollView {
                            VStack(alignment: .leading, spacing: 12) {
                                GroupBox("Pointer") {
                                    VStack(alignment: .leading, spacing: 8) {
                                        HStack {
                                            Text("X")
                                                .font(.caption)
                                            TextField("X", value: $coordinateX, formatter: Self.intFormatter)
                                                .frame(width: 80)
                                            Text("Y")
                                                .font(.caption)
                                            TextField("Y", value: $coordinateY, formatter: Self.intFormatter)
                                                .frame(width: 80)
                                            Spacer()
                                        }

                                        HStack {
                                            Button("Move") {
                                                Task { await performAction("mouse_move", payload: ["coordinate": [coordinateX, coordinateY]]) }
                                            }
                                            .disabled(isBusy)

                                            Picker("Click", selection: $clickAction) {
                                                ForEach(ClickAction.allCases) { action in
                                                    Text(action.label).tag(action)
                                                }
                                            }
                                            .frame(width: 140)

                                            Button("Run") {
                                                Task { await performAction(clickAction.rawValue, payload: ["coordinate": [coordinateX, coordinateY]]) }
                                            }
                                            .disabled(isBusy)

                                            Button("Mouse Down") {
                                                Task { await performAction("left_mouse_down", payload: ["coordinate": [coordinateX, coordinateY]]) }
                                            }
                                            .disabled(isBusy)

                                            Button("Mouse Up") {
                                                Task { await performAction("left_mouse_up", payload: ["coordinate": [coordinateX, coordinateY]]) }
                                            }
                                            .disabled(isBusy)
                                        }

                                        Divider()

                                        HStack {
                                            Text("Start")
                                                .font(.caption)
                                            TextField("X", value: $startX, formatter: Self.intFormatter)
                                                .frame(width: 70)
                                            TextField("Y", value: $startY, formatter: Self.intFormatter)
                                                .frame(width: 70)
                                            Text("End")
                                                .font(.caption)
                                            TextField("X", value: $endX, formatter: Self.intFormatter)
                                                .frame(width: 70)
                                            TextField("Y", value: $endY, formatter: Self.intFormatter)
                                                .frame(width: 70)
                                            Button("Drag") {
                                                Task {
                                                    await performAction("left_click_drag", payload: [
                                                        "start_coordinate": [startX, startY],
                                                        "end_coordinate": [endX, endY],
                                                    ])
                                                }
                                            }
                                            .disabled(isBusy)
                                        }
                                    }
                                    .padding(.vertical, 4)
                                }

                                GroupBox("Scroll") {
                                    HStack {
                                        Picker("Direction", selection: $scrollDirection) {
                                            ForEach(ScrollDirection.allCases) { direction in
                                                Text(direction.rawValue.capitalized).tag(direction)
                                            }
                                        }
                                        .frame(width: 140)
                                        Stepper("Amount: \(scrollAmount)", value: $scrollAmount, in: 1...20)
                                            .frame(width: 180)
                                        Button("Scroll") {
                                            Task {
                                                await performAction("scroll", payload: [
                                                    "scroll_direction": scrollDirection.rawValue,
                                                    "scroll_amount": scrollAmount,
                                                ])
                                            }
                                        }
                                        .disabled(isBusy)
                                        Spacer()
                                    }
                                    .padding(.vertical, 4)
                                }

                                GroupBox("Keyboard") {
                                    VStack(alignment: .leading, spacing: 8) {
                                        HStack {
                                            TextField("Type text", text: $typeText)
                                                .textFieldStyle(.roundedBorder)
                                            Button("Type") {
                                                Task { await performAction("type", payload: ["text": typeText]) }
                                            }
                                            .disabled(isBusy || typeText.isEmpty)
                                        }

                                        HStack {
                                            TextField("Key combo (cmd+c)", text: $keyCombo)
                                                .textFieldStyle(.roundedBorder)
                                            Button("Key") {
                                                Task { await performAction("key", payload: ["text": keyCombo]) }
                                            }
                                            .disabled(isBusy || keyCombo.isEmpty)
                                            Button("Hold") {
                                                Task {
                                                    await performAction("hold_key", payload: [
                                                        "text": keyCombo,
                                                        "duration_ms": holdDurationMs,
                                                    ])
                                                }
                                            }
                                            .disabled(isBusy || keyCombo.isEmpty)
                                            TextField("ms", value: $holdDurationMs, formatter: Self.intFormatter)
                                                .frame(width: 60)
                                        }
                                    }
                                    .padding(.vertical, 4)
                                }

                                GroupBox("Wait") {
                                    HStack {
                                        TextField("Seconds", value: $waitSeconds, formatter: Self.floatFormatter)
                                            .frame(width: 80)
                                        Button("Wait") {
                                            Task { await performAction("wait", payload: ["duration_seconds": waitSeconds]) }
                                        }
                                        .disabled(isBusy)
                                        Spacer()
                                    }
                                    .padding(.vertical, 4)
                                }
                            }
                            .padding(6)
                        }
                        .frame(minWidth: 380)
                    }
                    .frame(minHeight: 360)

                    GroupBox("Activity") {
                        if actionLog.isEmpty {
                            Text("No actions yet")
                                .font(.caption)
                                .foregroundColor(.secondary)
                        } else {
                            List(actionLog) { entry in
                                VStack(alignment: .leading, spacing: 4) {
                                    Text(entry.action)
                                        .font(.caption)
                                    Text(entry.detail)
                                        .font(.caption2)
                                        .foregroundColor(entry.isError ? .red : .secondary)
                                    Text(entry.timestamp.formatted(date: .numeric, time: .standard))
                                        .font(.caption2)
                                        .foregroundColor(.secondary)
                                }
                            }
                            .frame(minHeight: 160)
                        }
                    }
                } else {
                    Text("Select a node to control")
                        .foregroundColor(.secondary)
                    Spacer()
                }
            }
            .padding()
        }
        .task(id: autoRefresh) {
            guard autoRefresh else { return }
            while autoRefresh && !Task.isCancelled {
                await performAction("screenshot", logEntry: false)
                let delay = UInt64(max(refreshInterval, 0.5) * 1_000_000_000)
                try? await Task.sleep(nanoseconds: delay)
            }
        }
        .onChange(of: selectedNode) { newNode in
            if newNode == nil {
                autoRefresh = false
            }
            // Update the HotkeyManager with the selected node
            hotkeyManager.selectedNodeEdgeId = newNode?.edgeId
        }
        .onAppear {
            // Configure the HotkeyManager with the app model
            hotkeyManager.configure(with: model)
        }
        .onChange(of: coordinateX) { _ in
            selectedPoint = CGPoint(x: coordinateX, y: coordinateY)
        }
        .onChange(of: coordinateY) { _ in
            selectedPoint = CGPoint(x: coordinateX, y: coordinateY)
        }
    }

    @MainActor
    private func performAction(_ action: String, payload: [String: Any] = [:], logEntry: Bool = true) async {
        guard let node = selectedNode else {
            statusMessage = "Select a node first"
            return
        }
        isBusy = true
        defer { isBusy = false }

        var body = payload
        body["action"] = action
        let result = await model.invokeTool(edgeID: node.edgeId, toolName: "nodes.computer_use", payload: body, approved: approved)
        if let result = result {
            statusMessage = result.isError ? "Action failed" : "Action complete"
            if let firstImage = model.images(from: result).first {
                lastScreenshot = firstImage
                lastScreenshotAt = Date()
            }
            if action == "cursor_position" {
                updateCursorPosition(from: result.content)
            }
            if logEntry {
                appendLog(action: action, detail: result.content, isError: result.isError)
            }
        } else if logEntry {
            appendLog(action: action, detail: model.lastError ?? "Unknown error", isError: true)
        }
    }

    @MainActor
    private func updateCursorPosition(from content: String) {
        guard let data = content.data(using: .utf8),
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            return
        }
        if let x = json["x"] as? Double {
            coordinateX = Int(x)
        } else if let x = json["x"] as? Int {
            coordinateX = x
        }
        if let y = json["y"] as? Double {
            coordinateY = Int(y)
        } else if let y = json["y"] as? Int {
            coordinateY = y
        }
        selectedPoint = CGPoint(x: coordinateX, y: coordinateY)
    }

    @MainActor
    private func appendLog(action: String, detail: String, isError: Bool) {
        let entry = ComputerUseLogEntry(timestamp: Date(), action: action, detail: detail, isError: isError)
        actionLog.insert(entry, at: 0)
        if actionLog.count > 200 {
            actionLog.removeLast()
        }
    }

    private func displayLine(for node: NodeSummary) -> String {
        let width = node.metadata?["display_width_px"].flatMap(Int.init) ?? 0
        let height = node.metadata?["display_height_px"].flatMap(Int.init) ?? 0
        let displayNumber = node.metadata?["display_number"].flatMap(Int.init) ?? 0
        if width > 0 && height > 0 {
            return "Display \(displayNumber): \(width)x\(height) px"
        }
        return "Display \(displayNumber): unknown"
    }

    private func displayScale(for node: NodeSummary) -> String? {
        guard let raw = node.metadata?["display_scale"], !raw.isEmpty else { return nil }
        return raw
    }

    private func displayCount(for node: NodeSummary) -> String? {
        guard let raw = node.metadata?["display_count"], !raw.isEmpty else { return nil }
        return raw
    }

    private func displaySize(for node: NodeSummary, image: NSImage) -> CGSize {
        if let width = node.metadata?["display_width_px"].flatMap(Int.init),
           let height = node.metadata?["display_height_px"].flatMap(Int.init),
           width > 0 && height > 0 {
            return CGSize(width: width, height: height)
        }
        return image.pixelSize
    }

    private func permissionItems(for node: NodeSummary) -> [PermissionItem] {
        let mapping: [(String, String)] = [
            ("perm_accessibility", "Accessibility"),
            ("perm_screen_recording", "Screen Recording"),
            ("perm_camera", "Camera"),
            ("perm_microphone", "Microphone"),
            ("perm_notifications", "Notifications"),
        ]
        var items: [PermissionItem] = []
        for (key, label) in mapping {
            guard let value = node.metadata?[key], !value.isEmpty else { continue }
            let level = permissionLevel(for: value)
            items.append(PermissionItem(label: label, value: value, level: level))
        }
        return items
    }

    private func permissionLevel(for value: String) -> PermissionLevel {
        let normalized = value.lowercased()
        if ["granted", "authorized"].contains(normalized) {
            return .ok
        }
        if ["denied", "restricted"].contains(normalized) {
            return .bad
        }
        return .warn
    }
}

private struct ScreenshotPreview: View {
    let image: NSImage
    let displaySize: CGSize
    let invertY: Bool
    @Binding var selectedPoint: CGPoint?
    let onPick: (CGPoint) -> Void

    var body: some View {
        GeometryReader { geo in
            let viewSize = geo.size
            let layout = ImageLayout(viewSize: viewSize, imageSize: displaySize)

            ZStack(alignment: .topLeading) {
                Image(nsImage: image)
                    .resizable()
                    .aspectRatio(contentMode: .fit)
                    .frame(width: viewSize.width, height: viewSize.height)
                    .background(Color(NSColor.windowBackgroundColor))

                if let point = selectedPoint {
                    let viewPoint = layout.viewPoint(for: point, invertY: invertY)
                    Circle()
                        .stroke(Color.accentColor, lineWidth: 2)
                        .frame(width: 12, height: 12)
                        .position(x: viewPoint.x, y: viewPoint.y)
                }
            }
            .contentShape(Rectangle())
            .gesture(
                DragGesture(minimumDistance: 0)
                    .onEnded { value in
                        if let point = layout.pixelPoint(for: value.location, invertY: invertY) {
                            onPick(point)
                        }
                    }
            )
        }
    }
}

private struct ImageLayout {
    let viewSize: CGSize
    let imageSize: CGSize
    let renderSize: CGSize
    let offset: CGPoint
    let scale: CGFloat

    init(viewSize: CGSize, imageSize: CGSize) {
        self.viewSize = viewSize
        self.imageSize = imageSize
        let imageAspect = imageSize.width / max(imageSize.height, 1)
        let viewAspect = viewSize.width / max(viewSize.height, 1)

        if viewAspect > imageAspect {
            let height = viewSize.height
            let width = height * imageAspect
            renderSize = CGSize(width: width, height: height)
            offset = CGPoint(x: (viewSize.width - width) / 2, y: 0)
        } else {
            let width = viewSize.width
            let height = width / imageAspect
            renderSize = CGSize(width: width, height: height)
            offset = CGPoint(x: 0, y: (viewSize.height - height) / 2)
        }

        scale = renderSize.width / max(imageSize.width, 1)
    }

    func pixelPoint(for location: CGPoint, invertY: Bool) -> CGPoint? {
        let localX = location.x - offset.x
        let localY = location.y - offset.y
        guard localX >= 0,
              localY >= 0,
              localX <= renderSize.width,
              localY <= renderSize.height else {
            return nil
        }
        let rawX = localX / scale
        let rawY = localY / scale
        let finalY = invertY ? (imageSize.height - rawY) : rawY
        return CGPoint(x: rawX, y: finalY)
    }

    func viewPoint(for point: CGPoint, invertY: Bool) -> CGPoint {
        let yValue = invertY ? (imageSize.height - point.y) : point.y
        let x = point.x * scale + offset.x
        let y = yValue * scale + offset.y
        return CGPoint(x: x, y: y)
    }
}

private extension NSImage {
    var pixelSize: CGSize {
        if let rep = representations.first {
            return CGSize(width: rep.pixelsWide, height: rep.pixelsHigh)
        }
        return size
    }
}

// MARK: - Hotkey Hints Bar

private struct HotkeyHintsBar: View {
    @ObservedObject var hotkeyManager: HotkeyManager

    var body: some View {
        HStack(spacing: 16) {
            Text("Hotkeys:")
                .font(.caption)
                .foregroundColor(.secondary)

            ForEach(HotkeyAction.allCases) { action in
                if let binding = hotkeyManager.binding(for: action), binding.isEnabled {
                    HotkeyHintBadge(
                        action: action,
                        binding: binding,
                        isActive: hotkeyManager.lastTriggeredAction == action
                    )
                }
            }

            Spacer()

            if hotkeyManager.isExecuting {
                ProgressView()
                    .scaleEffect(0.6)
                    .frame(width: 16, height: 16)
                Text("Executing...")
                    .font(.caption)
                    .foregroundColor(.secondary)
            }

            if let status = hotkeyManager.statusMessage {
                Text(status)
                    .font(.caption)
                    .foregroundColor(.secondary)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(
            RoundedRectangle(cornerRadius: 8)
                .fill(Color(NSColor.controlBackgroundColor))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(Color.gray.opacity(0.2), lineWidth: 1)
        )
    }
}

private struct HotkeyHintBadge: View {
    let action: HotkeyAction
    let binding: HotkeyBinding
    let isActive: Bool

    var body: some View {
        HStack(spacing: 4) {
            Text(actionIcon)
                .font(.caption)
            Text(binding.keyDisplayString)
                .font(.system(.caption, design: .monospaced))
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(
            RoundedRectangle(cornerRadius: 6)
                .fill(isActive ? Color.accentColor.opacity(0.2) : Color(NSColor.controlBackgroundColor))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 6)
                .stroke(isActive ? Color.accentColor : Color.gray.opacity(0.3), lineWidth: 1)
        )
        .help(action.displayName)
    }

    private var actionIcon: String {
        switch action {
        case .screenshot: return "S"
        case .click: return "C"
        case .cursorPosition: return "P"
        case .typeClipboard: return "T"
        }
    }
}
