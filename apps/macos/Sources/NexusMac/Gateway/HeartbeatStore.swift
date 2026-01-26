import Foundation
import Observation
import OSLog
import SwiftUI

// MARK: - ConnectionQuality

enum ConnectionQuality: String, CaseIterable, Sendable {
    case excellent = "Excellent"
    case good = "Good"
    case fair = "Fair"
    case poor = "Poor"
    case disconnected = "Disconnected"

    var color: Color {
        switch self {
        case .excellent: .green
        case .good: .green.opacity(0.8)
        case .fair: .yellow
        case .poor: .orange
        case .disconnected: .red
        }
    }

    var systemImage: String {
        switch self {
        case .excellent: "heart.fill"
        case .good: "heart.fill"
        case .fair: "heart"
        case .poor: "heart.slash"
        case .disconnected: "heart.slash.fill"
        }
    }
}

// MARK: - HeartbeatRecord

struct HeartbeatRecord: Identifiable, Sendable {
    let id: UUID
    let timestamp: Date
    let event: ControlHeartbeatEvent
    let intervalSincePrevious: TimeInterval?

    init(event: ControlHeartbeatEvent, previousTimestamp: Date?) {
        self.id = UUID()
        self.timestamp = Date(timeIntervalSince1970: event.ts)
        self.event = event
        if let previous = previousTimestamp {
            self.intervalSincePrevious = timestamp.timeIntervalSince(previous)
        } else {
            self.intervalSincePrevious = nil
        }
    }
}

// MARK: - HeartbeatStore

@MainActor
@Observable
final class HeartbeatStore {
    static let shared = HeartbeatStore()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "heartbeat")

    // MARK: - Configuration

    /// Timeout threshold in seconds after which connection is considered unhealthy
    var timeoutThreshold: TimeInterval = 30

    /// Maximum number of heartbeat records to keep in history
    private let maxHistoryCount = 100

    // MARK: - Published State

    private(set) var lastHeartbeatTime: Date?
    private(set) var heartbeatInterval: TimeInterval = 0
    private(set) var missedHeartbeats: Int = 0
    private(set) var connectionQuality: ConnectionQuality = .disconnected
    private(set) var history: [HeartbeatRecord] = []
    private(set) var lastEvent: ControlHeartbeatEvent?

    // MARK: - Internal State

    private var observer: NSObjectProtocol?
    private var healthCheckTask: Task<Void, Never>?
    private var expectedInterval: TimeInterval = 10  // Expected heartbeat interval
    private var lastHealthCheck: Date?

    // MARK: - Initialization

    private init() {
        setupNotificationObserver()
        startHealthCheck()
        logger.info("HeartbeatStore initialized with timeout threshold: \(self.timeoutThreshold)s")
    }

    @MainActor
    deinit {
        if let observer { NotificationCenter.default.removeObserver(observer) }
        healthCheckTask?.cancel()
    }

    // MARK: - Setup

    private func setupNotificationObserver() {
        observer = NotificationCenter.default.addObserver(
            forName: .controlHeartbeat,
            object: nil,
            queue: .main
        ) { [weak self] note in
            guard let event = note.object as? ControlHeartbeatEvent else { return }
            Task { @MainActor in
                self?.recordHeartbeat(event: event)
            }
        }
    }

    private func startHealthCheck() {
        healthCheckTask?.cancel()
        healthCheckTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 5_000_000_000)  // Check every 5 seconds
                await MainActor.run { self?.checkHealth() }
            }
        }
    }

    // MARK: - Public Methods

    /// Record a heartbeat event from the control channel
    func recordHeartbeat(event: ControlHeartbeatEvent) {
        let previousTimestamp = lastHeartbeatTime
        let record = HeartbeatRecord(event: event, previousTimestamp: previousTimestamp)

        // Update state
        lastEvent = event
        lastHeartbeatTime = record.timestamp

        // Add to history, maintaining max count
        history.insert(record, at: 0)
        if history.count > maxHistoryCount {
            history = Array(history.prefix(maxHistoryCount))
        }

        // Reset missed heartbeats on successful heartbeat
        if event.status == "ok" || event.status == "sent" {
            missedHeartbeats = 0
        }

        // Calculate average interval from recent history
        calculateAverageInterval()

        // Update connection quality
        updateConnectionQuality()

        logger.debug("Heartbeat recorded: status=\(event.status), interval=\(record.intervalSincePrevious ?? 0, format: .fixed(precision: 2))s")

        // Post notification for health status change
        NotificationCenter.default.post(name: .heartbeatHealthChanged, object: connectionQuality)
    }

    /// Evaluate current health based on heartbeat timing
    func checkHealth() {
        lastHealthCheck = Date()

        guard let lastBeat = lastHeartbeatTime else {
            // No heartbeats received yet
            if connectionQuality != .disconnected {
                connectionQuality = .disconnected
                logger.warning("No heartbeats received, marking as disconnected")
                NotificationCenter.default.post(name: .heartbeatHealthChanged, object: connectionQuality)
            }
            return
        }

        let timeSinceLastBeat = Date().timeIntervalSince(lastBeat)

        // Check for timeout
        if timeSinceLastBeat > timeoutThreshold {
            let missedCount = Int(timeSinceLastBeat / max(expectedInterval, 1))
            if missedCount != missedHeartbeats {
                missedHeartbeats = missedCount
                logger.warning("Heartbeat timeout: \(timeSinceLastBeat, format: .fixed(precision: 1))s since last beat, \(missedCount) missed")
            }
        }

        // Update quality based on timing
        let previousQuality = connectionQuality
        updateConnectionQuality()

        if previousQuality != connectionQuality {
            logger.info("Connection quality changed: \(previousQuality.rawValue) -> \(self.connectionQuality.rawValue)")
            NotificationCenter.default.post(name: .heartbeatHealthChanged, object: connectionQuality)
        }
    }

    /// Reset all heartbeat tracking state
    func reset() {
        logger.info("Resetting heartbeat store")

        lastHeartbeatTime = nil
        heartbeatInterval = 0
        missedHeartbeats = 0
        connectionQuality = .disconnected
        history.removeAll()
        lastEvent = nil
        expectedInterval = 10

        NotificationCenter.default.post(name: .heartbeatHealthChanged, object: connectionQuality)
    }

    // MARK: - Computed Properties

    /// Time since last heartbeat, or nil if no heartbeats received
    var timeSinceLastHeartbeat: TimeInterval? {
        guard let lastBeat = lastHeartbeatTime else { return nil }
        return Date().timeIntervalSince(lastBeat)
    }

    /// Whether the connection is currently healthy
    var isHealthy: Bool {
        switch connectionQuality {
        case .excellent, .good, .fair:
            return true
        case .poor, .disconnected:
            return false
        }
    }

    /// Formatted string for last heartbeat time
    var lastHeartbeatFormatted: String {
        guard let lastBeat = lastHeartbeatTime else {
            return "Never"
        }

        let formatter = RelativeDateTimeFormatter()
        formatter.unitsStyle = .abbreviated
        return formatter.localizedString(for: lastBeat, relativeTo: Date())
    }

    // MARK: - Private Methods

    private func calculateAverageInterval() {
        let intervalsToConsider = history
            .prefix(20)  // Use last 20 heartbeats
            .compactMap { $0.intervalSincePrevious }
            .filter { $0 > 0 && $0 < 120 }  // Filter outliers

        guard !intervalsToConsider.isEmpty else { return }

        let sum = intervalsToConsider.reduce(0, +)
        heartbeatInterval = sum / Double(intervalsToConsider.count)

        // Update expected interval based on observed pattern
        if heartbeatInterval > 0 {
            expectedInterval = heartbeatInterval
        }
    }

    private func updateConnectionQuality() {
        guard let lastBeat = lastHeartbeatTime else {
            connectionQuality = .disconnected
            return
        }

        let timeSinceLastBeat = Date().timeIntervalSince(lastBeat)

        // Check recent event status
        let recentFailures = history
            .prefix(5)
            .filter { $0.event.status == "failed" }
            .count

        // Determine quality based on timing and failures
        if timeSinceLastBeat > timeoutThreshold {
            connectionQuality = .disconnected
        } else if timeSinceLastBeat > timeoutThreshold * 0.75 || recentFailures >= 3 {
            connectionQuality = .poor
        } else if timeSinceLastBeat > timeoutThreshold * 0.5 || recentFailures >= 2 {
            connectionQuality = .fair
        } else if timeSinceLastBeat > timeoutThreshold * 0.25 || recentFailures >= 1 {
            connectionQuality = .good
        } else {
            connectionQuality = .excellent
        }
    }
}

// MARK: - Notifications

extension Notification.Name {
    static let heartbeatHealthChanged = Notification.Name("nexus.heartbeat.healthChanged")
}
