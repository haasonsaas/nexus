import Foundation
import Observation

// MARK: - Connection Mode

enum ConnectionMode: String, Codable, CaseIterable {
    case unconfigured
    case local
    case remote
}

// MARK: - Exec Approval Mode

enum ExecApprovalMode: String, CaseIterable, Identifiable {
    case prompt
    case approve
    case deny

    var id: String { rawValue }
}

// MARK: - App State Store

@MainActor
@Observable
final class AppStateStore {
    static let shared = AppStateStore()

    // MARK: - Connection

    var connectionMode: ConnectionMode = .unconfigured {
        didSet {
            if oldValue != connectionMode {
                UserDefaults.standard.set(connectionMode.rawValue, forKey: Keys.connectionMode)
            }
        }
    }

    var isPaused: Bool = false {
        didSet {
            if oldValue != isPaused {
                UserDefaults.standard.set(isPaused, forKey: Keys.isPaused)
            }
        }
    }

    // MARK: - Features

    var heartbeatsEnabled: Bool = true {
        didSet {
            if oldValue != heartbeatsEnabled {
                UserDefaults.standard.set(heartbeatsEnabled, forKey: Keys.heartbeatsEnabled)
            }
        }
    }

    var voiceWakeEnabled: Bool = false {
        didSet {
            if oldValue != voiceWakeEnabled {
                UserDefaults.standard.set(voiceWakeEnabled, forKey: Keys.voiceWakeEnabled)
            }
        }
    }

    var talkModeEnabled: Bool = false {
        didSet {
            if oldValue != talkModeEnabled {
                UserDefaults.standard.set(talkModeEnabled, forKey: Keys.talkModeEnabled)
            }
        }
    }

    var canvasEnabled: Bool = true {
        didSet {
            if oldValue != canvasEnabled {
                UserDefaults.standard.set(canvasEnabled, forKey: Keys.canvasEnabled)
            }
        }
    }

    var execApprovalMode: ExecApprovalMode = .prompt {
        didSet {
            if oldValue != execApprovalMode {
                UserDefaults.standard.set(execApprovalMode.rawValue, forKey: Keys.execApprovalMode)
            }
        }
    }

    // MARK: - Onboarding

    var onboardingSeen: Bool = false {
        didSet {
            if oldValue != onboardingSeen {
                UserDefaults.standard.set(onboardingSeen, forKey: Keys.onboardingSeen)
            }
        }
    }

    // MARK: - Debug

    var debugPaneEnabled: Bool = false {
        didSet {
            if oldValue != debugPaneEnabled {
                UserDefaults.standard.set(debugPaneEnabled, forKey: Keys.debugPaneEnabled)
            }
        }
    }

    // MARK: - Voice

    var voiceWakeMicID: String = "" {
        didSet {
            if oldValue != voiceWakeMicID {
                UserDefaults.standard.set(voiceWakeMicID, forKey: Keys.voiceWakeMicID)
            }
        }
    }

    var voiceWakeMicName: String = "" {
        didSet {
            if oldValue != voiceWakeMicName {
                UserDefaults.standard.set(voiceWakeMicName, forKey: Keys.voiceWakeMicName)
            }
        }
    }

    var voiceWakeLocaleID: String = "en-US" {
        didSet {
            if oldValue != voiceWakeLocaleID {
                UserDefaults.standard.set(voiceWakeLocaleID, forKey: Keys.voiceWakeLocaleID)
            }
        }
    }

    // MARK: - Keys

    private enum Keys {
        static let connectionMode = "AppState_connectionMode"
        static let isPaused = "AppState_isPaused"
        static let heartbeatsEnabled = "AppState_heartbeatsEnabled"
        static let voiceWakeEnabled = "AppState_voiceWakeEnabled"
        static let talkModeEnabled = "AppState_talkModeEnabled"
        static let canvasEnabled = "AppState_canvasEnabled"
        static let execApprovalMode = "AppState_execApprovalMode"
        static let onboardingSeen = "AppState_onboardingSeen"
        static let debugPaneEnabled = "AppState_debugPaneEnabled"
        static let voiceWakeMicID = "AppState_voiceWakeMicID"
        static let voiceWakeMicName = "AppState_voiceWakeMicName"
        static let voiceWakeLocaleID = "AppState_voiceWakeLocaleID"
    }

    // MARK: - Initialization

    private init() {
        loadFromDefaults()
    }

    private func loadFromDefaults() {
        let defaults = UserDefaults.standard

        if let modeString = defaults.string(forKey: Keys.connectionMode),
           let mode = ConnectionMode(rawValue: modeString) {
            connectionMode = mode
        }

        isPaused = defaults.bool(forKey: Keys.isPaused)

        // Features - use registered defaults for booleans that default to true
        heartbeatsEnabled = defaults.object(forKey: Keys.heartbeatsEnabled) as? Bool ?? true
        voiceWakeEnabled = defaults.bool(forKey: Keys.voiceWakeEnabled)
        talkModeEnabled = defaults.bool(forKey: Keys.talkModeEnabled)
        canvasEnabled = defaults.object(forKey: Keys.canvasEnabled) as? Bool ?? true

        if let approvalString = defaults.string(forKey: Keys.execApprovalMode),
           let approvalMode = ExecApprovalMode(rawValue: approvalString) {
            execApprovalMode = approvalMode
        }

        onboardingSeen = defaults.bool(forKey: Keys.onboardingSeen)
        debugPaneEnabled = defaults.bool(forKey: Keys.debugPaneEnabled)

        voiceWakeMicID = defaults.string(forKey: Keys.voiceWakeMicID) ?? ""
        voiceWakeMicName = defaults.string(forKey: Keys.voiceWakeMicName) ?? ""
        voiceWakeLocaleID = defaults.string(forKey: Keys.voiceWakeLocaleID) ?? "en-US"
    }

    // MARK: - Voice Wake Methods

    func setVoiceWakeEnabled(_ enabled: Bool) async {
        voiceWakeEnabled = enabled

        if enabled {
            let config = VoiceWakeRuntime.RuntimeConfig(
                triggers: ["hey nexus", "nexus"],
                micID: voiceWakeMicID.isEmpty ? nil : voiceWakeMicID,
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
