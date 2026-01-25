import AVFoundation
import Foundation
import OSLog
import Speech

/// Actor-based voice wake runtime using SFSpeechRecognizer and AVAudioEngine.
actor VoiceWakeRuntime {
    static let shared = VoiceWakeRuntime()

    enum ListeningState: Sendable { case idle, voiceWake, pushToTalk }

    struct RuntimeConfig: Equatable, Sendable {
        let triggers: [String]
        let micID: String?
        let localeID: String?
        let triggerChime: VoiceWakeChime
        let sendChime: VoiceWakeChime
    }

    private let logger = Logger(subsystem: "com.nexus.mac", category: "voicewake.runtime")

    private var recognizer: SFSpeechRecognizer?
    private var audioEngine: AVAudioEngine?
    private var recognitionRequest: SFSpeechAudioBufferRecognitionRequest?
    private var recognitionTask: SFSpeechRecognitionTask?
    private var recognitionGeneration: Int = 0
    private var lastHeard: Date?
    private var noiseFloorRMS: Double = 1e-4
    private var captureStartedAt: Date?
    private var captureTask: Task<Void, Never>?
    private var capturedTranscript = ""
    private var isCapturing = false
    private var heardBeyondTrigger = false
    private var triggerChimePlayed = false
    private var cooldownUntil: Date?
    private var currentConfig: RuntimeConfig?
    private var listeningState: ListeningState = .idle
    private var activeTriggerEndTime: TimeInterval?
    private var scheduledRestartTask: Task<Void, Never>?
    private var lastTranscript: String?
    private var lastTranscriptAt: Date?
    private var triggerOnlyTask: Task<Void, Never>?
    private var isStarting = false

    // Tunables
    private let silenceWindow: TimeInterval = 2.0
    private let triggerOnlySilenceWindow: TimeInterval = 5.0
    private let captureHardStop: TimeInterval = 120.0
    private let debounceAfterSend: TimeInterval = 0.35
    private let minSpeechRMS: Double = 1e-3
    private let speechBoostFactor: Double = 6.0
    private let triggerPauseWindow: TimeInterval = 0.55

    func start(with config: RuntimeConfig) async {
        guard !isStarting else { return }
        isStarting = true
        defer { isStarting = false }

        do {
            recognitionGeneration &+= 1
            let generation = recognitionGeneration

            let locale = config.localeID.flatMap { Locale(identifier: $0) } ?? Locale.current
            recognizer = SFSpeechRecognizer(locale: locale)
            recognizer?.defaultTaskHint = .dictation
            guard let recognizer, recognizer.isAvailable else { logger.error("Recognizer unavailable"); return }

            recognitionRequest = SFSpeechAudioBufferRecognitionRequest()
            recognitionRequest?.shouldReportPartialResults = true
            recognitionRequest?.taskHint = .dictation
            guard let request = recognitionRequest else { return }

            if audioEngine == nil { audioEngine = AVAudioEngine() }
            guard let audioEngine else { return }

            let input = audioEngine.inputNode
            let format = input.outputFormat(forBus: 0)
            guard format.channelCount > 0, format.sampleRate > 0 else {
                throw NSError(domain: "VoiceWakeRuntime", code: 1, userInfo: [NSLocalizedDescriptionKey: "No audio input"])
            }

            input.removeTap(onBus: 0)
            input.installTap(onBus: 0, bufferSize: 2048, format: format) { [weak self, weak request] buffer, _ in
                request?.append(buffer)
                guard let rms = Self.rmsLevel(buffer: buffer) else { return }
                Task { await self?.noteAudioLevel(rms: rms) }
            }

            audioEngine.prepare()
            try audioEngine.start()
            currentConfig = config
            lastHeard = Date()

            recognitionTask = recognizer.recognitionTask(with: request) { [weak self, generation] result, error in
                guard let self else { return }
                let transcript = result?.bestTranscription.formattedString
                let segments = result.flatMap { r in transcript.map { WakeWordGate.segments(from: r.bestTranscription, transcript: $0) } } ?? []
                Task { await self.handleRecognition(transcript: transcript, segments: segments, isFinal: result?.isFinal ?? false, generation: generation, config: config) }
            }
            logger.info("VoiceWake started")
        } catch { logger.error("Failed to start: \(error.localizedDescription)"); stop() }
    }

    func stop() {
        scheduledRestartTask?.cancel(); captureTask?.cancel(); triggerOnlyTask?.cancel()
        scheduledRestartTask = nil; captureTask = nil; triggerOnlyTask = nil
        isCapturing = false; capturedTranscript = ""; captureStartedAt = nil; triggerChimePlayed = false
        lastTranscript = nil; lastTranscriptAt = nil
        haltRecognitionPipeline()
        recognizer = nil; currentConfig = nil; listeningState = .idle; activeTriggerEndTime = nil
    }

    private func haltRecognitionPipeline() {
        recognitionGeneration &+= 1
        recognitionTask?.cancel(); recognitionTask = nil
        recognitionRequest?.endAudio(); recognitionRequest = nil
        audioEngine?.inputNode.removeTap(onBus: 0); audioEngine?.stop(); audioEngine = nil
    }

    private func handleRecognition(transcript: String?, segments: [WakeWordSegment], isFinal: Bool, generation: Int, config: RuntimeConfig) async {
        guard generation == recognitionGeneration, let transcript, !transcript.isEmpty else { return }
        let now = Date()
        lastHeard = now

        if !isCapturing { lastTranscript = transcript; lastTranscriptAt = now }

        if isCapturing {
            let trimmed = WakeWordGate.commandAfterTrigger(transcript: transcript, segments: segments, triggerEndTime: activeTriggerEndTime, triggers: config.triggers)
            capturedTranscript = trimmed
            if !heardBeyondTrigger && !trimmed.isEmpty { heardBeyondTrigger = true }
            return
        }

        let gateConfig = WakeWordGateConfig(triggers: config.triggers)
        var match = WakeWordGate.match(transcript: transcript, segments: segments, config: gateConfig)
        if match == nil, isFinal, let cmd = WakeWordGate.textOnlyCommand(transcript: transcript, triggers: config.triggers, minLength: gateConfig.minCommandLength) {
            match = WakeWordGateMatch(triggerEndTime: 0, postGap: 0, command: cmd)
        }

        if let match {
            if let cooldown = cooldownUntil, now < cooldown { return }
            logger.info("Trigger detected")
            await beginCapture(command: match.command, triggerEndTime: match.triggerEndTime, config: config)
        } else if isTriggerOnly(transcript: transcript, triggers: config.triggers) {
            scheduleTriggerOnlyCheck(triggers: config.triggers, config: config)
        }
    }

    private func isTriggerOnly(transcript: String, triggers: [String]) -> Bool {
        WakeWordGate.matchesTextOnly(text: transcript, triggers: triggers) &&
        WakeWordGate.startsWithTrigger(transcript: transcript, triggers: triggers) &&
        WakeWordGate.trimmedAfterTrigger(transcript, triggers: triggers).isEmpty
    }

    private func scheduleTriggerOnlyCheck(triggers: [String], config: RuntimeConfig) {
        triggerOnlyTask?.cancel()
        let lastAt = lastTranscriptAt, lastText = lastTranscript
        triggerOnlyTask = Task { [weak self] in
            try? await Task.sleep(nanoseconds: UInt64((self?.triggerPauseWindow ?? 0.55) * 1_000_000_000))
            guard let self, !Task.isCancelled, !self.isCapturing else { return }
            guard self.lastTranscriptAt == lastAt, self.lastTranscript == lastText else { return }
            guard self.isTriggerOnly(transcript: lastText ?? "", triggers: triggers) else { return }
            if let cd = self.cooldownUntil, Date() < cd { return }
            await self.beginCapture(command: "", triggerEndTime: nil, config: config)
        }
    }

    private func beginCapture(command: String, triggerEndTime: TimeInterval?, config: RuntimeConfig) async {
        listeningState = .voiceWake; isCapturing = true; capturedTranscript = command
        captureStartedAt = Date(); cooldownUntil = nil; heardBeyondTrigger = !command.isEmpty
        triggerChimePlayed = false; activeTriggerEndTime = triggerEndTime
        triggerOnlyTask?.cancel(); triggerOnlyTask = nil

        if config.triggerChime != .none, !triggerChimePlayed {
            triggerChimePlayed = true
            await MainActor.run { VoiceWakeChimePlayer.play(config.triggerChime) }
        }

        captureTask?.cancel()
        captureTask = Task { [weak self] in await self?.monitorCapture(config: config) }
    }

    private func monitorCapture(config: RuntimeConfig) async {
        let start = captureStartedAt ?? Date()
        let hardStop = start.addingTimeInterval(captureHardStop)
        while isCapturing {
            let now = Date()
            if now >= hardStop { await finalizeCapture(config: config); return }
            let silenceThreshold = heardBeyondTrigger ? silenceWindow : triggerOnlySilenceWindow
            if let last = lastHeard, now.timeIntervalSince(last) >= silenceThreshold { await finalizeCapture(config: config); return }
            try? await Task.sleep(nanoseconds: 200_000_000)
        }
    }

    private func finalizeCapture(config: RuntimeConfig) async {
        guard isCapturing else { return }
        isCapturing = false; cooldownUntil = Date().addingTimeInterval(debounceAfterSend)
        captureTask?.cancel(); captureTask = nil

        let finalTranscript = capturedTranscript.trimmingCharacters(in: .whitespacesAndNewlines)
        haltRecognitionPipeline()
        capturedTranscript = ""; captureStartedAt = nil; lastHeard = nil; heardBeyondTrigger = false
        triggerChimePlayed = false; activeTriggerEndTime = nil; lastTranscript = nil; lastTranscriptAt = nil
        triggerOnlyTask?.cancel(); triggerOnlyTask = nil

        if !finalTranscript.isEmpty {
            if config.sendChime != .none { await MainActor.run { VoiceWakeChimePlayer.play(config.sendChime) } }
            Task.detached { await VoiceWakeForwarder.forward(transcript: finalTranscript) }
        }
        scheduleRestart()
    }

    private func noteAudioLevel(rms: Double) {
        guard isCapturing else { return }
        let alpha: Double = rms < noiseFloorRMS ? 0.08 : 0.01
        noiseFloorRMS = max(1e-7, noiseFloorRMS + (rms - noiseFloorRMS) * alpha)
        if rms >= max(minSpeechRMS, noiseFloorRMS * speechBoostFactor) { lastHeard = Date() }
    }

    private static func rmsLevel(buffer: AVAudioPCMBuffer) -> Double? {
        guard let data = buffer.floatChannelData?.pointee, buffer.frameLength > 0 else { return nil }
        var sum: Double = 0
        for i in 0..<Int(buffer.frameLength) { let s = Double(data[i]); sum += s * s }
        return sqrt(sum / Double(buffer.frameLength))
    }

    private func scheduleRestart(delay: TimeInterval = 0.7) {
        scheduledRestartTask?.cancel()
        scheduledRestartTask = Task { [weak self] in
            try? await Task.sleep(nanoseconds: UInt64(delay * 1_000_000_000))
            guard let self, !Task.isCancelled, !self.isCapturing else { return }
            let cfg = self.currentConfig; self.stop()
            if let cfg { await self.start(with: cfg) }
        }
    }

    func applyPushToTalkCooldown() { cooldownUntil = Date().addingTimeInterval(debounceAfterSend) }
    func pauseForPushToTalk() { listeningState = .pushToTalk; stop() }
}
