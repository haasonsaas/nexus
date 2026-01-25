import Foundation
import OSLog

/// Observable wrapper around VoiceWakeRuntime actor for UI binding.
/// Provides @MainActor methods to integrate with VoiceWakeOverlayController.
@MainActor
@Observable
final class VoiceWakeOverlayRuntime {
    static let shared = VoiceWakeOverlayRuntime()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "voice-wake-overlay-runtime")

    // MARK: - Observable State

    /// Whether voice wake is currently listening for the wake word
    private(set) var isListening = false

    /// Current transcript from speech recognition
    private(set) var transcript = ""

    /// Whether the current transcript is final
    private(set) var isFinal = false

    /// Current audio level (0-1)
    private(set) var audioLevel: Float = 0

    /// Whether voice wake is currently capturing a command
    private(set) var isCapturing = false

    // MARK: - Initialization

    private init() {}

    // MARK: - Voice Wake Control

    /// Start listening for wake word
    func startListening() {
        Task {
            let appState = AppStateStore.shared
            let config = VoiceWakeRuntime.RuntimeConfig(
                triggers: appState.voiceWakeTriggers,
                micID: appState.voiceWakeMicID.isEmpty ? appState.selectedMicrophone : appState.voiceWakeMicID,
                localeID: appState.voiceWakeLocaleID.isEmpty ? nil : appState.voiceWakeLocaleID,
                triggerChime: .subtle,
                sendChime: .standard
            )
            await VoiceWakeRuntime.shared.start(with: config)
            isListening = true
            logger.info("Voice wake listening started")
        }
    }

    /// Stop listening for wake word
    func stopListening() {
        Task {
            await VoiceWakeRuntime.shared.stop()
            isListening = false
            transcript = ""
            isFinal = false
            audioLevel = 0
            isCapturing = false
            logger.info("Voice wake listening stopped")
        }
    }

    // MARK: - Voice Wake Actions

    /// Start voice wake capture (show overlay)
    func startVoiceWake() {
        isCapturing = true
        transcript = ""
        isFinal = false
        VoiceWakeOverlayController.shared.show(source: .wakeWord)
        logger.info("Voice wake capture started")
    }

    /// Cancel voice wake capture (hide overlay)
    func cancelVoiceWake() {
        isCapturing = false
        transcript = ""
        isFinal = false
        VoiceWakeOverlayController.shared.hide()
        logger.info("Voice wake capture cancelled")
    }

    /// Update transcript from speech recognition
    func updateTranscript(_ text: String, isFinal: Bool) {
        transcript = text
        self.isFinal = isFinal
        VoiceWakeOverlayController.shared.updateText(text, isFinal: isFinal)
    }

    /// Update audio level
    func updateAudioLevel(_ level: Float) {
        audioLevel = level
        VoiceWakeOverlayController.shared.updateLevel(Double(level))
    }

    /// Send the voice wake result
    func sendVoiceWakeResult(_ text: String) {
        guard !text.isEmpty else { return }

        logger.info("Sending voice wake result: \(text)")

        // Let the overlay controller handle sending
        VoiceWakeOverlayController.shared.updateText(text, isFinal: true)
        VoiceWakeOverlayController.shared.requestSend()

        // Clean up
        isCapturing = false
        transcript = ""
        isFinal = false
    }

    // MARK: - Push to Talk

    /// Start push-to-talk capture
    func startPushToTalk() {
        isCapturing = true
        transcript = ""
        isFinal = false
        VoiceWakeOverlayController.shared.show(source: .pushToTalk)
        logger.info("Push-to-talk started")
    }

    /// End push-to-talk capture
    func endPushToTalk() {
        if !transcript.isEmpty && isFinal {
            sendVoiceWakeResult(transcript)
        } else {
            cancelVoiceWake()
        }
        logger.info("Push-to-talk ended")
    }
}
