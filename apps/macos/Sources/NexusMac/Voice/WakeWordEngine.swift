import AVFoundation
import Foundation
import OSLog
import Speech

// MARK: - Wake Word State

/// Wake word detection states
enum WakeWordState: String, Sendable {
    case idle
    case listening
    case hearing       // Voice activity detected
    case finalizing    // Speech ending, processing
    case detected      // Wake word found
    case failed        // Detection failed
}

// MARK: - Wake Word Configuration

/// Wake word configuration with tunable parameters
struct WakeWordConfig: Codable, Sendable, Equatable {
    var triggerWords: [String]
    var confidenceThreshold: Float
    var silenceTimeout: TimeInterval
    var maxListenDuration: TimeInterval
    var noiseFloorRMS: Float

    static let `default` = WakeWordConfig(
        triggerWords: ["hey nexus", "nexus", "okay nexus"],
        confidenceThreshold: 0.7,
        silenceTimeout: 1.5,
        maxListenDuration: 120,
        noiseFloorRMS: 0.02
    )
}

// MARK: - Wake Word Engine Delegate

/// Delegate protocol for wake word events
protocol WakeWordEngineDelegate: AnyObject, Sendable {
    @MainActor func wakeWordEngine(_ engine: WakeWordEngine, didChangeState state: WakeWordState)
    @MainActor func wakeWordEngine(_ engine: WakeWordEngine, didDetectWakeWord word: String, confidence: Float)
    @MainActor func wakeWordEngine(_ engine: WakeWordEngine, didUpdateTranscript transcript: String, isFinal: Bool)
    @MainActor func wakeWordEngine(_ engine: WakeWordEngine, didUpdateAudioLevel level: Float)
    @MainActor func wakeWordEngine(_ engine: WakeWordEngine, didFailWithError error: Error)
}

// MARK: - Wake Word Error

/// Errors that can occur during wake word detection
enum WakeWordError: LocalizedError {
    case notAuthorized
    case microphoneNotGranted
    case recognitionFailed
    case recognizerUnavailable
    case noAudioInput

    var errorDescription: String? {
        switch self {
        case .notAuthorized:
            return "Speech recognition not authorized"
        case .microphoneNotGranted:
            return "Microphone access not granted"
        case .recognitionFailed:
            return "Speech recognition failed"
        case .recognizerUnavailable:
            return "Speech recognizer unavailable"
        case .noAudioInput:
            return "No audio input available"
        }
    }
}

// MARK: - Wake Word Engine

/// Engine for detecting wake words using the Speech framework.
///
/// Uses SFSpeechRecognizer for speech-to-text and AVAudioEngine for audio capture.
/// Implements RMS-based voice activity detection and supports configurable trigger words.
///
/// Usage:
/// ```swift
/// let engine = WakeWordEngine.shared
/// engine.delegate = self
/// try await engine.start()
/// ```
@MainActor
@Observable
final class WakeWordEngine {
    static let shared = WakeWordEngine()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "wake-word")

    // MARK: - Observable State

    /// Current state of the wake word engine
    private(set) var state: WakeWordState = .idle

    /// Current transcript being processed
    private(set) var currentTranscript = ""

    /// Current audio level (0-1 normalized)
    private(set) var audioLevel: Float = 0

    /// Whether the engine is enabled and running
    private(set) var isEnabled = false

    /// Last detected wake word
    private(set) var lastDetectedWord: String?

    /// Last detection confidence
    private(set) var lastDetectionConfidence: Float = 0

    // MARK: - Configuration

    /// Engine configuration
    var config = WakeWordConfig.default

    // MARK: - Delegate

    /// Delegate for receiving wake word events
    weak var delegate: WakeWordEngineDelegate?

    // MARK: - Audio Components

    private var audioEngine: AVAudioEngine?
    private var recognitionRequest: SFSpeechAudioBufferRecognitionRequest?
    private var recognitionTask: SFSpeechRecognitionTask?
    private var speechRecognizer: SFSpeechRecognizer?

    // MARK: - Timing & State Tracking

    private var silenceTimer: Timer?
    private var maxDurationTimer: Timer?
    private var generationId: Int = 0
    private var lastHeard: Date?
    private var adaptiveNoiseFloor: Float = 0.02

    // MARK: - Tuning Constants

    private let speechBoostFactor: Float = 6.0
    private let minSpeechRMS: Float = 0.001
    private let noiseFloorAlphaLow: Float = 0.08
    private let noiseFloorAlphaHigh: Float = 0.01
    private let restartDelay: Duration = .milliseconds(100)
    private let bufferSize: AVAudioFrameCount = 1024

    // MARK: - Initialization

    private init() {
        logger.info("WakeWordEngine initialized")
    }

    // MARK: - Lifecycle

    /// Start the wake word detection engine.
    /// Requests necessary permissions and begins listening.
    func start() async throws {
        guard !isEnabled else {
            logger.debug("Engine already started")
            return
        }

        logger.info("Starting wake word engine")

        // Request speech recognition permission
        let speechStatus = await requestSpeechRecognitionPermission()
        guard speechStatus == .authorized else {
            logger.error("Speech recognition not authorized: \(speechStatus.rawValue)")
            throw WakeWordError.notAuthorized
        }

        // Request microphone permission
        let micGranted = await requestMicrophonePermission()
        guard micGranted else {
            logger.error("Microphone access not granted")
            throw WakeWordError.microphoneNotGranted
        }

        // Initialize speech recognizer
        let locale = Locale(identifier: "en-US")
        speechRecognizer = SFSpeechRecognizer(locale: locale)
        speechRecognizer?.defaultTaskHint = .dictation

        guard let recognizer = speechRecognizer, recognizer.isAvailable else {
            logger.error("Speech recognizer unavailable")
            throw WakeWordError.recognizerUnavailable
        }

        isEnabled = true
        adaptiveNoiseFloor = config.noiseFloorRMS
        startListening()
        logger.info("Wake word engine started successfully")
    }

    /// Stop the wake word detection engine.
    func stop() {
        guard isEnabled else { return }

        logger.info("Stopping wake word engine")
        isEnabled = false
        stopListening()
        speechRecognizer = nil
        lastDetectedWord = nil
        lastDetectionConfidence = 0
        logger.info("Wake word engine stopped")
    }

    /// Pause listening temporarily (e.g., during PTT)
    func pause() {
        guard isEnabled else { return }
        logger.debug("Pausing wake word engine")
        stopListening()
    }

    /// Resume listening after pause
    func resume() {
        guard isEnabled else { return }
        logger.debug("Resuming wake word engine")
        startListening()
    }

    // MARK: - Listening Management

    private func startListening() {
        guard isEnabled else { return }

        generationId += 1
        let currentGeneration = generationId

        // Setup audio engine
        audioEngine = AVAudioEngine()
        guard let audioEngine = audioEngine else {
            logger.error("Failed to create audio engine")
            return
        }

        // Create recognition request
        recognitionRequest = SFSpeechAudioBufferRecognitionRequest()
        guard let recognitionRequest = recognitionRequest else {
            logger.error("Failed to create recognition request")
            return
        }

        recognitionRequest.shouldReportPartialResults = true
        recognitionRequest.taskHint = .dictation

        // On-device recognition if available (better privacy)
        if #available(macOS 13.0, *) {
            recognitionRequest.requiresOnDeviceRecognition = speechRecognizer?.supportsOnDeviceRecognition ?? false
        }

        // Install audio tap
        let inputNode = audioEngine.inputNode
        let format = inputNode.outputFormat(forBus: 0)

        guard format.channelCount > 0, format.sampleRate > 0 else {
            logger.error("Invalid audio format: channels=\(format.channelCount) sampleRate=\(format.sampleRate)")
            updateState(.failed)
            delegate?.wakeWordEngine(self, didFailWithError: WakeWordError.noAudioInput)
            return
        }

        inputNode.removeTap(onBus: 0)
        inputNode.installTap(onBus: 0, bufferSize: bufferSize, format: format) { [weak self] buffer, _ in
            guard let self = self else { return }

            // Check generation to avoid processing stale audio
            Task { @MainActor in
                guard self.generationId == currentGeneration else { return }
                self.recognitionRequest?.append(buffer)
                self.processAudioLevel(buffer)
            }
        }

        // Start recognition task
        recognitionTask = speechRecognizer?.recognitionTask(with: recognitionRequest) { [weak self, currentGeneration] result, error in
            guard let self = self else { return }

            Task { @MainActor in
                guard self.generationId == currentGeneration else { return }

                if let result = result {
                    self.processRecognitionResult(result)
                }

                if let error = error {
                    self.handleRecognitionError(error)
                }
            }
        }

        // Start audio engine
        do {
            audioEngine.prepare()
            try audioEngine.start()
            updateState(.listening)
            startMaxDurationTimer()
            lastHeard = Date()
            logger.debug("Listening started (generation: \(currentGeneration))")
        } catch {
            logger.error("Failed to start audio engine: \(error.localizedDescription)")
            updateState(.failed)
            delegate?.wakeWordEngine(self, didFailWithError: error)
        }
    }

    private func stopListening() {
        // Cancel recognition
        recognitionTask?.cancel()
        recognitionTask = nil

        recognitionRequest?.endAudio()
        recognitionRequest = nil

        // Stop audio engine
        audioEngine?.inputNode.removeTap(onBus: 0)
        audioEngine?.stop()
        audioEngine = nil

        // Invalidate timers
        silenceTimer?.invalidate()
        silenceTimer = nil

        maxDurationTimer?.invalidate()
        maxDurationTimer = nil

        // Reset state
        updateState(.idle)
        currentTranscript = ""
        audioLevel = 0
        lastHeard = nil
    }

    private func restartListening() {
        logger.debug("Restarting listening")
        stopListening()

        // Small delay before restart to avoid rapid cycling
        Task {
            try? await Task.sleep(for: restartDelay)
            if isEnabled {
                startListening()
            }
        }
    }

    // MARK: - Recognition Processing

    private func processRecognitionResult(_ result: SFSpeechRecognitionResult) {
        let transcript = result.bestTranscription.formattedString.lowercased()
        currentTranscript = transcript

        delegate?.wakeWordEngine(self, didUpdateTranscript: transcript, isFinal: result.isFinal)

        // Check for wake words
        for word in config.triggerWords {
            let normalizedWord = word.lowercased().trimmingCharacters(in: .whitespacesAndNewlines)
            if transcript.contains(normalizedWord) {
                // Get confidence from the last segment that matches
                let confidence = findConfidenceForWord(normalizedWord, in: result.bestTranscription)

                // Accept if confidence is high enough or if it's a final result
                if confidence >= config.confidenceThreshold || result.isFinal {
                    handleWakeWordDetected(word, confidence: confidence)
                    return
                }
            }
        }

        // Update state based on voice activity
        if !transcript.isEmpty && state == .listening {
            updateState(.hearing)
        }

        // Reset silence timer on any speech
        if !transcript.isEmpty {
            lastHeard = Date()
            resetSilenceTimer()
        }

        // Handle final result without wake word
        if result.isFinal {
            logger.debug("Final result without wake word, restarting")
            restartListening()
        }
    }

    private func findConfidenceForWord(_ word: String, in transcription: SFTranscription) -> Float {
        let wordTokens = word.split(separator: " ").map { String($0).lowercased() }

        // Try to find matching segments
        var matchedConfidence: Float = 0
        var matchCount = 0

        for segment in transcription.segments {
            let segmentText = segment.substring.lowercased().trimmingCharacters(in: .whitespacesAndNewlines)
            if wordTokens.contains(segmentText) {
                matchedConfidence += segment.confidence
                matchCount += 1
            }
        }

        if matchCount > 0 {
            return matchedConfidence / Float(matchCount)
        }

        // Fallback to last segment confidence
        return transcription.segments.last?.confidence ?? 0
    }

    private func handleWakeWordDetected(_ word: String, confidence: Float) {
        logger.info("Wake word detected: '\(word)' (confidence: \(confidence, format: .fixed(precision: 2)))")

        lastDetectedWord = word
        lastDetectionConfidence = confidence

        updateState(.detected)
        delegate?.wakeWordEngine(self, didDetectWakeWord: word, confidence: confidence)

        // Stop listening after detection (caller can restart)
        stopListening()
    }

    private func handleRecognitionError(_ error: Error) {
        let nsError = error as NSError

        // Error 1110: No speech detected - normal, just restart
        // Error 1101: Network error - may need to retry
        // Error 301: Recognition canceled - normal during stop
        if nsError.code == 1110 {
            logger.debug("No speech detected, restarting")
            restartListening()
        } else if nsError.code == 301 {
            // Canceled - this is expected during stop
            logger.debug("Recognition canceled")
        } else {
            logger.error("Recognition error: \(error.localizedDescription) (code: \(nsError.code))")
            updateState(.failed)
            delegate?.wakeWordEngine(self, didFailWithError: error)

            // Attempt to restart after error
            restartListening()
        }
    }

    // MARK: - Audio Level Processing

    private func processAudioLevel(_ buffer: AVAudioPCMBuffer) {
        guard let channelData = buffer.floatChannelData else { return }
        guard buffer.frameLength > 0 else { return }

        let channelDataValue = channelData.pointee
        let frameLength = Int(buffer.frameLength)

        // Calculate RMS
        var sum: Float = 0
        for i in stride(from: 0, to: frameLength, by: buffer.stride) {
            let sample = channelDataValue[i]
            sum += sample * sample
        }
        let rms = sqrt(sum / Float(frameLength))

        // Update adaptive noise floor
        let alpha = rms < adaptiveNoiseFloor ? noiseFloorAlphaLow : noiseFloorAlphaHigh
        adaptiveNoiseFloor = max(Float(1e-7), adaptiveNoiseFloor + (rms - adaptiveNoiseFloor) * alpha)

        // Normalize level (0-1) based on noise floor
        let normalizedLevel = min(max((rms - config.noiseFloorRMS) * 10, 0), 1)

        audioLevel = normalizedLevel
        delegate?.wakeWordEngine(self, didUpdateAudioLevel: normalizedLevel)

        // Update voice activity detection
        if rms >= max(minSpeechRMS, adaptiveNoiseFloor * speechBoostFactor) {
            lastHeard = Date()
        }
    }

    // MARK: - Timer Management

    private func resetSilenceTimer() {
        silenceTimer?.invalidate()
        silenceTimer = Timer.scheduledTimer(withTimeInterval: config.silenceTimeout, repeats: false) { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.handleSilenceTimeout()
            }
        }
    }

    private func handleSilenceTimeout() {
        if state == .hearing {
            logger.debug("Silence timeout reached")
            updateState(.finalizing)
            // Let the recognition finalize
        }
    }

    private func startMaxDurationTimer() {
        maxDurationTimer?.invalidate()
        maxDurationTimer = Timer.scheduledTimer(withTimeInterval: config.maxListenDuration, repeats: false) { [weak self] _ in
            Task { @MainActor [weak self] in
                guard let self = self else { return }
                self.logger.debug("Max duration reached, restarting")
                self.restartListening()
            }
        }
    }

    // MARK: - State Management

    private func updateState(_ newState: WakeWordState) {
        guard state != newState else { return }

        let oldState = state
        state = newState

        logger.debug("State: \(oldState.rawValue) -> \(newState.rawValue)")
        delegate?.wakeWordEngine(self, didChangeState: newState)
    }

    // MARK: - Configuration Methods

    /// Set the trigger words for wake word detection
    func setTriggerWords(_ words: [String]) {
        config.triggerWords = words.map { $0.lowercased().trimmingCharacters(in: .whitespacesAndNewlines) }
        logger.info("Trigger words updated: \(self.config.triggerWords.joined(separator: ", "))")
    }

    /// Add a trigger word
    func addTriggerWord(_ word: String) {
        let normalized = word.lowercased().trimmingCharacters(in: .whitespacesAndNewlines)
        if !normalized.isEmpty && !config.triggerWords.contains(normalized) {
            config.triggerWords.append(normalized)
            logger.info("Added trigger word: \(normalized)")
        }
    }

    /// Remove a trigger word
    func removeTriggerWord(_ word: String) {
        let normalized = word.lowercased().trimmingCharacters(in: .whitespacesAndNewlines)
        config.triggerWords.removeAll { $0 == normalized }
        logger.info("Removed trigger word: \(normalized)")
    }

    /// Set the confidence threshold (0-1)
    func setConfidenceThreshold(_ threshold: Float) {
        config.confidenceThreshold = max(0, min(1, threshold))
        logger.info("Confidence threshold set to \(self.config.confidenceThreshold, format: .fixed(precision: 2))")
    }

    /// Set the silence timeout in seconds
    func setSilenceTimeout(_ timeout: TimeInterval) {
        config.silenceTimeout = max(0.5, min(10, timeout))
        logger.info("Silence timeout set to \(self.config.silenceTimeout)s")
    }

    // MARK: - Permission Helpers

    private func requestSpeechRecognitionPermission() async -> SFSpeechRecognizerAuthorizationStatus {
        await withCheckedContinuation { continuation in
            SFSpeechRecognizer.requestAuthorization { status in
                continuation.resume(returning: status)
            }
        }
    }

    private func requestMicrophonePermission() async -> Bool {
        await AVCaptureDevice.requestAccess(for: .audio)
    }

    // MARK: - Query Methods

    /// Check if the engine is currently listening for wake words
    var isListening: Bool {
        state == .listening || state == .hearing
    }

    /// Check if a wake word was detected
    var isDetected: Bool {
        state == .detected
    }

    /// Get all configured trigger words
    var triggerWords: [String] {
        config.triggerWords
    }
}
