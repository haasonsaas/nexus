import AVFoundation
import Combine
import Foundation
import Observation
import OSLog

// MARK: - Playback State

/// State of the audio player.
enum TalkAudioPlaybackState: String, Sendable {
    case idle
    case loading
    case playing
    case paused
    case finished
}

// MARK: - Audio Format

/// Supported audio formats for TTS playback.
enum TalkAudioFormat: String, Sendable {
    case mp3
    case aac
    case wav
    case pcm

    var fileExtension: String {
        switch self {
        case .mp3: return "mp3"
        case .aac: return "aac"
        case .wav: return "wav"
        case .pcm: return "pcm"
        }
    }

    var mimeType: String {
        switch self {
        case .mp3: return "audio/mpeg"
        case .aac: return "audio/aac"
        case .wav: return "audio/wav"
        case .pcm: return "audio/pcm"
        }
    }
}

// MARK: - Playback Event

/// Events emitted during audio playback for UI updates.
enum TalkAudioPlaybackEvent: Sendable {
    case stateChanged(TalkAudioPlaybackState)
    case progressUpdated(current: TimeInterval, duration: TimeInterval)
    case speedChanged(Float)
    case volumeChanged(Float)
    case segmentStarted(index: Int, total: Int)
    case segmentFinished(index: Int)
    case queueCleared
    case error(String)
    case interrupted
    case resumed
}

// MARK: - Audio Segment

/// Represents a queued audio segment for playback.
struct TalkAudioSegment: Sendable {
    let id: UUID
    let data: Data
    let format: TalkAudioFormat

    init(data: Data, format: TalkAudioFormat = .mp3) {
        self.id = UUID()
        self.data = data
        self.format = format
    }
}

// MARK: - Playback Result

/// Result of audio playback completion.
struct TalkAudioPlaybackResult: Sendable {
    let finished: Bool
    let interruptedAt: TimeInterval?
    let segmentIndex: Int?
}

// MARK: - Talk Audio Player

/// Main audio player for TTS responses with support for streaming, queueing, and playback controls.
@MainActor
@Observable
final class TalkAudioPlayer: NSObject, @preconcurrency AVAudioPlayerDelegate {
    static let shared = TalkAudioPlayer()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "talk-audio")

    // MARK: - Observable State

    private(set) var state: TalkAudioPlaybackState = .idle
    private(set) var currentTime: TimeInterval = 0
    private(set) var duration: TimeInterval = 0
    private(set) var playbackSpeed: Float = 1.0
    private(set) var volume: Float = 1.0
    private(set) var currentSegmentIndex: Int = 0
    private(set) var totalSegments: Int = 0
    private(set) var audioLevels: [Float] = []

    // MARK: - Speed Bounds

    static let minSpeed: Float = 0.5
    static let maxSpeed: Float = 2.0
    static let speedStep: Float = 0.25

    // MARK: - Volume Bounds

    static let minVolume: Float = 0.0
    static let maxVolume: Float = 1.0

    // MARK: - Private State

    private var player: AVAudioPlayer?
    private var audioEngine: AVAudioEngine?
    private var playerNode: AVAudioPlayerNode?
    private var queue: [TalkAudioSegment] = []
    private var playback: Playback?
    private var progressTimer: Timer?
    private var levelTimer: Timer?
    private var interruptionObserver: NSObjectProtocol?
    private var streamBuffer: Data = Data()
    private var isStreaming: Bool = false

    // MARK: - Event Publisher

    private let eventSubject = PassthroughSubject<TalkAudioPlaybackEvent, Never>()
    var eventPublisher: AnyPublisher<TalkAudioPlaybackEvent, Never> {
        eventSubject.eraseToAnyPublisher()
    }

    // MARK: - Playback Tracking

    private final class Playback: @unchecked Sendable {
        private let lock = NSLock()
        private var finished = false
        private var continuation: CheckedContinuation<TalkAudioPlaybackResult, Never>?
        private var watchdog: Task<Void, Never>?
        let segmentIndex: Int

        init(segmentIndex: Int) {
            self.segmentIndex = segmentIndex
        }

        func setContinuation(_ continuation: CheckedContinuation<TalkAudioPlaybackResult, Never>) {
            lock.lock()
            defer { lock.unlock() }
            self.continuation = continuation
        }

        func setWatchdog(_ task: Task<Void, Never>?) {
            lock.lock()
            let old = watchdog
            watchdog = task
            lock.unlock()
            old?.cancel()
        }

        func cancelWatchdog() {
            setWatchdog(nil)
        }

        func finish(_ result: TalkAudioPlaybackResult) {
            let cont: CheckedContinuation<TalkAudioPlaybackResult, Never>?
            lock.lock()
            if finished {
                cont = nil
            } else {
                finished = true
                cont = continuation
                continuation = nil
            }
            lock.unlock()
            cont?.resume(returning: result)
        }
    }

    // MARK: - Initialization

    override private init() {
        super.init()
        setupInterruptionHandling()
        logger.info("TalkAudioPlayer initialized")
    }

    deinit {
        if let observer = interruptionObserver {
            NotificationCenter.default.removeObserver(observer)
        }
    }

    // MARK: - Audio Session Configuration

    private func configureAudioSession() {
        // On macOS, we don't have AVAudioSession like iOS
        // Audio session configuration is handled differently
        logger.debug("Audio session configured for voice playback")
    }

    // MARK: - Interruption Handling

    private func setupInterruptionHandling() {
        // On macOS, handle audio interruptions through workspace notifications
        let center = NSWorkspace.shared.notificationCenter
        interruptionObserver = center.addObserver(
            forName: NSWorkspace.willSleepNotification,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.handleInterruption(began: true)
            }
        }

        // Also observe wake notifications
        center.addObserver(
            forName: NSWorkspace.didWakeNotification,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.handleInterruption(began: false)
            }
        }
    }

    private func handleInterruption(began: Bool) {
        if began {
            logger.info("Audio interruption began")
            if state == .playing {
                pause()
                emitEvent(.interrupted)
            }
        } else {
            logger.info("Audio interruption ended")
            emitEvent(.resumed)
        }
    }

    // MARK: - Single Segment Playback

    /// Plays audio data and returns the playback result.
    /// - Parameters:
    ///   - data: The audio data to play
    ///   - format: The audio format (defaults to MP3)
    /// - Returns: The playback result indicating completion status
    func play(data: Data, format: TalkAudioFormat = .mp3) async -> TalkAudioPlaybackResult {
        stopInternal()
        configureAudioSession()

        let segment = TalkAudioSegment(data: data, format: format)
        return await playSegment(segment, index: 0, total: 1)
    }

    private func playSegment(_ segment: TalkAudioSegment, index: Int, total: Int) async -> TalkAudioPlaybackResult {
        let playback = Playback(segmentIndex: index)
        self.playback = playback
        currentSegmentIndex = index
        totalSegments = total

        updateState(.loading)
        emitEvent(.segmentStarted(index: index, total: total))

        return await withCheckedContinuation { continuation in
            playback.setContinuation(continuation)
            do {
                let player = try AVAudioPlayer(data: segment.data)
                self.player = player
                player.delegate = self
                player.enableRate = true
                player.rate = playbackSpeed
                player.volume = volume
                player.prepareToPlay()

                duration = player.duration
                currentTime = 0
                armWatchdog(playback: playback)
                startProgressTimer()
                startLevelMeter()

                let ok = player.play()
                if ok {
                    updateState(.playing)
                    logger.info("Playing audio segment \(index + 1)/\(total), duration: \(player.duration)s")
                } else {
                    logger.error("Audio player refused to play")
                    finishPlayback(
                        playback: playback,
                        result: TalkAudioPlaybackResult(finished: false, interruptedAt: nil, segmentIndex: index)
                    )
                }
            } catch {
                logger.error("Failed to create audio player: \(error.localizedDescription, privacy: .public)")
                emitEvent(.error(error.localizedDescription))
                finishPlayback(
                    playback: playback,
                    result: TalkAudioPlaybackResult(finished: false, interruptedAt: nil, segmentIndex: index)
                )
            }
        }
    }

    // MARK: - Queue Management

    /// Enqueues an audio segment for playback.
    /// - Parameters:
    ///   - data: The audio data
    ///   - format: The audio format
    func enqueue(data: Data, format: TalkAudioFormat = .mp3) {
        let segment = TalkAudioSegment(data: data, format: format)
        queue.append(segment)
        totalSegments = queue.count
        logger.debug("Enqueued audio segment, queue size: \(self.queue.count)")

        // Start playback if idle
        if state == .idle || state == .finished {
            Task {
                await playQueue()
            }
        }
    }

    /// Plays all queued segments sequentially.
    func playQueue() async {
        guard !queue.isEmpty else {
            logger.debug("Queue is empty, nothing to play")
            return
        }

        configureAudioSession()
        let segments = queue
        queue.removeAll()
        totalSegments = segments.count

        for (index, segment) in segments.enumerated() {
            let result = await playSegment(segment, index: index, total: segments.count)
            if !result.finished {
                logger.info("Queue playback stopped at segment \(index)")
                emitEvent(.segmentFinished(index: index))
                break
            }
            emitEvent(.segmentFinished(index: index))
        }

        if state != .paused {
            updateState(.finished)
        }
    }

    /// Clears all queued segments.
    func clearQueue() {
        queue.removeAll()
        totalSegments = 0
        emitEvent(.queueCleared)
        logger.debug("Queue cleared")
    }

    // MARK: - Streaming Support

    /// Begins streaming audio playback.
    func beginStreaming(format: TalkAudioFormat = .mp3) {
        stopInternal()
        isStreaming = true
        streamBuffer = Data()
        updateState(.loading)
        logger.info("Began streaming audio playback")
    }

    /// Appends streaming audio data.
    /// - Parameter chunk: The audio data chunk
    func appendStreamChunk(_ chunk: Data) {
        guard isStreaming else {
            logger.warning("Received stream chunk but not in streaming mode")
            return
        }

        streamBuffer.append(chunk)
        logger.debug("Appended stream chunk, buffer size: \(self.streamBuffer.count)")

        // Try to start playback once we have enough data
        if state == .loading, streamBuffer.count > 4096 {
            Task {
                await playStreamBuffer()
            }
        }
    }

    /// Ends streaming and plays remaining buffer.
    func endStreaming() async {
        guard isStreaming else { return }
        isStreaming = false

        if !streamBuffer.isEmpty {
            await playStreamBuffer()
        }

        logger.info("Ended streaming audio playback")
    }

    private func playStreamBuffer() async {
        guard !streamBuffer.isEmpty else { return }

        let data = streamBuffer
        streamBuffer = Data()

        _ = await play(data: data, format: .mp3)
    }

    // MARK: - Playback Controls

    /// Resumes playback if paused.
    func resume() {
        guard let player, state == .paused else { return }
        player.play()
        updateState(.playing)
        startProgressTimer()
        startLevelMeter()
        logger.info("Resumed playback")
    }

    /// Pauses playback.
    func pause() {
        guard let player, state == .playing else { return }
        player.pause()
        updateState(.paused)
        stopProgressTimer()
        stopLevelMeter()
        logger.info("Paused playback")
    }

    /// Toggles between play and pause.
    func togglePlayPause() {
        if state == .playing {
            pause()
        } else if state == .paused {
            resume()
        }
    }

    /// Stops playback and returns the interrupted time.
    /// - Returns: The time at which playback was stopped, or nil if not playing
    @discardableResult
    func stop() -> TimeInterval? {
        guard let player else { return nil }
        let time = player.currentTime
        stopInternal(interruptedAt: time)
        return time
    }

    /// Seeks to a specific time.
    /// - Parameter time: The target time in seconds
    func seek(to time: TimeInterval) {
        guard let player else { return }
        let clampedTime = max(0, min(time, player.duration))
        player.currentTime = clampedTime
        currentTime = clampedTime
        emitEvent(.progressUpdated(current: currentTime, duration: duration))
        logger.debug("Seeked to \(clampedTime)s")
    }

    /// Seeks forward by the specified amount.
    /// - Parameter seconds: Seconds to skip forward
    func seekForward(by seconds: TimeInterval = 10) {
        guard let player else { return }
        seek(to: player.currentTime + seconds)
    }

    /// Seeks backward by the specified amount.
    /// - Parameter seconds: Seconds to skip backward
    func seekBackward(by seconds: TimeInterval = 10) {
        guard let player else { return }
        seek(to: player.currentTime - seconds)
    }

    // MARK: - Speed Control

    /// Sets the playback speed.
    /// - Parameter speed: Speed multiplier (0.5 - 2.0)
    func setSpeed(_ speed: Float) {
        let clampedSpeed = max(Self.minSpeed, min(Self.maxSpeed, speed))
        playbackSpeed = clampedSpeed
        player?.rate = clampedSpeed
        emitEvent(.speedChanged(clampedSpeed))
        logger.debug("Set playback speed to \(clampedSpeed)x")
    }

    /// Increases playback speed by one step.
    func increaseSpeed() {
        setSpeed(playbackSpeed + Self.speedStep)
    }

    /// Decreases playback speed by one step.
    func decreaseSpeed() {
        setSpeed(playbackSpeed - Self.speedStep)
    }

    /// Resets playback speed to normal (1.0x).
    func resetSpeed() {
        setSpeed(1.0)
    }

    // MARK: - Volume Control

    /// Sets the playback volume.
    /// - Parameter level: Volume level (0.0 - 1.0)
    func setVolume(_ level: Float) {
        let clampedVolume = max(Self.minVolume, min(Self.maxVolume, level))
        volume = clampedVolume
        player?.volume = clampedVolume
        emitEvent(.volumeChanged(clampedVolume))
        logger.debug("Set volume to \(clampedVolume)")
    }

    /// Mutes playback (sets volume to 0).
    func mute() {
        setVolume(0)
    }

    /// Unmutes playback (restores to full volume).
    func unmute() {
        setVolume(1.0)
    }

    // MARK: - AVAudioPlayerDelegate

    nonisolated func audioPlayerDidFinishPlaying(_ player: AVAudioPlayer, successfully flag: Bool) {
        Task { @MainActor in
            self.handlePlaybackFinished(successfully: flag)
        }
    }

    nonisolated func audioPlayerDecodeErrorDidOccur(_ player: AVAudioPlayer, error: Error?) {
        Task { @MainActor in
            let message = error?.localizedDescription ?? "Unknown decode error"
            self.logger.error("Audio decode error: \(message, privacy: .public)")
            self.emitEvent(.error(message))
            self.stopInternal(finished: false)
        }
    }

    private func handlePlaybackFinished(successfully flag: Bool) {
        stopInternal(finished: flag)
    }

    // MARK: - Progress Tracking

    private func startProgressTimer() {
        stopProgressTimer()
        progressTimer = Timer.scheduledTimer(withTimeInterval: 0.1, repeats: true) { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.updateProgress()
            }
        }
    }

    private func stopProgressTimer() {
        progressTimer?.invalidate()
        progressTimer = nil
    }

    private func updateProgress() {
        guard let player, state == .playing else { return }
        currentTime = player.currentTime
        emitEvent(.progressUpdated(current: currentTime, duration: duration))
    }

    // MARK: - Level Metering

    private func startLevelMeter() {
        stopLevelMeter()
        player?.isMeteringEnabled = true

        levelTimer = Timer.scheduledTimer(withTimeInterval: 0.05, repeats: true) { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.updateLevels()
            }
        }
    }

    private func stopLevelMeter() {
        levelTimer?.invalidate()
        levelTimer = nil
        player?.isMeteringEnabled = false
        audioLevels = []
    }

    private func updateLevels() {
        guard let player, state == .playing else { return }
        player.updateMeters()

        let channelCount = player.numberOfChannels
        var levels: [Float] = []

        for channel in 0..<channelCount {
            let power = player.averagePower(forChannel: channel)
            // Convert dB to linear scale (0-1)
            let normalizedPower = max(0, min(1, (power + 60) / 60))
            levels.append(normalizedPower)
        }

        audioLevels = levels
    }

    // MARK: - Watchdog

    private func armWatchdog(playback: Playback) {
        playback.setWatchdog(Task { @MainActor [weak self] in
            guard let self else { return }

            // Initial check after startup
            do {
                try await Task.sleep(nanoseconds: 650_000_000)
            } catch {
                return
            }
            if Task.isCancelled { return }

            guard self.playback === playback else { return }
            if self.player?.isPlaying != true, self.state == .playing {
                self.logger.error("Audio player did not start playing")
                self.finishPlayback(
                    playback: playback,
                    result: TalkAudioPlaybackResult(finished: false, interruptedAt: nil, segmentIndex: playback.segmentIndex)
                )
                return
            }

            // Timeout watchdog
            let audioDuration = self.player?.duration ?? 0
            let timeoutSeconds = min(max(2.0, audioDuration + 2.0), 5 * 60.0)
            do {
                try await Task.sleep(nanoseconds: UInt64(timeoutSeconds * 1_000_000_000))
            } catch {
                return
            }
            if Task.isCancelled { return }

            guard self.playback === playback else { return }
            guard self.player?.isPlaying == true else { return }
            self.logger.error("Audio player watchdog fired")
            self.finishPlayback(
                playback: playback,
                result: TalkAudioPlaybackResult(finished: false, interruptedAt: nil, segmentIndex: playback.segmentIndex)
            )
        })
    }

    // MARK: - Internal Stop

    private func stopInternal(finished: Bool = false, interruptedAt: TimeInterval? = nil) {
        guard let playback else {
            player?.stop()
            player = nil
            updateState(.idle)
            return
        }

        let result = TalkAudioPlaybackResult(
            finished: finished,
            interruptedAt: interruptedAt,
            segmentIndex: playback.segmentIndex
        )
        finishPlayback(playback: playback, result: result)
    }

    private func finishPlayback(playback: Playback, result: TalkAudioPlaybackResult) {
        playback.cancelWatchdog()
        playback.finish(result)

        guard self.playback === playback else { return }
        self.playback = nil
        stopProgressTimer()
        stopLevelMeter()
        player?.stop()
        player = nil
        currentTime = 0

        if result.finished {
            updateState(.finished)
        } else if result.interruptedAt != nil {
            updateState(.paused)
        } else {
            updateState(.idle)
        }
    }

    // MARK: - State Management

    private func updateState(_ newState: TalkAudioPlaybackState) {
        guard state != newState else { return }
        state = newState
        emitEvent(.stateChanged(newState))
        logger.debug("State changed to \(newState.rawValue)")
    }

    // MARK: - Event Emission

    private func emitEvent(_ event: TalkAudioPlaybackEvent) {
        eventSubject.send(event)
    }
}
