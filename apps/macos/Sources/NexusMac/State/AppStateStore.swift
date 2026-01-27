import AppKit
import Foundation
import Observation
import OSLog

// MARK: - Enums

/// Connection mode for gateway communication
enum ConnectionMode: String, Codable, CaseIterable {
    case unconfigured
    case local
    case remote
}

/// Execution approval mode for agent actions
enum ExecApprovalMode: String, Codable, CaseIterable, Identifiable {
    case prompt
    case approve
    case deny

    var id: String { rawValue }
}

// MARK: - App State Store

/// Central application state store.
/// Manages persistent settings and runtime state.
/// This is the single source of truth for all app configuration.
@MainActor
@Observable
final class AppStateStore {
    static let shared = AppStateStore()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "state")
    private let defaults = UserDefaults.standard

    // MARK: - Connection Settings

    var connectionMode: ConnectionMode = .unconfigured {
        didSet {
            if oldValue != connectionMode {
                save(Keys.connectionMode, connectionMode.rawValue)
            }
        }
    }

    var isPaused: Bool = false {
        didSet {
            if oldValue != isPaused {
                save(Keys.isPaused, isPaused)
            }
        }
    }

    // MARK: - Gateway Settings

    var gatewayPort: Int = 8080 {
        didSet {
            if oldValue != gatewayPort {
                save(Keys.gatewayPort, gatewayPort)
            }
        }
    }

    var gatewayAutostart: Bool = true {
        didSet {
            if oldValue != gatewayAutostart {
                save(Keys.gatewayAutostart, gatewayAutostart)
            }
        }
    }

    var gatewayUseTLS: Bool = false {
        didSet {
            if oldValue != gatewayUseTLS {
                save(Keys.gatewayUseTLS, gatewayUseTLS)
            }
        }
    }

    // MARK: - Remote Settings

    var remoteHost: String? {
        didSet {
            if oldValue != remoteHost {
                save(Keys.remoteHost, remoteHost ?? "")
            }
        }
    }

    var remoteUser: String = "root" {
        didSet {
            if oldValue != remoteUser {
                save(Keys.remoteUser, remoteUser)
            }
        }
    }

    var remoteIdentityFile: String? {
        didSet {
            if oldValue != remoteIdentityFile {
                save(Keys.remoteIdentityFile, remoteIdentityFile ?? "")
            }
        }
    }

    // MARK: - Voice Settings

    var voiceWakeEnabled: Bool = false {
        didSet {
            if oldValue != voiceWakeEnabled {
                save(Keys.voiceWakeEnabled, voiceWakeEnabled)
            }
        }
    }

    var voiceWakeTriggers: [String] = ["hey nexus"] {
        didSet {
            if oldValue != voiceWakeTriggers {
                save(Keys.voiceWakeTriggers, voiceWakeTriggers)
            }
        }
    }

    var selectedMicrophone: String? {
        didSet {
            if oldValue != selectedMicrophone {
                save(Keys.selectedMicrophone, selectedMicrophone ?? "")
            }
        }
    }

    var voiceWakeMicID: String = "" {
        didSet {
            if oldValue != voiceWakeMicID {
                save(Keys.voiceWakeMicID, voiceWakeMicID)
            }
        }
    }

    var voiceWakeMicName: String = "" {
        didSet {
            if oldValue != voiceWakeMicName {
                save(Keys.voiceWakeMicName, voiceWakeMicName)
            }
        }
    }

    var voiceWakeLocaleID: String = "en-US" {
        didSet {
            if oldValue != voiceWakeLocaleID {
                save(Keys.voiceWakeLocaleID, voiceWakeLocaleID)
            }
        }
    }

    var talkModeEnabled: Bool = false {
        didSet {
            if oldValue != talkModeEnabled {
                save(Keys.talkModeEnabled, talkModeEnabled)
            }
        }
    }

    // MARK: - Feature Flags

    var heartbeatsEnabled: Bool = true {
        didSet {
            if oldValue != heartbeatsEnabled {
                save(Keys.heartbeatsEnabled, heartbeatsEnabled)
            }
        }
    }

    var canvasEnabled: Bool = true {
        didSet {
            if oldValue != canvasEnabled {
                save(Keys.canvasEnabled, canvasEnabled)
            }
        }
    }

    var nodeModeEnabled: Bool = false {
        didSet {
            if oldValue != nodeModeEnabled {
                save(Keys.nodeModeEnabled, nodeModeEnabled)
            }
        }
    }

    var cameraEnabled: Bool = false {
        didSet {
            if oldValue != cameraEnabled {
                save(Keys.cameraEnabled, cameraEnabled)
            }
        }
    }

    var channelsEnabled: Bool = false {
        didSet {
            if oldValue != channelsEnabled {
                save(Keys.channelsEnabled, channelsEnabled)
            }
        }
    }

    // MARK: - Security Settings

    var execApprovalMode: ExecApprovalMode = .prompt {
        didSet {
            if oldValue != execApprovalMode {
                save(Keys.execApprovalMode, execApprovalMode.rawValue)
            }
        }
    }

    // MARK: - UI Settings

    var showDockIcon: Bool = false {
        didSet {
            if oldValue != showDockIcon {
                save(Keys.showDockIcon, showDockIcon)
                applyDockIconSetting()
            }
        }
    }

    var launchAtLogin: Bool = false {
        didSet {
            if oldValue != launchAtLogin {
                save(Keys.launchAtLogin, launchAtLogin)
                applyLaunchAtLoginSetting()
            }
        }
    }

    var debugPaneEnabled: Bool = false {
        didSet {
            if oldValue != debugPaneEnabled {
                save(Keys.debugPaneEnabled, debugPaneEnabled)
            }
        }
    }

    // MARK: - Onboarding

    var hasCompletedOnboarding: Bool = false {
        didSet {
            if oldValue != hasCompletedOnboarding {
                save(Keys.hasCompletedOnboarding, hasCompletedOnboarding)
            }
        }
    }

    /// Alias for backward compatibility
    var onboardingSeen: Bool {
        get { hasCompletedOnboarding }
        set { hasCompletedOnboarding = newValue }
    }

    // MARK: - Keys

    private enum Keys {
        static let connectionMode = "connectionMode"
        static let isPaused = "isPaused"
        static let gatewayPort = "gatewayPort"
        static let gatewayAutostart = "gatewayAutostart"
        static let gatewayUseTLS = "gatewayUseTLS"
        static let remoteHost = "remoteHost"
        static let remoteUser = "remoteUser"
        static let remoteIdentityFile = "remoteIdentityFile"
        static let voiceWakeEnabled = "voiceWakeEnabled"
        static let voiceWakeTriggers = "voiceWakeTriggers"
        static let selectedMicrophone = "selectedMicrophone"
        static let voiceWakeMicID = "voiceWakeMicID"
        static let voiceWakeMicName = "voiceWakeMicName"
        static let voiceWakeLocaleID = "voiceWakeLocaleID"
        static let talkModeEnabled = "talkModeEnabled"
        static let heartbeatsEnabled = "heartbeatsEnabled"
        static let canvasEnabled = "canvasEnabled"
        static let nodeModeEnabled = "nodeModeEnabled"
        static let cameraEnabled = "cameraEnabled"
        static let channelsEnabled = "channelsEnabled"
        static let execApprovalMode = "execApprovalMode"
        static let showDockIcon = "showDockIcon"
        static let launchAtLogin = "launchAtLogin"
        static let debugPaneEnabled = "debugPaneEnabled"
        static let hasCompletedOnboarding = "hasCompletedOnboarding"
    }

    // MARK: - Initialization

    private init() {
        loadFromDefaults()
        logger.debug("app state loaded")
    }

    private func loadFromDefaults() {
        // Connection
        if let modeString = defaults.string(forKey: Keys.connectionMode),
           let mode = ConnectionMode(rawValue: modeString) {
            connectionMode = mode
        }
        isPaused = defaults.bool(forKey: Keys.isPaused)

        // Gateway
        gatewayPort = defaults.integer(forKey: Keys.gatewayPort).nonZero ?? 8080
        gatewayAutostart = defaults.object(forKey: Keys.gatewayAutostart) == nil ? true : defaults.bool(forKey: Keys.gatewayAutostart)
        gatewayUseTLS = defaults.bool(forKey: Keys.gatewayUseTLS)

        // Remote
        remoteHost = defaults.string(forKey: Keys.remoteHost).nonEmpty
        remoteUser = defaults.string(forKey: Keys.remoteUser).nonEmpty ?? "root"
        remoteIdentityFile = defaults.string(forKey: Keys.remoteIdentityFile).nonEmpty

        // Voice
        voiceWakeEnabled = defaults.bool(forKey: Keys.voiceWakeEnabled)
        voiceWakeTriggers = defaults.stringArray(forKey: Keys.voiceWakeTriggers) ?? ["hey nexus"]
        selectedMicrophone = defaults.string(forKey: Keys.selectedMicrophone).nonEmpty
        voiceWakeMicID = defaults.string(forKey: Keys.voiceWakeMicID) ?? ""
        voiceWakeMicName = defaults.string(forKey: Keys.voiceWakeMicName) ?? ""
        voiceWakeLocaleID = defaults.string(forKey: Keys.voiceWakeLocaleID) ?? "en-US"
        talkModeEnabled = defaults.bool(forKey: Keys.talkModeEnabled)

        // Features
        heartbeatsEnabled = defaults.object(forKey: Keys.heartbeatsEnabled) as? Bool ?? true
        canvasEnabled = defaults.object(forKey: Keys.canvasEnabled) as? Bool ?? true
        nodeModeEnabled = defaults.bool(forKey: Keys.nodeModeEnabled)
        cameraEnabled = defaults.bool(forKey: Keys.cameraEnabled)
        channelsEnabled = defaults.bool(forKey: Keys.channelsEnabled)

        // Security
        if let approvalString = defaults.string(forKey: Keys.execApprovalMode),
           let approvalMode = ExecApprovalMode(rawValue: approvalString) {
            execApprovalMode = approvalMode
        }

        // UI
        showDockIcon = defaults.bool(forKey: Keys.showDockIcon)
        launchAtLogin = defaults.bool(forKey: Keys.launchAtLogin)
        debugPaneEnabled = defaults.bool(forKey: Keys.debugPaneEnabled)

        // Onboarding
        hasCompletedOnboarding = defaults.bool(forKey: Keys.hasCompletedOnboarding)

        applyLaunchAtLoginSetting()
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

    private func applyLaunchAtLoginSetting() {
        LaunchAtLoginManager.shared.applyPreference(launchAtLogin)
    }

    /// Reset all settings to defaults
    func resetToDefaults() {
        connectionMode = .unconfigured
        isPaused = false
        gatewayPort = 8080
        gatewayAutostart = true
        gatewayUseTLS = false
        remoteHost = nil
        remoteUser = "root"
        remoteIdentityFile = nil
        voiceWakeEnabled = false
        voiceWakeTriggers = ["hey nexus"]
        selectedMicrophone = nil
        voiceWakeMicID = ""
        voiceWakeMicName = ""
        voiceWakeLocaleID = "en-US"
        talkModeEnabled = false
        heartbeatsEnabled = true
        canvasEnabled = true
        nodeModeEnabled = false
        cameraEnabled = false
        channelsEnabled = false
        execApprovalMode = .prompt
        showDockIcon = false
        launchAtLogin = false
        debugPaneEnabled = false
        hasCompletedOnboarding = false

        logger.info("settings reset to defaults")
    }

    // MARK: - Voice Wake Methods

    func setVoiceWakeEnabled(_ enabled: Bool) async {
        voiceWakeEnabled = enabled

        if enabled {
            let config = VoiceWakeRuntime.RuntimeConfig(
                triggers: voiceWakeTriggers,
                micID: voiceWakeMicID.isEmpty ? selectedMicrophone : voiceWakeMicID,
                localeID: voiceWakeLocaleID.isEmpty ? nil : voiceWakeLocaleID,
                triggerChime: .subtle,
                sendChime: .standard
            )
            await VoiceWakeRuntime.shared.start(with: config)
        } else {
            await VoiceWakeRuntime.shared.stop()
        }
    }

    // MARK: - Talk Mode Methods

    func setTalkModeEnabled(_ enabled: Bool) async {
        talkModeEnabled = enabled
        await TalkModeController.shared.setEnabled(enabled)
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
