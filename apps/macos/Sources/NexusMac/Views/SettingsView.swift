import SwiftUI

struct SettingsView: View {
    @EnvironmentObject var model: AppModel
    @State private var showSaved = false
    @StateObject private var notificationPreferences = NotificationPreferencesModel()
    @StateObject private var hotkeyPreferences = HotkeyPreferencesModel()

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 24) {
                // Connection Settings Section
                VStack(alignment: .leading, spacing: 16) {
                    Text("Settings")
                        .font(.title2)

                    VStack(alignment: .leading, spacing: 8) {
                        Text("Gateway Base URL")
                            .font(.headline)
                        TextField("http://localhost:8080", text: $model.baseURL)
                            .textFieldStyle(.roundedBorder)
                    }

                    VStack(alignment: .leading, spacing: 8) {
                        Text("API Key")
                            .font(.headline)
                        SecureField("X-API-Key", text: $model.apiKey)
                            .textFieldStyle(.roundedBorder)
                        Text("Stored in Keychain")
                            .font(.caption)
                            .foregroundColor(.secondary)
                    }

                    HStack(spacing: 12) {
                        Button("Save") {
                            model.saveSettings()
                            showSaved = true
                        }
                        Button("Test Connection") {
                            Task { await model.refreshStatus() }
                        }
                        Button("Reconnect WebSocket") {
                            model.reconnectWebSocket()
                        }
                    }

                    // WebSocket Connection Status
                    HStack(spacing: 12) {
                        HStack(spacing: 6) {
                            Circle()
                                .fill(model.isWebSocketConnected ? Color.green : Color.red)
                                .frame(width: 8, height: 8)
                            Text("WebSocket: \(model.isWebSocketConnected ? "Connected" : "Disconnected")")
                                .font(.caption)
                        }

                        if showSaved {
                            Text("Settings saved")
                                .font(.caption)
                                .foregroundColor(.green)
                        }
                    }

                    if let wsError = model.webSocketError {
                        HStack {
                            Image(systemName: "exclamationmark.triangle")
                            Text(wsError)
                        }
                        .font(.caption)
                        .foregroundColor(.orange)
                    }

                    if let error = model.lastError {
                        Text(error)
                            .foregroundColor(.red)
                    }
                }

                Divider()

                // Hotkey Preferences Section
                HotkeyPreferencesView(preferences: hotkeyPreferences)

                Divider()

                // Notification Preferences Section
                NotificationPreferencesView(preferences: notificationPreferences)
            }
            .padding()
        }
    }
}

// MARK: - Notification Preferences

@MainActor
class NotificationPreferencesModel: ObservableObject {
    @Published var categoryStates: [NotificationCategory: Bool] = [:]

    private let notificationService = NotificationService.shared

    init() {
        loadPreferences()
    }

    func loadPreferences() {
        for category in NotificationCategory.allCases {
            categoryStates[category] = notificationService.isEnabled(for: category)
        }
    }

    func setEnabled(_ enabled: Bool, for category: NotificationCategory) {
        categoryStates[category] = enabled
        notificationService.setEnabled(enabled, for: category)
    }

    func isEnabled(for category: NotificationCategory) -> Bool {
        categoryStates[category] ?? true
    }
}

struct NotificationPreferencesView: View {
    @ObservedObject var preferences: NotificationPreferencesModel
    @State private var permissionStatus: String = "Unknown"

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                Text("Notifications")
                    .font(.title2)
                Spacer()
                Text(permissionStatus)
                    .font(.caption)
                    .foregroundColor(.secondary)
            }

            Text("Choose which events trigger system notifications.")
                .font(.caption)
                .foregroundColor(.secondary)

            VStack(alignment: .leading, spacing: 12) {
                ForEach(NotificationCategory.allCases, id: \.rawValue) { category in
                    NotificationCategoryToggle(
                        category: category,
                        isEnabled: Binding(
                            get: { preferences.isEnabled(for: category) },
                            set: { preferences.setEnabled($0, for: category) }
                        )
                    )
                }
            }
            .padding(.top, 8)

            Button("Request Permission") {
                NotificationService.shared.requestPermission()
                Task {
                    try? await Task.sleep(nanoseconds: 500_000_000)
                    await updatePermissionStatus()
                }
            }
            .font(.caption)
        }
        .task {
            await updatePermissionStatus()
        }
    }

    private func updatePermissionStatus() async {
        let granted = await NotificationService.shared.checkPermissionStatus()
        permissionStatus = granted ? "Enabled" : "Disabled (click to enable)"
    }
}

struct NotificationCategoryToggle: View {
    let category: NotificationCategory
    @Binding var isEnabled: Bool

    var body: some View {
        Toggle(isOn: $isEnabled) {
            VStack(alignment: .leading, spacing: 2) {
                Text(category.displayName)
                    .font(.body)
                Text(category.description)
                    .font(.caption)
                    .foregroundColor(.secondary)
            }
        }
        .toggleStyle(.switch)
    }
}

// MARK: - Hotkey Preferences

@MainActor
class HotkeyPreferencesModel: ObservableObject {
    @Published var bindings: [HotkeyBinding] = []
    @Published var globalEnabled: Bool = true

    private let hotkeyManager = HotkeyManager.shared

    init() {
        loadPreferences()
    }

    func loadPreferences() {
        bindings = hotkeyManager.bindings
        globalEnabled = hotkeyManager.globalHotkeysEnabled
    }

    func setGlobalEnabled(_ enabled: Bool) {
        globalEnabled = enabled
        hotkeyManager.globalHotkeysEnabled = enabled
    }

    func setEnabled(_ enabled: Bool, for action: HotkeyAction) {
        hotkeyManager.setEnabled(enabled, for: action)
        loadPreferences()
    }

    func isEnabled(for action: HotkeyAction) -> Bool {
        bindings.first { $0.action == action }?.isEnabled ?? true
    }

    func binding(for action: HotkeyAction) -> HotkeyBinding? {
        bindings.first { $0.action == action }
    }

    func resetToDefaults() {
        hotkeyManager.resetToDefaults()
        loadPreferences()
    }

    func checkAccessibilityPermission() -> Bool {
        hotkeyManager.checkAccessibilityPermission()
    }

    func isAccessibilityGranted() -> Bool {
        hotkeyManager.isAccessibilityGranted()
    }
}

struct HotkeyPreferencesView: View {
    @ObservedObject var preferences: HotkeyPreferencesModel
    @State private var accessibilityStatus: String = "Unknown"
    @State private var showingResetConfirm: Bool = false

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                Text("Global Hotkeys")
                    .font(.title2)
                Spacer()
                Text(accessibilityStatus)
                    .font(.caption)
                    .foregroundColor(preferences.isAccessibilityGranted() ? .green : .orange)
            }

            Text("Use keyboard shortcuts to trigger computer use actions even when the app is not focused.")
                .font(.caption)
                .foregroundColor(.secondary)

            // Global enable toggle
            Toggle(isOn: Binding(
                get: { preferences.globalEnabled },
                set: { preferences.setGlobalEnabled($0) }
            )) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Enable Global Hotkeys")
                        .font(.body)
                    Text("When enabled, hotkeys work system-wide")
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
            }
            .toggleStyle(.switch)

            if !preferences.isAccessibilityGranted() {
                HStack {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .foregroundColor(.orange)
                    Text("Accessibility permission required for global hotkeys")
                        .font(.caption)
                    Spacer()
                    Button("Grant Permission") {
                        _ = preferences.checkAccessibilityPermission()
                        updateAccessibilityStatus()
                    }
                    .font(.caption)
                }
                .padding(8)
                .background(Color.orange.opacity(0.1))
                .cornerRadius(8)
            }

            Divider()

            // Hotkey bindings
            VStack(alignment: .leading, spacing: 12) {
                Text("Hotkey Bindings")
                    .font(.headline)

                ForEach(HotkeyAction.allCases) { action in
                    HotkeyBindingRow(
                        action: action,
                        binding: preferences.binding(for: action),
                        isEnabled: Binding(
                            get: { preferences.isEnabled(for: action) },
                            set: { preferences.setEnabled($0, for: action) }
                        ),
                        globalEnabled: preferences.globalEnabled
                    )
                }
            }
            .padding(.top, 8)

            HStack {
                Button("Reset to Defaults") {
                    showingResetConfirm = true
                }
                .font(.caption)

                Spacer()

                if let lastAction = HotkeyManager.shared.lastTriggeredAction {
                    Text("Last triggered: \(lastAction.displayName)")
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
            }
        }
        .task {
            updateAccessibilityStatus()
        }
        .alert("Reset Hotkeys", isPresented: $showingResetConfirm) {
            Button("Cancel", role: .cancel) {}
            Button("Reset", role: .destructive) {
                preferences.resetToDefaults()
            }
        } message: {
            Text("Reset all hotkey bindings to their default values?")
        }
    }

    private func updateAccessibilityStatus() {
        accessibilityStatus = preferences.isAccessibilityGranted() ? "Enabled" : "Permission Required"
    }
}

struct HotkeyBindingRow: View {
    let action: HotkeyAction
    let binding: HotkeyBinding?
    @Binding var isEnabled: Bool
    let globalEnabled: Bool

    var body: some View {
        HStack {
            Toggle(isOn: $isEnabled) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(action.displayName)
                        .font(.body)
                    Text(action.description)
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
            }
            .toggleStyle(.switch)
            .disabled(!globalEnabled)

            Spacer()

            if let binding = binding {
                Text(binding.keyDisplayString)
                    .font(.system(.body, design: .monospaced))
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(
                        RoundedRectangle(cornerRadius: 6)
                            .fill(Color(NSColor.controlBackgroundColor))
                    )
                    .overlay(
                        RoundedRectangle(cornerRadius: 6)
                            .stroke(Color.gray.opacity(0.3), lineWidth: 1)
                    )
                    .foregroundColor(isEnabled && globalEnabled ? .primary : .secondary)
            }
        }
    }
}
