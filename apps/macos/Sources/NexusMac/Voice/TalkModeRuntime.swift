import Foundation
import OSLog
import AVFoundation

// MARK: - Voice Talk Mode Runtime

/// Runtime for voice talk mode with audio input/output and phase management
@MainActor
@Observable
final class VoiceTalkModeRuntime {
    static let shared = VoiceTalkModeRuntime()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "voice-talk-mode")

    // MARK: - State

    private(set) var isActive = false
    private(set) var phase: VoiceTalkPhase = .idle
    private(set) var audioLevel: Float = 0
    private(set) var isMuted = false
    private(set) var transcript = ""

    // MARK: - Audio Components

    private var audioEngine: AVAudioEngine?
    private var audioPlayer: TalkAudioPlayer?

    // MARK: - Configuration

    var autoSend = true
    var silenceTimeout: TimeInterval = 1.5

    private init() {}

    // MARK: - Lifecycle

    func start() {
        guard !isActive else { return }

        isActive = true
        phase = .idle

        setupAudio()
        VoiceTalkOverlayController.shared.show()

        logger.info("Talk mode started")
    }

    func stop() {
        guard isActive else { return }

        teardownAudio()
        VoiceTalkOverlayController.shared.hide()

        isActive = false
        phase = .idle
        audioLevel = 0
        transcript = ""

        logger.info("Talk mode stopped")
    }

    func pause() {
        guard isActive, phase != .paused else { return }
        phase = .paused
        audioEngine?.pause()
        logger.info("Talk mode paused")
    }

    func resume() {
        guard isActive, phase == .paused else { return }
        phase = .idle
        try? audioEngine?.start()
        logger.info("Talk mode resumed")
    }

    func toggleMute() {
        isMuted.toggle()
        logger.info("Talk mode mute: \(self.isMuted)")
    }

    // MARK: - Phase Transitions

    func updatePhase(_ newPhase: VoiceTalkPhase) {
        guard phase != newPhase else { return }

        let oldPhase = phase
        phase = newPhase

        logger.debug("Phase: \(oldPhase.rawValue) -> \(newPhase.rawValue)")

        // Handle phase-specific logic
        switch newPhase {
        case .listening:
            startListening()
        case .thinking:
            stopListening()
        case .speaking:
            break
        case .idle, .paused:
            break
        }
    }

    // MARK: - Audio Setup

    private func setupAudio() {
        audioEngine = AVAudioEngine()
        audioPlayer = TalkAudioPlayer.shared

        guard let engine = audioEngine else { return }

        let inputNode = engine.inputNode
        let format = inputNode.outputFormat(forBus: 0)

        guard format.channelCount > 0, format.sampleRate > 0 else {
            logger.error("No audio input available")
            return
        }

        inputNode.installTap(onBus: 0, bufferSize: 1024, format: format) { [weak self] buffer, _ in
            self?.processAudioBuffer(buffer)
        }

        do {
            try engine.start()
            logger.debug("Audio engine started")
        } catch {
            logger.error("Failed to start audio engine: \(error.localizedDescription)")
        }
    }

    private func teardownAudio() {
        audioEngine?.inputNode.removeTap(onBus: 0)
        audioEngine?.stop()
        audioEngine = nil
        audioPlayer = nil
    }

    // MARK: - Audio Processing

    private func processAudioBuffer(_ buffer: AVAudioPCMBuffer) {
        guard !isMuted else {
            Task { @MainActor in
                self.audioLevel = 0
            }
            return
        }

        // Calculate RMS level
        guard let channelData = buffer.floatChannelData else { return }
        let channelDataValue = channelData.pointee
        let frameLength = Int(buffer.frameLength)
        guard frameLength > 0 else { return }

        let channelDataValueArray = stride(from: 0, to: frameLength, by: buffer.stride)
            .map { channelDataValue[$0] }

        let rms = sqrt(channelDataValueArray.map { $0 * $0 }.reduce(0, +) / Float(frameLength))
        let level = min(max(rms * 10, 0), 1) // Normalize to 0-1

        Task { @MainActor in
            self.audioLevel = level
        }
    }

    // MARK: - Speech Recognition

    private func startListening() {
        // Start speech recognition
        Task {
            // Implementation would use SFSpeechRecognizer
            logger.debug("Started listening")
        }
    }

    private func stopListening() {
        // Stop speech recognition
        logger.debug("Stopped listening")
    }

    // MARK: - Playback

    func playResponse(_ audioData: Data) {
        updatePhase(.speaking)

        Task {
            let result = await audioPlayer?.play(data: audioData)
            if result?.finished == true {
                updatePhase(.idle)
            }
        }
    }
}
