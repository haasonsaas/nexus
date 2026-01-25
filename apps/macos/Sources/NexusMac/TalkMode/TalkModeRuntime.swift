import AVFoundation
import Foundation
import OSLog
import Speech

actor TalkModeRuntime {
    static let shared = TalkModeRuntime()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "talk.runtime")
    private let ttsLogger = Logger(subsystem: "com.nexus.mac", category: "talk.tts")
    private static let defaultModelId = "eleven_v3"

    // MARK: - RMS Meter

    private final class RMSMeter: @unchecked Sendable {
        private let lock = NSLock()
        private var latestRMS: Double = 0

        func set(_ rms: Double) {
            lock.lock()
            latestRMS = rms
            lock.unlock()
        }

        func get() -> Double {
            lock.lock()
            let value = latestRMS
            lock.unlock()
            return value
        }
    }

    // MARK: - State

    private var recognizer: SFSpeechRecognizer?
    private var audioEngine: AVAudioEngine?
    private var recognitionRequest: SFSpeechAudioBufferRecognitionRequest?
    private var recognitionTask: SFSpeechRecognitionTask?
    private var recognitionGeneration: Int = 0
    private var rmsTask: Task<Void, Never>?
    private let rmsMeter = RMSMeter()

    private var silenceTask: Task<Void, Never>?
    private var phase: TalkModePhase = .idle
    private var isEnabled = false
    private var isPaused = false
    private var lifecycleGeneration: Int = 0

    private var lastHeard: Date?
    private var noiseFloorRMS: Double = 1e-4
    private var lastTranscript: String = ""
    private var lastSpeechEnergyAt: Date?

    // TTS Configuration
    private var voiceId: String?
    private var modelId: String?
    private var apiKey: String?
    private var interruptOnSpeech: Bool = true
    private var lastInterruptedAtSeconds: Double?
    private var lastSpokenText: String?
    private var audioPlayer: AVAudioPlayer?
    private var isPlaying = false

    // Tunables
    private let silenceWindow: TimeInterval = 0.7
    private let minSpeechRMS: Double = 1e-3
    private let speechBoostFactor: Double = 6.0

    // MARK: - Lifecycle

    func setEnabled(_ enabled: Bool) async {
        guard enabled != isEnabled else { return }
        isEnabled = enabled
        lifecycleGeneration &+= 1
        if enabled {
            await start()
        } else {
            await stop()
        }
    }

    func setPaused(_ paused: Bool) async {
        guard paused != isPaused else { return }
        isPaused = paused
        await MainActor.run { TalkModeController.shared.updateLevel(0) }

        guard isEnabled else { return }

        if paused {
            lastTranscript = ""
            lastHeard = nil
            lastSpeechEnergyAt = nil
            await stopRecognition()
            return
        }

        if phase == .idle || phase == .listening {
            await startRecognition()
            phase = .listening
            await MainActor.run { TalkModeController.shared.updatePhase(.listening) }
            startSilenceMonitor()
        }
    }

    private func isCurrent(_ generation: Int) -> Bool {
        generation == lifecycleGeneration && isEnabled
    }

    private func start() async {
        let gen = lifecycleGeneration
        await reloadConfig()
        guard isCurrent(gen) else { return }

        if isPaused {
            phase = .idle
            await MainActor.run {
                TalkModeController.shared.updateLevel(0)
                TalkModeController.shared.updatePhase(.idle)
            }
            return
        }

        await startRecognition()
        guard isCurrent(gen) else { return }
        phase = .listening
        await MainActor.run { TalkModeController.shared.updatePhase(.listening) }
        startSilenceMonitor()
    }

    private func stop() async {
        silenceTask?.cancel()
        silenceTask = nil

        await stopSpeaking(reason: .manual)

        lastTranscript = ""
        lastHeard = nil
        lastSpeechEnergyAt = nil
        phase = .idle
        await stopRecognition()
        await MainActor.run {
            TalkModeController.shared.updateLevel(0)
            TalkModeController.shared.updatePhase(.idle)
        }
    }

    // MARK: - Speech Recognition

    private struct RecognitionUpdate {
        let transcript: String?
        let hasConfidence: Bool
        let isFinal: Bool
        let errorDescription: String?
        let generation: Int
    }

    private func startRecognition() async {
        await stopRecognition()
        recognitionGeneration &+= 1
        let generation = recognitionGeneration

        recognizer = SFSpeechRecognizer(locale: Locale.current)
        guard let recognizer, recognizer.isAvailable else {
            logger.error("talk recognizer unavailable")
            return
        }

        recognitionRequest = SFSpeechAudioBufferRecognitionRequest()
        recognitionRequest?.shouldReportPartialResults = true
        guard let request = recognitionRequest else { return }

        if audioEngine == nil {
            audioEngine = AVAudioEngine()
        }
        guard let audioEngine else { return }

        let input = audioEngine.inputNode
        let format = input.outputFormat(forBus: 0)
        guard format.channelCount > 0, format.sampleRate > 0 else {
            logger.error("talk no audio input available")
            return
        }

        input.removeTap(onBus: 0)
        let meter = rmsMeter
        input.installTap(onBus: 0, bufferSize: 2048, format: format) { [weak request, meter] buffer, _ in
            request?.append(buffer)
            if let rms = Self.rmsLevel(buffer: buffer) {
                meter.set(rms)
            }
        }

        audioEngine.prepare()
        do {
            try audioEngine.start()
        } catch {
            logger.error("talk audio engine start failed: \(error.localizedDescription, privacy: .public)")
            return
        }

        startRMSTicker(meter: meter)

        recognitionTask = recognizer.recognitionTask(with: request) { [weak self, generation] result, error in
            guard let self else { return }
            let segments = result?.bestTranscription.segments ?? []
            let transcript = result?.bestTranscription.formattedString
            let update = RecognitionUpdate(
                transcript: transcript,
                hasConfidence: segments.contains { $0.confidence > 0.6 },
                isFinal: result?.isFinal ?? false,
                errorDescription: error?.localizedDescription,
                generation: generation
            )
            Task { await self.handleRecognition(update) }
        }
    }

    private func stopRecognition() async {
        recognitionGeneration &+= 1
        recognitionTask?.cancel()
        recognitionTask = nil
        recognitionRequest?.endAudio()
        recognitionRequest = nil
        audioEngine?.inputNode.removeTap(onBus: 0)
        audioEngine?.stop()
        audioEngine = nil
        recognizer = nil
        rmsTask?.cancel()
        rmsTask = nil
    }

    private func startRMSTicker(meter: RMSMeter) {
        rmsTask?.cancel()
        rmsTask = Task { [weak self, meter] in
            while let self {
                try? await Task.sleep(nanoseconds: 50_000_000)
                if Task.isCancelled { return }
                await self.noteAudioLevel(rms: meter.get())
            }
        }
    }

    private func handleRecognition(_ update: RecognitionUpdate) async {
        guard update.generation == recognitionGeneration else { return }
        guard !isPaused else { return }

        if let errorDescription = update.errorDescription {
            logger.debug("talk recognition error: \(errorDescription, privacy: .public)")
        }

        guard let transcript = update.transcript else { return }
        let trimmed = transcript.trimmingCharacters(in: .whitespacesAndNewlines)

        if phase == .speaking, interruptOnSpeech {
            if await shouldInterrupt(transcript: trimmed, hasConfidence: update.hasConfidence) {
                await stopSpeaking(reason: .speech)
                lastTranscript = ""
                lastHeard = nil
                await startListening()
            }
            return
        }

        guard phase == .listening else { return }

        if !trimmed.isEmpty {
            lastTranscript = trimmed
            lastHeard = Date()
        }

        if update.isFinal {
            lastTranscript = trimmed
        }
    }

    // MARK: - Silence Handling

    private func startSilenceMonitor() {
        silenceTask?.cancel()
        silenceTask = Task { [weak self] in
            await self?.silenceLoop()
        }
    }

    private func silenceLoop() async {
        while isEnabled {
            try? await Task.sleep(nanoseconds: 200_000_000)
            await checkSilence()
        }
    }

    private func checkSilence() async {
        guard !isPaused else { return }
        guard phase == .listening else { return }
        let transcript = lastTranscript.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !transcript.isEmpty else { return }
        guard let lastHeard else { return }
        let elapsed = Date().timeIntervalSince(lastHeard)
        guard elapsed >= silenceWindow else { return }
        await finalizeTranscript(transcript)
    }

    private func startListening() async {
        phase = .listening
        lastTranscript = ""
        lastHeard = nil
        await MainActor.run {
            TalkModeController.shared.updatePhase(.listening)
            TalkModeController.shared.updateLevel(0)
        }
    }

    private func finalizeTranscript(_ text: String) async {
        lastTranscript = ""
        lastHeard = nil
        phase = .thinking
        await MainActor.run { TalkModeController.shared.updatePhase(.thinking) }
        await stopRecognition()
        await sendAndSpeak(text)
    }

    // MARK: - Gateway + TTS

    private func sendAndSpeak(_ transcript: String) async {
        let gen = lifecycleGeneration
        await reloadConfig()
        guard isCurrent(gen) else { return }

        logger.info("talk send start chars=\(transcript.count, privacy: .public)")

        do {
            let response = try await sendToGateway(message: transcript)
            guard isCurrent(gen) else { return }

            guard let assistantText = response else {
                logger.warning("talk no assistant response")
                await resumeListeningIfNeeded()
                return
            }

            logger.info("talk assistant text len=\(assistantText.count, privacy: .public)")
            await playAssistant(text: assistantText)
            guard isCurrent(gen) else { return }
            await resumeListeningIfNeeded()
        } catch {
            logger.error("talk send failed: \(error.localizedDescription, privacy: .public)")
            await resumeListeningIfNeeded()
        }
    }

    private func sendToGateway(message: String) async throws -> String? {
        let params: [String: AnyCodable] = [
            "message": AnyCodable(message),
            "thinking": AnyCodable("low")
        ]

        let data = try await GatewayConnection.shared.request(
            method: "chat.send",
            params: params,
            timeoutMs: 45000
        )

        struct ChatResponse: Decodable {
            let runId: String?
            let response: String?
        }

        let response = try JSONDecoder().decode(ChatResponse.self, from: data)
        return response.response
    }

    private func resumeListeningIfNeeded() async {
        if isPaused {
            lastTranscript = ""
            lastHeard = nil
            lastSpeechEnergyAt = nil
            await MainActor.run {
                TalkModeController.shared.updateLevel(0)
            }
            return
        }
        await startListening()
        await startRecognition()
    }

    // MARK: - TTS Playback

    private func playAssistant(text: String) async {
        let gen = lifecycleGeneration
        let cleaned = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !cleaned.isEmpty else { return }
        guard isCurrent(gen) else { return }

        lastSpokenText = cleaned

        // Try ElevenLabs first, fall back to system voice
        if let apiKey, !apiKey.isEmpty, let voiceId {
            do {
                try await playElevenLabs(text: cleaned, voiceId: voiceId, apiKey: apiKey, generation: gen)
                return
            } catch {
                ttsLogger.warning("talk ElevenLabs failed, falling back to system: \(error.localizedDescription)")
            }
        }

        // System voice fallback
        await playSystemVoice(text: cleaned, generation: gen)
    }

    private func playElevenLabs(text: String, voiceId: String, apiKey: String, generation: Int) async throws {
        let model = modelId ?? Self.defaultModelId
        ttsLogger.info("talk TTS request voiceId=\(voiceId, privacy: .public) chars=\(text.count, privacy: .public)")

        guard isCurrent(generation) else { return }

        if interruptOnSpeech {
            await startRecognition()
            guard isCurrent(generation) else { return }
        }

        await MainActor.run { TalkModeController.shared.updatePhase(.speaking) }
        phase = .speaking

        let audioData = try await synthesizeElevenLabs(
            text: text,
            voiceId: voiceId,
            modelId: model,
            apiKey: apiKey
        )

        guard isCurrent(generation) else { return }
        try await playAudioData(audioData)

        if phase == .speaking {
            phase = .thinking
            await MainActor.run { TalkModeController.shared.updatePhase(.thinking) }
        }
    }

    private func synthesizeElevenLabs(text: String, voiceId: String, modelId: String, apiKey: String) async throws -> Data {
        let url = URL(string: "https://api.elevenlabs.io/v1/text-to-speech/\(voiceId)")!

        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue(apiKey, forHTTPHeaderField: "xi-api-key")
        request.setValue("audio/mpeg", forHTTPHeaderField: "Accept")

        let body: [String: Any] = [
            "text": text,
            "model_id": modelId,
            "voice_settings": [
                "stability": 0.5,
                "similarity_boost": 0.75
            ]
        ]

        request.httpBody = try JSONSerialization.data(withJSONObject: body)

        let (data, response) = try await URLSession.shared.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse else {
            throw NSError(domain: "TalkModeRuntime", code: 1, userInfo: [
                NSLocalizedDescriptionKey: "Invalid response"
            ])
        }

        guard (200...299).contains(httpResponse.statusCode) else {
            let errorMessage = String(data: data, encoding: .utf8) ?? "Unknown error"
            throw NSError(domain: "TalkModeRuntime", code: httpResponse.statusCode, userInfo: [
                NSLocalizedDescriptionKey: "ElevenLabs API error: \(errorMessage)"
            ])
        }

        return data
    }

    private func playAudioData(_ data: Data) async throws {
        try await MainActor.run {
            self.stopAudioPlayer()
            let player = try AVAudioPlayer(data: data)
            self.audioPlayer = player
            self.isPlaying = true
            player.prepareToPlay()
            player.play()
        }

        // Wait for playback to complete
        while await isPlayerPlaying() {
            try await Task.sleep(nanoseconds: 100_000_000)
            if !isCurrent(lifecycleGeneration) { break }
        }

        await MainActor.run {
            self.isPlaying = false
        }
    }

    @MainActor
    private func isPlayerPlaying() -> Bool {
        audioPlayer?.isPlaying ?? false
    }

    @MainActor
    private func stopAudioPlayer() {
        audioPlayer?.stop()
        audioPlayer = nil
        isPlaying = false
    }

    private func playSystemVoice(text: String, generation: Int) async {
        ttsLogger.info("talk system voice start chars=\(text.count, privacy: .public)")

        if interruptOnSpeech {
            await startRecognition()
            guard isCurrent(generation) else { return }
        }

        await MainActor.run { TalkModeController.shared.updatePhase(.speaking) }
        phase = .speaking

        let synthesizer = NSSpeechSynthesizer()
        await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
            let delegate = SpeechDelegate(continuation: continuation)
            synthesizer.delegate = delegate
            synthesizer.startSpeaking(text)
            // Keep delegate alive
            withExtendedLifetime(delegate) {}
        }

        ttsLogger.info("talk system voice done")

        if phase == .speaking {
            phase = .thinking
            await MainActor.run { TalkModeController.shared.updatePhase(.thinking) }
        }
    }

    func stopSpeaking(reason: TalkStopReason) async {
        let interruptedAt = await MainActor.run { () -> Double? in
            let time = self.audioPlayer?.currentTime
            self.stopAudioPlayer()
            return time
        }

        guard phase == .speaking else { return }

        if reason == .speech, let interruptedAt {
            lastInterruptedAtSeconds = interruptedAt
        }

        if reason == .manual {
            return
        }

        if reason == .speech || reason == .userTap {
            await startListening()
            return
        }

        phase = .thinking
        await MainActor.run { TalkModeController.shared.updatePhase(.thinking) }
    }

    // MARK: - Audio Level Handling

    private func noteAudioLevel(rms: Double) async {
        if phase != .listening, phase != .speaking { return }
        let alpha: Double = rms < noiseFloorRMS ? 0.08 : 0.01
        noiseFloorRMS = max(1e-7, noiseFloorRMS + (rms - noiseFloorRMS) * alpha)

        let threshold = max(minSpeechRMS, noiseFloorRMS * speechBoostFactor)
        if rms >= threshold {
            let now = Date()
            lastHeard = now
            lastSpeechEnergyAt = now
        }

        if phase == .listening {
            let clamped = min(1.0, max(0.0, rms / max(minSpeechRMS, threshold)))
            await MainActor.run { TalkModeController.shared.updateLevel(clamped) }
        }
    }

    private static func rmsLevel(buffer: AVAudioPCMBuffer) -> Double? {
        guard let channelData = buffer.floatChannelData?.pointee else { return nil }
        let frameCount = Int(buffer.frameLength)
        guard frameCount > 0 else { return nil }
        var sum: Double = 0
        for i in 0..<frameCount {
            let sample = Double(channelData[i])
            sum += sample * sample
        }
        return sqrt(sum / Double(frameCount))
    }

    private func shouldInterrupt(transcript: String, hasConfidence: Bool) async -> Bool {
        let trimmed = transcript.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed.count >= 3 else { return false }
        if isLikelyEcho(of: trimmed) { return false }
        let now = Date()
        if let lastSpeechEnergyAt, now.timeIntervalSince(lastSpeechEnergyAt) > 0.35 {
            return false
        }
        return hasConfidence
    }

    private func isLikelyEcho(of transcript: String) -> Bool {
        guard let spoken = lastSpokenText?.lowercased(), !spoken.isEmpty else { return false }
        let probe = transcript.lowercased()
        if probe.count < 6 {
            return spoken.contains(probe)
        }
        return spoken.contains(probe)
    }

    // MARK: - Config

    private func reloadConfig() async {
        let env = ProcessInfo.processInfo.environment
        voiceId = env["ELEVENLABS_VOICE_ID"]?.trimmingCharacters(in: .whitespacesAndNewlines)
        modelId = env["ELEVENLABS_MODEL_ID"]?.trimmingCharacters(in: .whitespacesAndNewlines)
        apiKey = env["ELEVENLABS_API_KEY"]?.trimmingCharacters(in: .whitespacesAndNewlines)

        if modelId?.isEmpty != false {
            modelId = Self.defaultModelId
        }

        let hasApiKey = (apiKey?.isEmpty == false)
        let voiceLabel = (voiceId?.isEmpty == false) ? voiceId! : "none"
        logger.info("talk config voiceId=\(voiceLabel, privacy: .public) apiKey=\(hasApiKey, privacy: .public)")
    }
}

// MARK: - Speech Delegate

private final class SpeechDelegate: NSObject, NSSpeechSynthesizerDelegate, @unchecked Sendable {
    private var continuation: CheckedContinuation<Void, Never>?

    init(continuation: CheckedContinuation<Void, Never>) {
        self.continuation = continuation
    }

    func speechSynthesizer(_ sender: NSSpeechSynthesizer, didFinishSpeaking finishedSpeaking: Bool) {
        continuation?.resume()
        continuation = nil
    }
}
