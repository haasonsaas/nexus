import Foundation
import OSLog

/// Central application state store.
/// Manages persistent settings and runtime state.
@MainActor
@Observable
final class AppStateStore {
    static let shared = AppStateStore()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "state")
    private let defaults = UserDefaults.standard

    // MARK: - Connection Settings

    var connectionMode: ConnectionMode {
        didSet { save("connectionMode", connectionMode.rawValue) }
    }

    var isPaused: Bool {
        didSet { save("isPaused", isPaused) }
    }

    enum ConnectionMode: String, Codable {
        case local
        case remote
        case unconfigured
    }

    // MARK: - Gateway Settings

    var gatewayPort: Int {
        didSet { save("gatewayPort", gatewayPort) }
    }

    var gatewayAutostart: Bool {
        didSet { save("gatewayAutostart", gatewayAutostart) }
    }

    // MARK: - Remote Settings

    var remoteHost: String? {
        didSet { save("remoteHost", remoteHost ?? "") }
    }

    var remoteUser: String {
        didSet { save("remoteUser", remoteUser) }
    }

    var remoteIdentityFile: String? {
        didSet { save("remoteIdentityFile", remoteIdentityFile ?? "") }
    }

    // MARK: - Voice Settings

    var voiceWakeEnabled: Bool {
        didSet { save("voiceWakeEnabled", voiceWakeEnabled) }
    }

    var voiceWakeTriggers: [String] {
        didSet { save("voiceWakeTriggers", voiceWakeTriggers) }
    }

    var selectedMicrophone: String? {
        didSet { save("selectedMicrophone", selectedMicrophone ?? "") }
    }

    // MARK: - UI Settings

    var showDockIcon: Bool {
        didSet {
            save("showDockIcon", showDockIcon)
            applyDockIconSetting()
        }
    }

    var launchAtLogin: Bool {
        didSet { save("launchAtLogin", launchAtLogin) }
    }

    // MARK: - Feature Flags

    var nodeModeEnabled: Bool {
        didSet { save("nodeModeEnabled", nodeModeEnabled) }
    }

    var cameraEnabled: Bool {
        didSet { save("cameraEnabled", cameraEnabled) }
    }

    var channelsEnabled: Bool {
        didSet { save("channelsEnabled", channelsEnabled) }
    }

    // MARK: - Onboarding

    var hasCompletedOnboarding: Bool {
        didSet { save("hasCompletedOnboarding", hasCompletedOnboarding) }
    }

    // MARK: - Initialization

    private init() {
        // Load settings from UserDefaults
        connectionMode = ConnectionMode(rawValue: defaults.string(forKey: "connectionMode") ?? "") ?? .unconfigured
        isPaused = defaults.bool(forKey: "isPaused")

        gatewayPort = defaults.integer(forKey: "gatewayPort").nonZero ?? 3000
        gatewayAutostart = defaults.object(forKey: "gatewayAutostart") == nil ? true : defaults.bool(forKey: "gatewayAutostart")

        remoteHost = defaults.string(forKey: "remoteHost").nonEmpty
        remoteUser = defaults.string(forKey: "remoteUser").nonEmpty ?? "root"
        remoteIdentityFile = defaults.string(forKey: "remoteIdentityFile").nonEmpty

        voiceWakeEnabled = defaults.bool(forKey: "voiceWakeEnabled")
        voiceWakeTriggers = defaults.stringArray(forKey: "voiceWakeTriggers") ?? ["hey nexus"]
        selectedMicrophone = defaults.string(forKey: "selectedMicrophone").nonEmpty

        showDockIcon = defaults.bool(forKey: "showDockIcon")
        launchAtLogin = defaults.bool(forKey: "launchAtLogin")

        nodeModeEnabled = defaults.bool(forKey: "nodeModeEnabled")
        cameraEnabled = defaults.bool(forKey: "cameraEnabled")
        channelsEnabled = defaults.bool(forKey: "channelsEnabled")

        hasCompletedOnboarding = defaults.bool(forKey: "hasCompletedOnboarding")

        logger.debug("app state loaded")
    }

    // MARK: - Persistence

    private func save(_ key: String, _ value: Any) {
        defaults.set(value, forKey: key)
    }

    // MARK: - Actions

    private func applyDockIconSetting() {
        if showDockIcon {
            NSApp.setActivationPolicy(.regular)
        } else {
            NSApp.setActivationPolicy(.accessory)
        }
    }

    /// Reset all settings to defaults
    func resetToDefaults() {
        connectionMode = .unconfigured
        isPaused = false
        gatewayPort = 3000
        gatewayAutostart = true
        remoteHost = nil
        remoteUser = "root"
        remoteIdentityFile = nil
        voiceWakeEnabled = false
        voiceWakeTriggers = ["hey nexus"]
        selectedMicrophone = nil
        showDockIcon = false
        launchAtLogin = false
        nodeModeEnabled = false
        cameraEnabled = false
        channelsEnabled = false
        hasCompletedOnboarding = false

        logger.info("settings reset to defaults")
    }
}

// MARK: - Extensions

private extension Int {
    var nonZero: Int? {
        self == 0 ? nil : self
    }
}

private extension String? {
    var nonEmpty: String? {
        guard let self, !self.isEmpty else { return nil }
        return self
    }
}
