import Combine
import Foundation
import Observation
import OSLog

// MARK: - Voice Mode

/// The different voice input modes supported.
enum VoiceMode: String, Sendable, CaseIterable {
    case wakeWord
    case pushToTalk
    case talkMode
}

// MARK: - Session Event

/// Events emitted by the voice session coordinator.
enum VoiceSessionEvent: Sendable {
    case sessionStarted(mode: VoiceMode)
    case sessionPaused(mode: VoiceMode)
    case sessionResumed(mode: VoiceMode)
    case sessionEnded(mode: VoiceMode)
    case modeTransition(from: VoiceMode?, to: VoiceMode?)
    case conflict(requested: VoiceMode, active: VoiceMode)
}

// MARK: - Session State

/// State of a voice session.
enum VoiceSessionState: String, Sendable {
    case inactive
    case active
    case paused
}

// MARK: - Session Info

/// Information about an active voice session.
struct VoiceSessionInfo: Sendable {
    let mode: VoiceMode
    let state: VoiceSessionState
    let startedAt: Date
    let sessionID: UUID
}

// MARK: - VoiceSessionCoordinator

/// Coordinates voice sessions between wake word, PTT, and talk mode.
/// Ensures only one voice mode is active at a time and manages transitions.
@MainActor
@Observable
final class VoiceSessionCoordinator {
    static let shared = VoiceSessionCoordinator()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "voice")

    // MARK: - Observable State

    private(set) var activeMode: VoiceMode?
    private(set) var sessionState: VoiceSessionState = .inactive
    private(set) var currentSessionID: UUID?

    /// True if any voice session is currently active
    var isActive: Bool { sessionState == .active }

    /// True if a session is paused
    var isPaused: Bool { sessionState == .paused }

    /// True if the system is idle (no active or paused session)
    var isIdle: Bool { sessionState == .inactive }

    // MARK: - Event Stream

    private var eventContinuations: [UUID: AsyncStream<VoiceSessionEvent>.Continuation] = [:]

    /// Subscribe to session events.
    func subscribe() -> AsyncStream<VoiceSessionEvent> {
        let id = UUID()
        return AsyncStream(bufferingPolicy: .bufferingNewest(50)) { [weak self] continuation in
            Task { @MainActor in
                self?.eventContinuations[id] = continuation
            }
            continuation.onTermination = { _ in
                Task { @MainActor in
                    self?.eventContinuations.removeValue(forKey: id)
                }
            }
        }
    }

    // MARK: - Private State

    private var sessions: [VoiceMode: VoiceSessionInfo] = [:]
    private var pausedSessions: Set<VoiceMode> = []
    private var transitionLock = false

    private init() {
        logger.info("voice coordinator initialized")
    }

    // MARK: - Session Management

    /// Request to start a voice session for the given mode.
    /// Returns true if the session was granted, false if denied due to conflict.
    func requestSession(mode: VoiceMode) -> Bool {
        guard !transitionLock else {
            logger.warning("session request denied: transition in progress")
            return false
        }

        // Check for conflicts
        if let active = activeMode, active != mode {
            logger.info("session conflict: \(mode.rawValue) requested while \(active.rawValue) active")
            emit(.conflict(requested: mode, active: active))

            // PTT has highest priority - it can interrupt wake word
            if mode == .pushToTalk && active == .wakeWord {
                logger.info("ptt interrupting wake word")
                pauseSession(mode: .wakeWord)
            } else if mode == .talkMode {
                // Talk mode takes over everything
                logger.info("talk mode interrupting \(active.rawValue)")
                pauseSession(mode: active)
            } else {
                return false
            }
        }

        return startSession(mode: mode)
    }

    /// Start a voice session for the given mode.
    @discardableResult
    func startSession(mode: VoiceMode) -> Bool {
        guard activeMode == nil || activeMode == mode else {
            logger.warning("cannot start \(mode.rawValue): \(activeMode?.rawValue ?? "nil") already active")
            return false
        }

        let sessionID = UUID()
        let session = VoiceSessionInfo(
            mode: mode,
            state: .active,
            startedAt: Date(),
            sessionID: sessionID
        )

        sessions[mode] = session
        activeMode = mode
        sessionState = .active
        currentSessionID = sessionID

        logger.info("session started: \(mode.rawValue) id=\(sessionID.uuidString.prefix(8))")
        emit(.sessionStarted(mode: mode))

        return true
    }

    /// Pause the current session for the given mode.
    func pauseSession(mode: VoiceMode) {
        guard sessions[mode] != nil else { return }
        guard activeMode == mode else { return }

        pausedSessions.insert(mode)

        if activeMode == mode {
            activeMode = nil
            sessionState = .paused
        }

        logger.info("session paused: \(mode.rawValue)")
        emit(.sessionPaused(mode: mode))
    }

    /// Resume a paused session.
    func resumeSession(mode: VoiceMode) {
        guard pausedSessions.contains(mode) else { return }
        guard sessions[mode] != nil else { return }

        // Can only resume if nothing else is active
        guard activeMode == nil else {
            logger.warning("cannot resume \(mode.rawValue): \(activeMode?.rawValue ?? "nil") is active")
            return
        }

        pausedSessions.remove(mode)
        activeMode = mode
        sessionState = .active

        logger.info("session resumed: \(mode.rawValue)")
        emit(.sessionResumed(mode: mode))
    }

    /// End the session for the given mode.
    func endSession(mode: VoiceMode) {
        guard sessions[mode] != nil else { return }

        let wasActive = activeMode == mode
        sessions.removeValue(forKey: mode)
        pausedSessions.remove(mode)

        if wasActive {
            activeMode = nil
            sessionState = .inactive
            currentSessionID = nil
        }

        logger.info("session ended: \(mode.rawValue)")
        emit(.sessionEnded(mode: mode))

        // Check if we should resume a paused session
        if wasActive {
            resumePausedSessionIfNeeded()
        }
    }

    /// End all active sessions.
    func endAllSessions() {
        let activeModes = Array(sessions.keys)
        for mode in activeModes {
            endSession(mode: mode)
        }

        activeMode = nil
        sessionState = .inactive
        currentSessionID = nil
        pausedSessions.removeAll()

        logger.info("all sessions ended")
    }

    // MARK: - Mode Transitions

    /// Transition from the current mode to a new mode.
    /// Handles cleanup of the old mode and initialization of the new mode.
    func transition(to newMode: VoiceMode?) async {
        guard !transitionLock else {
            logger.warning("transition denied: already in progress")
            return
        }

        transitionLock = true
        defer { transitionLock = false }

        let oldMode = activeMode
        logger.info("transitioning from \(oldMode?.rawValue ?? "nil") to \(newMode?.rawValue ?? "nil")")

        emit(.modeTransition(from: oldMode, to: newMode))

        // End old mode
        if let old = oldMode {
            await cleanupMode(old)
            endSession(mode: old)
        }

        // Start new mode
        if let new = newMode {
            _ = startSession(mode: new)
            await initializeMode(new)
        }
    }

    // MARK: - Query Methods

    /// Get information about the current session, if any.
    func currentSession() -> VoiceSessionInfo? {
        guard let mode = activeMode else { return nil }
        return sessions[mode]
    }

    /// Check if a specific mode is currently active.
    func isActive(mode: VoiceMode) -> Bool {
        activeMode == mode && sessionState == .active
    }

    /// Check if a specific mode is currently paused.
    func isPaused(mode: VoiceMode) -> Bool {
        pausedSessions.contains(mode)
    }

    /// Check if a session can be started for the given mode.
    func canStart(mode: VoiceMode) -> Bool {
        if sessionState == .inactive { return true }

        // PTT can interrupt wake word
        if mode == .pushToTalk && activeMode == .wakeWord { return true }

        // Talk mode can interrupt anything
        if mode == .talkMode { return true }

        return false
    }

    // MARK: - Integration Methods

    /// Called when wake word detection triggers.
    func wakeWordTriggered() {
        guard canStart(mode: .wakeWord) else {
            logger.debug("wake word trigger ignored: session active")
            return
        }

        _ = startSession(mode: .wakeWord)
    }

    /// Called when wake word processing completes.
    func wakeWordCompleted() {
        endSession(mode: .wakeWord)
    }

    /// Called when talk mode is toggled.
    func setTalkModeEnabled(_ enabled: Bool) async {
        if enabled {
            // Talk mode takes priority over everything
            if activeMode != .talkMode {
                await transition(to: .talkMode)
            }
        } else {
            if activeMode == .talkMode {
                await transition(to: nil)
            }
        }
    }

    // MARK: - Private Methods

    private func emit(_ event: VoiceSessionEvent) {
        for (_, continuation) in eventContinuations {
            continuation.yield(event)
        }
    }

    private func resumePausedSessionIfNeeded() {
        // Priority: talk mode > wake word > ptt
        if pausedSessions.contains(.talkMode) {
            resumeSession(mode: .talkMode)
        } else if pausedSessions.contains(.wakeWord) {
            resumeSession(mode: .wakeWord)
        }
        // PTT is user-initiated, don't auto-resume
    }

    private func cleanupMode(_ mode: VoiceMode) async {
        switch mode {
        case .wakeWord:
            // VoiceWakeRuntime handles its own cleanup
            break
        case .pushToTalk:
            await VoicePushToTalkCapture.shared.end()
        case .talkMode:
            await TalkModeRuntime.shared.setEnabled(false)
        }
    }

    private func initializeMode(_ mode: VoiceMode) async {
        switch mode {
        case .wakeWord:
            // Wake word runtime handles its own initialization
            break
        case .pushToTalk:
            // PTT is initialized on-demand via hotkey
            break
        case .talkMode:
            await TalkModeRuntime.shared.setEnabled(true)
        }
    }
}

// MARK: - Notification Names

extension Notification.Name {
    static let voiceSessionStarted = Notification.Name("nexus.voice.sessionStarted")
    static let voiceSessionEnded = Notification.Name("nexus.voice.sessionEnded")
    static let voiceModeChanged = Notification.Name("nexus.voice.modeChanged")
}

// MARK: - Test Helpers

#if DEBUG
extension VoiceSessionCoordinator {
    static func _testReset() {
        shared.sessions.removeAll()
        shared.pausedSessions.removeAll()
        shared.activeMode = nil
        shared.sessionState = .inactive
        shared.currentSessionID = nil
        shared.transitionLock = false
    }

    static func _testSetActiveMode(_ mode: VoiceMode?) {
        shared.activeMode = mode
        shared.sessionState = mode == nil ? .inactive : .active
    }

    static func _testSetPaused(_ mode: VoiceMode) {
        shared.pausedSessions.insert(mode)
        if shared.activeMode == mode {
            shared.activeMode = nil
            shared.sessionState = .paused
        }
    }
}
#endif
