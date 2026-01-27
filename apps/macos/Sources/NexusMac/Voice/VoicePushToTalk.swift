import AppKit
import AVFoundation
import Foundation
import OSLog
import Speech

// MARK: - PTT State

/// State of the push-to-talk system.
enum PTTState: String, Sendable {
    case idle
    case listening
    case processing
    case speaking
}

// MARK: - PTT Key Binding

/// Configurable key binding for push-to-talk.
struct PTTKeyBinding: Codable, Equatable, Sendable {
    let keyCode: UInt16
    let modifiers: NSEvent.ModifierFlags.RawValue

    static let defaultBinding = PTTKeyBinding(keyCode: 61, modifiers: 0) // Right Option

    var displayName: String {
        var parts: [String] = []
        let flags = NSEvent.ModifierFlags(rawValue: modifiers)
        if flags.contains(.control) { parts.append("^") }
        if flags.contains(.option) { parts.append("\u{2325}") }
        if flags.contains(.shift) { parts.append("\u{21E7}") }
        if flags.contains(.command) { parts.append("\u{2318}") }

        let keyName: String
        switch keyCode {
        case 61: keyName = "Right Option"
        case 58: keyName = "Left Option"
        case 59: keyName = "Left Control"
        case 62: keyName = "Right Control"
        case 56: keyName = "Left Shift"
        case 60: keyName = "Right Shift"
        case 55: keyName = "Left Command"
        case 54: keyName = "Right Command"
        case 49: keyName = "Space"
        default: keyName = "Key \(keyCode)"
        }

        if parts.isEmpty {
            return keyName
        }
        return parts.joined() + " " + keyName
    }
}

// MARK: - VoicePushToTalk

/// Main push-to-talk controller.
/// Manages hotkey registration, audio capture, and gateway communication.
@MainActor
@Observable
final class VoicePushToTalk {
    static let shared = VoicePushToTalk()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "voice")

    // MARK: - Observable State

    private(set) var state: PTTState = .idle
    private(set) var isEnabled: Bool = false
    private(set) var audioLevel: Double = 0
    private(set) var currentTranscript: String = ""
    private(set) var keyBinding: PTTKeyBinding = .defaultBinding

    // MARK: - Private State

    private var pttHotkey: VoicePushToTalkHotkey?
    private var captureTask: Task<Void, Never>?

    private init() {}

    // MARK: - Public API

    /// Enable or disable push-to-talk.
    func setEnabled(_ enabled: Bool) {
        guard enabled != isEnabled else { return }
        logger.info("ptt enabled=\(enabled)")
        isEnabled = enabled

        if enabled {
            startMonitoring()
        } else {
            stopMonitoring()
        }
    }

    /// Update the PTT key binding.
    func setKeyBinding(_ binding: PTTKeyBinding) {
        logger.info("ptt keyBinding=\(binding.keyCode) modifiers=\(binding.modifiers)")
        keyBinding = binding

        // Restart monitoring with new binding if enabled
        if isEnabled {
            stopMonitoring()
            startMonitoring()
        }
    }

    /// Force stop any active PTT session.
    func forceStop() {
        Task {
            await VoicePushToTalkCapture.shared.end()
        }
        state = .idle
        audioLevel = 0
        currentTranscript = ""
    }

    // MARK: - Internal Callbacks (from Hotkey)

    func hotkeyDown() {
        guard isEnabled else { return }
        guard state == .idle else { return }

        logger.info("ptt hotkey down")
        state = .listening
        currentTranscript = ""

        captureTask = Task {
            await beginCapture()
        }
    }

    func hotkeyUp() {
        guard state == .listening else { return }

        logger.info("ptt hotkey up")
        state = .processing

        Task {
            await endCapture()
        }
    }

    // MARK: - State Updates (from Capture)

    func updateTranscript(_ text: String) {
        currentTranscript = text
    }

    func updateAudioLevel(_ level: Double) {
        audioLevel = max(0, min(1, level))
    }

    func transitionToSpeaking() {
        state = .speaking
    }

    func transitionToIdle() {
        state = .idle
        audioLevel = 0
    }

    // MARK: - Private Methods

    private func startMonitoring() {
        pttHotkey = VoicePushToTalkHotkey(
            keyCode: keyBinding.keyCode,
            modifiers: NSEvent.ModifierFlags(rawValue: keyBinding.modifiers),
            onDown: { [weak self] in
                Task { @MainActor in
                    self?.hotkeyDown()
                }
            },
            onUp: { [weak self] in
                Task { @MainActor in
                    self?.hotkeyUp()
                }
            }
        )
        pttHotkey?.setEnabled(true)
    }

    private func stopMonitoring() {
        pttHotkey?.setEnabled(false)
        pttHotkey = nil
        forceStop()
    }

    private func beginCapture() async {
        // Request coordinator permission
        let granted = VoiceSessionCoordinator.shared.requestSession(mode: .pushToTalk)
        guard granted else {
            logger.warning("ptt session denied by coordinator")
            await MainActor.run { state = .idle }
            return
        }

        await VoicePushToTalkCapture.shared.begin()
    }

    private func endCapture() async {
        await VoicePushToTalkCapture.shared.end()
    }
}

// MARK: - VoicePushToTalkHotkey

/// Monitors keyboard events for the PTT hotkey.
final class VoicePushToTalkHotkey: @unchecked Sendable {
    private let keyCode: UInt16
    private let modifiers: NSEvent.ModifierFlags
    private let onDown: @Sendable () -> Void
    private let onUp: @Sendable () -> Void

    private var globalMonitor: Any?
    private var localMonitor: Any?
    private var isKeyDown = false
    private var isActive = false

    private let logger = Logger(subsystem: "com.nexus.mac", category: "voice")

    init(
        keyCode: UInt16,
        modifiers: NSEvent.ModifierFlags,
        onDown: @escaping @Sendable () -> Void,
        onUp: @escaping @Sendable () -> Void
    ) {
        self.keyCode = keyCode
        self.modifiers = modifiers
        self.onDown = onDown
        self.onUp = onUp
    }

    func setEnabled(_ enabled: Bool) {
        if ProcessInfo.processInfo.environment["XCTestConfigurationFilePath"] != nil { return }
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            if enabled {
                self.startMonitoring()
            } else {
                self.stopMonitoring()
            }
        }
    }

    private func startMonitoring() {
        guard globalMonitor == nil, localMonitor == nil else { return }

        // Global monitor for when app is not focused
        globalMonitor = NSEvent.addGlobalMonitorForEvents(matching: [.flagsChanged, .keyDown, .keyUp]) { [weak self] event in
            self?.handleEvent(event)
        }

        // Local monitor for when app is focused
        localMonitor = NSEvent.addLocalMonitorForEvents(matching: [.flagsChanged, .keyDown, .keyUp]) { [weak self] event in
            self?.handleEvent(event)
            return event
        }

        logger.debug("ptt hotkey monitoring started keyCode=\(self.keyCode)")
    }

    private func stopMonitoring() {
        if let globalMonitor {
            NSEvent.removeMonitor(globalMonitor)
            self.globalMonitor = nil
        }
        if let localMonitor {
            NSEvent.removeMonitor(localMonitor)
            self.localMonitor = nil
        }
        isKeyDown = false
        isActive = false

        logger.debug("ptt hotkey monitoring stopped")
    }

    private func handleEvent(_ event: NSEvent) {
        DispatchQueue.main.async { [weak self] in
            self?.processEvent(event)
        }
    }

    private func processEvent(_ event: NSEvent) {
        // Handle modifier keys (Option, Control, etc.)
        if event.type == .flagsChanged {
            handleFlagsChanged(event)
            return
        }

        // Handle regular keys
        guard event.keyCode == keyCode else { return }

        let requiredModifiersPresent = modifiers.isEmpty || event.modifierFlags.contains(modifiers)
        guard requiredModifiersPresent else { return }

        if event.type == .keyDown, !isKeyDown {
            isKeyDown = true
            triggerDown()
        } else if event.type == .keyUp, isKeyDown {
            isKeyDown = false
            triggerUp()
        }
    }

    private func handleFlagsChanged(keyCode: UInt16, modifierFlags: NSEvent.ModifierFlags) {
        // Check if this is our modifier key
        let isModifierKey: Bool
        switch keyCode {
        case 58, 61: // Left/Right Option
            isModifierKey = self.keyCode == keyCode || (self.keyCode == 58 || self.keyCode == 61)
        case 59, 62: // Left/Right Control
            isModifierKey = self.keyCode == keyCode || (self.keyCode == 59 || self.keyCode == 62)
        case 56, 60: // Left/Right Shift
            isModifierKey = self.keyCode == keyCode || (self.keyCode == 56 || self.keyCode == 60)
        case 55, 54: // Left/Right Command
            isModifierKey = self.keyCode == keyCode || (self.keyCode == 55 || self.keyCode == 54)
        default:
            isModifierKey = false
        }

        guard isModifierKey else { return }

        // Determine if the modifier is currently pressed
        let isPressed: Bool
        switch keyCode {
        case 58, 61: // Option
            isPressed = modifierFlags.contains(.option)
        case 59, 62: // Control
            isPressed = modifierFlags.contains(.control)
        case 56, 60: // Shift
            isPressed = modifierFlags.contains(.shift)
        case 55, 54: // Command
            isPressed = modifierFlags.contains(.command)
        default:
            isPressed = false
        }

        if isPressed, !isActive {
            isActive = true
            triggerDown()
        } else if !isPressed, isActive {
            isActive = false
            triggerUp()
        }
    }

    private func handleFlagsChanged(_ event: NSEvent) {
        handleFlagsChanged(keyCode: event.keyCode, modifierFlags: event.modifierFlags)
    }

    private func triggerDown() {
        onDown()
    }

    private func triggerUp() {
        onUp()
    }
}

// MARK: - VoicePushToTalkCapture

/// Actor that handles audio capture and speech recognition for PTT.
actor VoicePushToTalkCapture {
    static let shared = VoicePushToTalkCapture()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "voice")

    private var recognizer: SFSpeechRecognizer?
    private var audioEngine: AVAudioEngine?
    private var recognitionRequest: SFSpeechAudioBufferRecognitionRequest?
    private var recognitionTask: SFSpeechRecognitionTask?
    private var tapInstalled = false

    private var sessionID = UUID()
    private var committed: String = ""
    private var volatile: String = ""
    private var isCapturing = false
    private var finalized = false
    private var timeoutTask: Task<Void, Never>?

    private var noiseFloorRMS: Double = 1e-4
    private let minSpeechRMS: Double = 1e-3
    private let speechBoostFactor: Double = 6.0

    // MARK: - Public API

    func begin() async {
        guard !isCapturing else { return }

        // Ensure permissions
        let permManager = await MainActor.run { PermissionManager.shared }
        let hasPermissions = await MainActor.run { permManager.voiceWakePermissionsGranted }
        guard hasPermissions else {
            logger.warning("ptt missing permissions")
            return
        }

        // Start a fresh session
        sessionID = UUID()
        isCapturing = true
        finalized = false
        committed = ""
        volatile = ""
        timeoutTask?.cancel()
        timeoutTask = nil

        logger.info("ptt capture begin sessionID=\(self.sessionID.uuidString.prefix(8))")

        // Play trigger chime
        await MainActor.run {
            VoiceWakeChimePlayer.play(.subtle)
        }

        // Pause wake word runtime if active
        await VoiceWakeRuntime.shared.pauseForPushToTalk()

        // Show overlay
        await MainActor.run {
            VoiceWakeOverlayController.shared.show(source: .pushToTalk)
        }

        do {
            try await startRecognition()
        } catch {
            logger.error("ptt recognition failed: \(error.localizedDescription)")
            await cleanup()
        }
    }

    func end() async {
        guard isCapturing else { return }
        isCapturing = false

        let currentSessionID = sessionID
        logger.info("ptt capture end")

        // Stop feeding buffers
        if tapInstalled {
            audioEngine?.inputNode.removeTap(onBus: 0)
            tapInstalled = false
        }
        recognitionRequest?.endAudio()

        // If empty, dismiss immediately
        if committed.isEmpty, volatile.isEmpty {
            await finalize(transcriptOverride: "", reason: "emptyOnRelease", sessionID: currentSessionID)
            return
        }

        // Give Speech a grace period for final result
        timeoutTask?.cancel()
        timeoutTask = Task { [weak self] in
            try? await Task.sleep(nanoseconds: 1_500_000_000)
            await self?.finalize(transcriptOverride: nil, reason: "timeout", sessionID: currentSessionID)
        }
    }

    // MARK: - Private Methods

    private func startRecognition() async throws {
        let locale = Locale.current
        recognizer = SFSpeechRecognizer(locale: locale)
        guard let recognizer, recognizer.isAvailable else {
            throw NSError(domain: "VoicePushToTalk", code: 1, userInfo: [
                NSLocalizedDescriptionKey: "Speech recognizer unavailable",
            ])
        }

        recognitionRequest = SFSpeechAudioBufferRecognitionRequest()
        recognitionRequest?.shouldReportPartialResults = true
        guard let request = recognitionRequest else { return }

        // Lazily create audio engine
        if audioEngine == nil {
            audioEngine = AVAudioEngine()
        }
        guard let audioEngine else { return }

        let input = audioEngine.inputNode
        let format = input.outputFormat(forBus: 0)
        guard format.channelCount > 0, format.sampleRate > 0 else {
            throw NSError(domain: "VoicePushToTalk", code: 2, userInfo: [
                NSLocalizedDescriptionKey: "No audio input available",
            ])
        }

        if tapInstalled {
            input.removeTap(onBus: 0)
            tapInstalled = false
        }

        // Install audio tap
        input.installTap(onBus: 0, bufferSize: 2048, format: format) { [weak self, weak request] buffer, _ in
            request?.append(buffer)
            guard let rms = Self.rmsLevel(buffer: buffer) else { return }
            Task { await self?.noteAudioLevel(rms: rms) }
        }
        tapInstalled = true

        audioEngine.prepare()
        try audioEngine.start()

        let currentSessionID = sessionID
        recognitionTask = recognizer.recognitionTask(with: request) { [weak self] result, error in
            guard let self else { return }
            if let error {
                self.logger.debug("ptt recognition error: \(error.localizedDescription)")
            }
            let transcript = result?.bestTranscription.formattedString
            let isFinal = result?.isFinal ?? false
            Task.detached {
                await self.handle(transcript: transcript, isFinal: isFinal, sessionID: currentSessionID)
            }
        }
    }

    private func handle(transcript: String?, isFinal: Bool, sessionID: UUID) async {
        guard sessionID == self.sessionID else { return }
        guard let transcript else { return }

        if isFinal {
            committed = transcript
            volatile = ""
            timeoutTask?.cancel()
            await finalize(transcriptOverride: nil, reason: "final", sessionID: sessionID)
        } else {
            volatile = Self.delta(after: committed, current: transcript)
        }

        let snapshot = Self.join(committed, volatile)
        await MainActor.run {
            VoicePushToTalk.shared.updateTranscript(snapshot)
            VoiceWakeOverlayController.shared.updateText(snapshot, isFinal: isFinal)
        }
    }

    private func finalize(transcriptOverride: String?, reason: String, sessionID: UUID?) async {
        if finalized { return }
        if let sessionID, sessionID != self.sessionID { return }
        finalized = true
        isCapturing = false
        timeoutTask?.cancel()
        timeoutTask = nil

        let finalText: String = {
            if let override = transcriptOverride?.trimmingCharacters(in: .whitespacesAndNewlines) {
                return override
            }
            return (committed + volatile).trimmingCharacters(in: .whitespacesAndNewlines)
        }()

        logger.info("ptt finalize reason=\(reason) len=\(finalText.count)")

        // Send to gateway if not empty
        if !finalText.isEmpty {
            await MainActor.run {
                VoiceWakeChimePlayer.play(.standard)
                VoicePushToTalk.shared.transitionToSpeaking()
            }

            Task.detached {
                _ = await VoiceWakeForwarder.forward(transcript: finalText)
            }
        }

        await cleanup()
        await MainActor.run {
            VoiceWakeOverlayController.shared.hide()
            VoicePushToTalk.shared.transitionToIdle()
        }

        // Resume wake word runtime
        await VoiceWakeRuntime.shared.applyPushToTalkCooldown()

        // End coordinator session
        await VoiceSessionCoordinator.shared.endSession(mode: .pushToTalk)
    }

    private func cleanup() async {
        recognitionTask?.cancel()
        recognitionRequest = nil
        recognitionTask = nil

        if tapInstalled {
            audioEngine?.inputNode.removeTap(onBus: 0)
            tapInstalled = false
        }
        if audioEngine?.isRunning == true {
            audioEngine?.stop()
            audioEngine?.reset()
        }
        audioEngine = nil

        committed = ""
        volatile = ""
    }

    private func noteAudioLevel(rms: Double) async {
        guard isCapturing else { return }

        let alpha: Double = rms < noiseFloorRMS ? 0.08 : 0.01
        noiseFloorRMS = max(1e-7, noiseFloorRMS + (rms - noiseFloorRMS) * alpha)

        let threshold = max(minSpeechRMS, noiseFloorRMS * speechBoostFactor)
        let normalized = min(1.0, max(0.0, rms / threshold))

        await MainActor.run {
            VoicePushToTalk.shared.updateAudioLevel(normalized)
            VoiceWakeOverlayController.shared.updateLevel(normalized)
        }
    }

    // MARK: - Utilities

    private static func rmsLevel(buffer: AVAudioPCMBuffer) -> Double? {
        guard let data = buffer.floatChannelData?.pointee, buffer.frameLength > 0 else { return nil }
        var sum: Double = 0
        for i in 0..<Int(buffer.frameLength) {
            let sample = Double(data[i])
            sum += sample * sample
        }
        return sqrt(sum / Double(buffer.frameLength))
    }

    private static func join(_ prefix: String, _ suffix: String) -> String {
        if prefix.isEmpty { return suffix }
        if suffix.isEmpty { return prefix }
        return "\(prefix) \(suffix)"
    }

    private static func delta(after committed: String, current: String) -> String {
        if current.hasPrefix(committed) {
            let start = current.index(current.startIndex, offsetBy: committed.count)
            return String(current[start...])
        }
        return current
    }
}

// MARK: - Test Helpers

#if DEBUG
extension VoicePushToTalk {
    static func _testSetState(_ newState: PTTState) {
        shared.state = newState
    }

    static func _testSetEnabled(_ enabled: Bool) {
        shared.isEnabled = enabled
    }
}

extension VoicePushToTalkHotkey {
    func _testTriggerDown() {
        triggerDown()
    }

    func _testTriggerUp() {
        triggerUp()
    }
}
#endif
