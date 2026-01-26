import Foundation
import OSLog

// MARK: - WatchdogState

/// Connection watchdog states
enum WatchdogState: String, Sendable {
    case idle
    case monitoring
    case recovering
    case backingOff
}

// MARK: - ConnectionWatchdog

/// Monitors gateway connection and triggers recovery
///
/// The watchdog continuously monitors the connection health by:
/// - Tracking "tick" events from the gateway
/// - Polling the health endpoint periodically
/// - Triggering recovery when connection appears dead
///
/// Based on Clawdbot's watchdog loop and retry patterns.
@MainActor
@Observable
final class ConnectionWatchdog {
    static let shared = ConnectionWatchdog()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "watchdog")

    // MARK: - State

    private(set) var state: WatchdogState = .idle
    private(set) var lastTick: Date?
    private(set) var lastHealthCheck: Date?
    private(set) var recoveryAttempts: Int = 0
    private(set) var nextRetryAt: Date?
    private(set) var lastRecoveryReason: String?

    // MARK: - Configuration

    /// How often to check connection health (30 seconds)
    let watchdogInterval: TimeInterval = 30

    /// No tick for this long = connection dead (90 seconds)
    let tickTimeout: TimeInterval = 90

    /// Health poll interval (60 seconds)
    let healthCheckInterval: TimeInterval = 60

    /// Maximum recovery attempts before extended backoff
    let maxRecoveryAttempts: Int = 10

    // MARK: - Backoff Configuration

    /// Initial backoff delay (500ms)
    private let baseBackoff: TimeInterval = 0.5

    /// Maximum backoff delay (30 seconds)
    private let maxBackoff: TimeInterval = 30

    /// Current backoff delay
    private var currentBackoff: TimeInterval = 0.5

    // MARK: - Tasks

    private var watchdogTask: Task<Void, Never>?
    private var healthTask: Task<Void, Never>?
    private var recoveryTask: Task<Void, Never>?

    // MARK: - Initialization

    private init() {}

    // MARK: - Lifecycle

    /// Start the watchdog monitoring
    func start() {
        guard state == .idle else {
            logger.debug("Watchdog already running in state: \(self.state.rawValue)")
            return
        }

        state = .monitoring
        recoveryAttempts = 0
        currentBackoff = baseBackoff
        lastTick = Date() // Initialize to now to avoid immediate recovery

        startWatchdogLoop()
        startHealthPolling()

        logger.info("Watchdog started with interval=\(self.watchdogInterval)s, timeout=\(self.tickTimeout)s")
    }

    /// Stop the watchdog monitoring
    func stop() {
        watchdogTask?.cancel()
        healthTask?.cancel()
        recoveryTask?.cancel()

        watchdogTask = nil
        healthTask = nil
        recoveryTask = nil

        state = .idle
        nextRetryAt = nil
        lastRecoveryReason = nil

        logger.info("Watchdog stopped")
    }

    // MARK: - Tick Handling

    /// Record a tick from the gateway (heartbeat, event, or response)
    ///
    /// Call this whenever the gateway sends any data, indicating the connection is alive.
    func recordTick() {
        lastTick = Date()

        // Reset backoff on successful tick if we were backing off
        if state == .backingOff {
            let previousAttempts = recoveryAttempts
            state = .monitoring
            currentBackoff = baseBackoff
            recoveryAttempts = 0
            nextRetryAt = nil
            lastRecoveryReason = nil
            logger.info("Connection recovered after \(previousAttempts) attempts")
        }
    }

    /// Record a tick with additional context
    func recordTick(source: String) {
        logger.debug("Tick received from: \(source)")
        recordTick()
    }

    // MARK: - Watchdog Loop

    private func startWatchdogLoop() {
        watchdogTask?.cancel()
        watchdogTask = Task { [weak self] in
            while !Task.isCancelled {
                await self?.checkConnection()
                try? await Task.sleep(for: .seconds(self?.watchdogInterval ?? 30))
            }
        }
    }

    private func checkConnection() async {
        // Skip if already recovering or backing off
        guard state == .monitoring else {
            logger.debug("Skipping connection check, state=\(self.state.rawValue)")
            return
        }

        // Check tick timeout
        if let lastTick = lastTick {
            let elapsed = Date().timeIntervalSince(lastTick)
            if elapsed > tickTimeout {
                logger.warning("No tick received for \(Int(elapsed))s (threshold: \(Int(self.tickTimeout))s), triggering recovery")
                await triggerRecovery(reason: "tick_timeout")
                return
            }
        } else {
            // No tick ever received - this shouldn't happen if start() initializes lastTick
            logger.warning("No tick ever received, triggering recovery")
            await triggerRecovery(reason: "no_ticks")
            return
        }

        // Check ControlChannel state
        let channelState = ControlChannel.shared.state
        switch channelState {
        case .disconnected:
            logger.warning("ControlChannel disconnected, triggering recovery")
            await triggerRecovery(reason: "channel_disconnected")
        case .degraded(let message):
            logger.warning("ControlChannel degraded: \(message), triggering recovery")
            await triggerRecovery(reason: "channel_degraded:\(message)")
        case .connecting, .connected:
            break
        }
    }

    // MARK: - Health Polling

    private func startHealthPolling() {
        healthTask?.cancel()
        healthTask = Task { [weak self] in
            while !Task.isCancelled {
                await self?.pollHealth()
                try? await Task.sleep(for: .seconds(self?.healthCheckInterval ?? 60))
            }
        }
    }

    private func pollHealth() async {
        do {
            let data = try await ControlChannel.shared.health(timeout: 15)
            lastHealthCheck = Date()

            // Parse health response
            struct HealthResponse: Decodable {
                let ok: Bool?
                let state: String?
                let summary: String?
                let message: String?
            }

            if let response = try? JSONDecoder().decode(HealthResponse.self, from: data) {
                if response.ok == true || response.state == "ok" {
                    await HealthStore.shared.refresh()
                    recordTick(source: "health_poll")
                } else {
                    let summary = response.summary ?? response.message ?? "Unknown"
                    logger.warning("Health check returned non-ok: \(summary)")
                }
            }
        } catch {
            logger.warning("Health check failed: \(error.localizedDescription)")
            // Don't trigger recovery here - let the watchdog loop handle it based on tick timeout
        }
    }

    // MARK: - Recovery

    private func triggerRecovery(reason: String) async {
        guard state == .monitoring else {
            logger.debug("Skipping recovery trigger, state=\(self.state.rawValue)")
            return
        }

        state = .recovering
        recoveryAttempts += 1
        lastRecoveryReason = reason

        logger.info("Starting recovery attempt \(self.recoveryAttempts)/\(self.maxRecoveryAttempts) (reason: \(reason))")

        // Check max attempts
        if recoveryAttempts > maxRecoveryAttempts {
            logger.error("Max recovery attempts (\(self.maxRecoveryAttempts)) exceeded")
            state = .backingOff
            await MainActor.run {
                HealthStore.shared.markUnknown()
            }
            scheduleBackoffRetry()
            return
        }

        // Attempt reconnection with backoff
        do {
            // Disconnect first
            await ControlChannel.shared.disconnect()

            // Wait for backoff period
            let backoffMs = Int(currentBackoff * 1000)
            logger.debug("Waiting \(backoffMs)ms before reconnect")
            try await Task.sleep(for: .milliseconds(backoffMs))

            // Reconnect
            await ControlChannel.shared.refreshEndpoint(reason: "watchdog_recovery")

            // Verify connection
            let channelState = ControlChannel.shared.state
            if case .connected = channelState {
                // Success
                state = .monitoring
                lastTick = Date()
                logger.info("Recovery successful on attempt \(self.recoveryAttempts)")
            } else {
                throw NSError(
                    domain: "ConnectionWatchdog",
                    code: 1,
                    userInfo: [NSLocalizedDescriptionKey: "Connection not established after refresh"]
                )
            }

        } catch {
            logger.error("Recovery attempt \(self.recoveryAttempts) failed: \(error.localizedDescription)")

            // Increase backoff using exponential backoff
            currentBackoff = min(currentBackoff * 2, maxBackoff)

            // Schedule next attempt
            state = .backingOff
            scheduleBackoffRetry()
        }
    }

    private func scheduleBackoffRetry() {
        nextRetryAt = Date().addingTimeInterval(currentBackoff)

        recoveryTask?.cancel()
        recoveryTask = Task { [weak self] in
            guard let self = self else { return }

            let backoffSeconds = self.currentBackoff
            logger.info("Next retry in \(String(format: "%.1f", backoffSeconds))s")

            try? await Task.sleep(for: .seconds(backoffSeconds))

            if !Task.isCancelled {
                await MainActor.run {
                    self.state = .monitoring
                }
                await self.triggerRecovery(reason: "scheduled_retry")
            }
        }
    }

    // MARK: - Manual Recovery

    /// Force an immediate reconnection attempt
    func forceReconnect() async {
        logger.info("Force reconnect requested")

        // Cancel any pending retry
        recoveryTask?.cancel()
        recoveryTask = nil

        // Reset state
        state = .monitoring
        currentBackoff = baseBackoff
        recoveryAttempts = 0
        nextRetryAt = nil

        await triggerRecovery(reason: "manual")
    }

    /// Reset backoff state without triggering reconnection
    func resetBackoff() {
        currentBackoff = baseBackoff
        recoveryAttempts = 0
        nextRetryAt = nil
        lastRecoveryReason = nil

        recoveryTask?.cancel()
        recoveryTask = nil

        if state == .backingOff {
            state = .monitoring
        }

        logger.info("Backoff reset")
    }

    // MARK: - Status

    /// Whether the watchdog is actively monitoring
    var isActive: Bool {
        state != .idle
    }

    /// Whether recovery is in progress
    var isRecovering: Bool {
        state == .recovering || state == .backingOff
    }

    /// Time until next retry, if scheduled
    var timeUntilRetry: TimeInterval? {
        guard let nextRetryAt = nextRetryAt else { return nil }
        let interval = nextRetryAt.timeIntervalSinceNow
        return interval > 0 ? interval : nil
    }

    /// Summary of current watchdog status
    var statusSummary: String {
        switch state {
        case .idle:
            return "Watchdog not running"
        case .monitoring:
            if let lastTick = lastTick {
                let elapsed = Int(Date().timeIntervalSince(lastTick))
                return "Monitoring (last tick \(elapsed)s ago)"
            }
            return "Monitoring"
        case .recovering:
            return "Recovering (attempt \(recoveryAttempts))"
        case .backingOff:
            if let timeUntilRetry = timeUntilRetry {
                return "Backing off (retry in \(Int(timeUntilRetry))s)"
            }
            return "Backing off"
        }
    }
}
