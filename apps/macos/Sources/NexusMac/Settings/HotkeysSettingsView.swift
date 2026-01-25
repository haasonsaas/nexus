import SwiftUI
import Carbon.HIToolbox

/// Settings view for configuring global keyboard shortcuts.
struct HotkeysSettingsView: View {
    @State private var hotkeyManager = HotkeyManager.shared
    @State private var showRecorder = false
    @State private var recordingAction: HotkeyAction?
    @State private var hasAccessibility = false

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            // Header
            headerSection

            // Permission notice
            if !hasAccessibility {
                accessibilityNotice
            }

            // Global toggle
            globalToggle

            // Bindings list
            bindingsList

            Spacer()
        }
        .padding()
        .onAppear {
            hasAccessibility = hotkeyManager.isAccessibilityGranted()
        }
        .sheet(isPresented: $showRecorder) {
            if let action = recordingAction {
                HotkeyRecorderSheet(
                    action: action,
                    currentBinding: hotkeyManager.binding(for: action),
                    onSave: { binding in
                        hotkeyManager.updateBinding(binding)
                        showRecorder = false
                    },
                    onCancel: {
                        showRecorder = false
                    }
                )
            }
        }
    }

    // MARK: - Header

    private var headerSection: some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                Text("Keyboard Shortcuts")
                    .font(.headline)
                Text("Configure global hotkeys for quick actions")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            Button {
                hotkeyManager.resetToDefaults()
            } label: {
                Text("Reset to Defaults")
            }
            .buttonStyle(.bordered)
            .controlSize(.small)
        }
    }

    // MARK: - Accessibility Notice

    private var accessibilityNotice: some View {
        HStack(spacing: 12) {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.orange)
                .font(.title3)

            VStack(alignment: .leading, spacing: 4) {
                Text("Accessibility Permission Required")
                    .font(.subheadline.weight(.medium))
                Text("Global hotkeys need accessibility access to work.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            Button("Grant Access") {
                _ = hotkeyManager.checkAccessibilityPermission()
                // Refresh status after a delay
                Task {
                    try? await Task.sleep(for: .seconds(1))
                    hasAccessibility = hotkeyManager.isAccessibilityGranted()
                }
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.small)
        }
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .fill(Color.orange.opacity(0.1))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .stroke(Color.orange.opacity(0.3), lineWidth: 1)
        )
    }

    // MARK: - Global Toggle

    private var globalToggle: some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text("Enable Global Hotkeys")
                    .font(.subheadline.weight(.medium))
                Text("Allow shortcuts to work even when Nexus is in the background")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            Toggle("", isOn: Binding(
                get: { hotkeyManager.globalHotkeysEnabled },
                set: { hotkeyManager.globalHotkeysEnabled = $0 }
            ))
            .labelsHidden()
            .toggleStyle(.switch)
        }
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .fill(Color(NSColor.controlBackgroundColor))
        )
    }

    // MARK: - Bindings List

    private var bindingsList: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Actions")
                .font(.subheadline.weight(.medium))
                .foregroundStyle(.secondary)

            ForEach(hotkeyManager.bindings) { binding in
                HotkeyBindingRow(
                    binding: binding,
                    isEnabled: hotkeyManager.globalHotkeysEnabled,
                    onToggle: { enabled in
                        hotkeyManager.setEnabled(enabled, for: binding.action)
                    },
                    onEdit: {
                        recordingAction = binding.action
                        showRecorder = true
                    }
                )
            }
        }
    }
}

// MARK: - Binding Row

struct HotkeyBindingRow: View {
    let binding: HotkeyBinding
    let isEnabled: Bool
    let onToggle: (Bool) -> Void
    let onEdit: () -> Void

    @State private var isHovered = false

    var body: some View {
        HStack(spacing: 12) {
            // Icon
            Image(systemName: actionIcon)
                .font(.system(size: 14))
                .foregroundStyle(binding.isEnabled ? .primary : .secondary)
                .frame(width: 24)

            // Action info
            VStack(alignment: .leading, spacing: 2) {
                Text(binding.action.displayName)
                    .font(.subheadline)
                    .foregroundStyle(binding.isEnabled ? .primary : .secondary)

                Text(binding.action.description)
                    .font(.caption)
                    .foregroundStyle(.tertiary)
                    .lineLimit(1)
            }

            Spacer()

            // Shortcut display
            HStack(spacing: 4) {
                ForEach(modifierSymbols, id: \.self) { symbol in
                    Text(symbol)
                        .font(.system(size: 12, weight: .medium, design: .rounded))
                }
                Text(keySymbol)
                    .font(.system(size: 12, weight: .semibold, design: .rounded))
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(
                RoundedRectangle(cornerRadius: 6, style: .continuous)
                    .fill(Color(NSColor.controlBackgroundColor))
            )
            .overlay(
                RoundedRectangle(cornerRadius: 6, style: .continuous)
                    .stroke(Color.gray.opacity(0.2), lineWidth: 1)
            )
            .opacity(binding.isEnabled ? 1 : 0.5)

            // Edit button
            Button {
                onEdit()
            } label: {
                Image(systemName: "pencil")
                    .font(.caption)
            }
            .buttonStyle(.borderless)
            .opacity(isHovered ? 1 : 0)

            // Toggle
            Toggle("", isOn: Binding(
                get: { binding.isEnabled },
                set: { onToggle($0) }
            ))
            .labelsHidden()
            .toggleStyle(.switch)
            .controlSize(.small)
            .disabled(!isEnabled)
        }
        .padding(10)
        .background(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .fill(isHovered ? Color(NSColor.controlBackgroundColor) : Color.clear)
        )
        .onHover { hovering in
            withAnimation(.easeOut(duration: 0.15)) {
                isHovered = hovering
            }
        }
    }

    private var actionIcon: String {
        switch binding.action {
        case .screenshot: return "camera.viewfinder"
        case .click: return "cursorarrow.click"
        case .cursorPosition: return "cursorarrow"
        case .typeClipboard: return "keyboard"
        }
    }

    private var modifierSymbols: [String] {
        var symbols: [String] = []
        if binding.modifiers.contains(.control) { symbols.append("\u{2303}") }
        if binding.modifiers.contains(.option) { symbols.append("\u{2325}") }
        if binding.modifiers.contains(.shift) { symbols.append("\u{21E7}") }
        if binding.modifiers.contains(.command) { symbols.append("\u{2318}") }
        return symbols
    }

    private var keySymbol: String {
        HotkeyBinding.keyCodeToString(binding.keyCode)
    }
}

// MARK: - Hotkey Recorder Sheet

struct HotkeyRecorderSheet: View {
    let action: HotkeyAction
    let currentBinding: HotkeyBinding?
    let onSave: (HotkeyBinding) -> Void
    let onCancel: () -> Void

    @State private var isRecording = false
    @State private var recordedKeyCode: UInt32?
    @State private var recordedModifiers: HotkeyModifiers?

    var body: some View {
        VStack(spacing: 20) {
            // Header
            VStack(spacing: 8) {
                Image(systemName: "keyboard")
                    .font(.system(size: 40))
                    .foregroundStyle(.secondary)

                Text("Record Shortcut")
                    .font(.headline)

                Text("Press the key combination for \"\(action.displayName)\"")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            // Recording area
            VStack(spacing: 8) {
                ZStack {
                    RoundedRectangle(cornerRadius: 12, style: .continuous)
                        .fill(Color(NSColor.controlBackgroundColor))
                        .frame(height: 60)

                    if isRecording {
                        Text("Press keys...")
                            .font(.title3)
                            .foregroundStyle(.secondary)
                    } else if let keyCode = recordedKeyCode, let modifiers = recordedModifiers {
                        HStack(spacing: 4) {
                            ForEach(modifierSymbols(modifiers), id: \.self) { symbol in
                                Text(symbol)
                                    .font(.system(size: 24, weight: .medium, design: .rounded))
                            }
                            Text(HotkeyBinding.keyCodeToString(keyCode))
                                .font(.system(size: 24, weight: .semibold, design: .rounded))
                        }
                    } else if let binding = currentBinding {
                        Text(binding.keyDisplayString)
                            .font(.title3)
                            .foregroundStyle(.secondary)
                    } else {
                        Text("No shortcut set")
                            .font(.title3)
                            .foregroundStyle(.tertiary)
                    }
                }

                Button(isRecording ? "Stop Recording" : "Start Recording") {
                    isRecording.toggle()
                }
                .buttonStyle(.bordered)
            }

            Divider()

            // Actions
            HStack {
                Button("Cancel") {
                    onCancel()
                }
                .keyboardShortcut(.escape, modifiers: [])

                Spacer()

                Button("Save") {
                    if let keyCode = recordedKeyCode, let modifiers = recordedModifiers {
                        let newBinding = HotkeyBinding(
                            action: action,
                            keyCode: keyCode,
                            modifiers: modifiers,
                            isEnabled: currentBinding?.isEnabled ?? true
                        )
                        onSave(newBinding)
                    }
                }
                .buttonStyle(.borderedProminent)
                .disabled(recordedKeyCode == nil)
                .keyboardShortcut(.return, modifiers: [])
            }
        }
        .padding(24)
        .frame(width: 350)
        .onAppear {
            if isRecording {
                startKeyMonitor()
            }
        }
        .onChange(of: isRecording) { _, recording in
            if recording {
                startKeyMonitor()
            }
        }
    }

    private func modifierSymbols(_ modifiers: HotkeyModifiers) -> [String] {
        var symbols: [String] = []
        if modifiers.contains(.control) { symbols.append("\u{2303}") }
        if modifiers.contains(.option) { symbols.append("\u{2325}") }
        if modifiers.contains(.shift) { symbols.append("\u{21E7}") }
        if modifiers.contains(.command) { symbols.append("\u{2318}") }
        return symbols
    }

    private func startKeyMonitor() {
        // In a real implementation, this would use NSEvent.addLocalMonitorForEvents
        // to capture key events. For now, we'll use a simple placeholder.
        // The full implementation would require proper event monitoring.
    }
}

#Preview {
    HotkeysSettingsView()
        .frame(width: 500, height: 500)
}
