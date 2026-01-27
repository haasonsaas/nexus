import Foundation
import Observation
import OSLog

// MARK: - Stored Event Model

/// Persistent wrapper for ControlAgentEvent with additional metadata
struct StoredAgentEvent: Codable, Identifiable, Sendable {
    let id: UUID
    let runId: String
    let seq: Int
    let stream: String
    let timestamp: Date
    let data: [String: AnyCodable]
    let summary: String?
    let receivedAt: Date

    init(from event: ControlAgentEvent) {
        self.id = UUID()
        self.runId = event.runId
        self.seq = event.seq
        self.stream = event.stream
        self.timestamp = Date(timeIntervalSince1970: event.ts / 1000)
        self.data = event.data
        self.summary = event.summary
        self.receivedAt = Date()
    }
}

/// Stream types for filtering agent events
enum AgentEventStreamType: String, CaseIterable, Codable, Sendable {
    case all = "all"
    case output = "output"
    case toolUse = "tool_use"
    case toolResult = "tool_result"
    case error = "error"
    case thinking = "thinking"
    case message = "message"
    case status = "status"

    var displayName: String {
        switch self {
        case .all: return "All"
        case .output: return "Output"
        case .toolUse: return "Tool Use"
        case .toolResult: return "Tool Result"
        case .error: return "Error"
        case .thinking: return "Thinking"
        case .message: return "Message"
        case .status: return "Status"
        }
    }

    var systemImage: String {
        switch self {
        case .all: return "square.stack.3d.up"
        case .output: return "text.bubble"
        case .toolUse: return "wrench.and.screwdriver"
        case .toolResult: return "checkmark.circle"
        case .error: return "exclamationmark.triangle"
        case .thinking: return "brain"
        case .message: return "message"
        case .status: return "info.circle"
        }
    }
}

/// Session summary for filtering
struct AgentSessionSummary: Identifiable, Sendable {
    let id: String
    let eventCount: Int
    let firstEventAt: Date
    let lastEventAt: Date
    let streamTypes: Set<String>
}

// MARK: - Agent Event Store

/// Stores and persists agent events with filtering and search capabilities.
/// Events are automatically pruned when exceeding the maximum limit.
@MainActor
@Observable
final class AgentEventStore {
    static let shared = AgentEventStore()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "agent-events")
    private let maxEvents = 1000
    private let saveDebounceMs: UInt64 = 500

    private(set) var events: [StoredAgentEvent] = []
    private(set) var sessionSummaries: [AgentSessionSummary] = []
    private(set) var isLoading = false

    private var saveTask: Task<Void, Never>?
    private var isDirty = false

    private init() {
        loadFromDisk()
        updateSessionSummaries()
    }

    // MARK: - Public Methods

    /// Append a new event from the control channel
    func append(event: ControlAgentEvent) {
        let stored = StoredAgentEvent(from: event)
        events.append(stored)
        logger.debug("event appended runId=\(event.runId) stream=\(event.stream) seq=\(event.seq)")

        // Prune if over limit
        if events.count > maxEvents {
            let removeCount = events.count - maxEvents
            events.removeFirst(removeCount)
            logger.info("pruned \(removeCount) old events")
        }

        updateSessionSummaries()
        scheduleSave()
    }

    /// Get events for a specific session/run ID
    func events(for sessionId: String) -> [StoredAgentEvent] {
        events.filter { $0.runId == sessionId }
    }

    /// Get events with filters applied
    func filteredEvents(
        sessionId: String? = nil,
        streamType: AgentEventStreamType = .all,
        startDate: Date? = nil,
        endDate: Date? = nil,
        searchText: String? = nil
    ) -> [StoredAgentEvent] {
        events.filter { event in
            // Session filter
            if let sessionId, event.runId != sessionId {
                return false
            }

            // Stream type filter
            if streamType != .all && event.stream != streamType.rawValue {
                return false
            }

            // Time range filter
            if let startDate, event.timestamp < startDate {
                return false
            }
            if let endDate, event.timestamp > endDate {
                return false
            }

            // Search text filter
            if let searchText, !searchText.isEmpty {
                let lowercaseSearch = searchText.lowercased()
                let matchesSummary = event.summary?.lowercased().contains(lowercaseSearch) ?? false
                let matchesStream = event.stream.lowercased().contains(lowercaseSearch)
                let matchesRunId = event.runId.lowercased().contains(lowercaseSearch)
                let matchesData = searchInData(event.data, for: lowercaseSearch)

                if !matchesSummary && !matchesStream && !matchesRunId && !matchesData {
                    return false
                }
            }

            return true
        }
    }

    /// Search events by content
    func search(query: String) -> [StoredAgentEvent] {
        guard !query.isEmpty else { return events }
        return filteredEvents(searchText: query)
    }

    /// Clear events for a specific session
    func clear(sessionId: String) {
        let beforeCount = events.count
        events.removeAll { $0.runId == sessionId }
        let removedCount = beforeCount - events.count
        logger.info("cleared \(removedCount) events for session \(sessionId)")
        updateSessionSummaries()
        scheduleSave()
    }

    /// Clear all events
    func clearAll() {
        let count = events.count
        events.removeAll()
        sessionSummaries.removeAll()
        logger.info("cleared all \(count) events")
        scheduleSave()
    }

    /// Get unique session IDs
    var sessionIds: [String] {
        Array(Set(events.map(\.runId))).sorted()
    }

    /// Get event count for a session
    func eventCount(for sessionId: String) -> Int {
        events.filter { $0.runId == sessionId }.count
    }

    // MARK: - Private Methods

    private func searchInData(_ data: [String: AnyCodable], for query: String) -> Bool {
        for (key, value) in data {
            if key.lowercased().contains(query) {
                return true
            }
            if searchInValue(value.value, for: query) {
                return true
            }
        }
        return false
    }

    private func searchInValue(_ value: Any, for query: String) -> Bool {
        if let string = value as? String {
            return string.lowercased().contains(query)
        }
        if let dict = value as? [String: Any] {
            for (key, val) in dict {
                if key.lowercased().contains(query) || searchInValue(val, for: query) {
                    return true
                }
            }
        }
        if let array = value as? [Any] {
            for item in array {
                if searchInValue(item, for: query) {
                    return true
                }
            }
        }
        return false
    }

    private func updateSessionSummaries() {
        var summaryMap: [String: (events: [StoredAgentEvent], streams: Set<String>)] = [:]

        for event in events {
            var entry = summaryMap[event.runId] ?? (events: [], streams: [])
            entry.events.append(event)
            entry.streams.insert(event.stream)
            summaryMap[event.runId] = entry
        }

        sessionSummaries = summaryMap.map { runId, data in
            let sortedEvents = data.events.sorted { $0.timestamp < $1.timestamp }
            return AgentSessionSummary(
                id: runId,
                eventCount: data.events.count,
                firstEventAt: sortedEvents.first?.timestamp ?? Date(),
                lastEventAt: sortedEvents.last?.timestamp ?? Date(),
                streamTypes: data.streams
            )
        }.sorted { $0.lastEventAt > $1.lastEventAt }
    }

    // MARK: - Persistence

    private static func stateDirectory() -> URL {
        let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        return appSupport.appendingPathComponent("Nexus", isDirectory: true)
    }

    private static func fileURL() -> URL {
        stateDirectory().appendingPathComponent("agent-events.json")
    }

    private func loadFromDisk() {
        isLoading = true
        defer { isLoading = false }

        let url = Self.fileURL()
        guard FileManager.default.fileExists(atPath: url.path) else {
            logger.debug("no existing events file at \(url.path)")
            return
        }

        do {
            let data = try Data(contentsOf: url)
            let decoder = JSONDecoder()
            decoder.dateDecodingStrategy = .iso8601
            let loaded = try decoder.decode([StoredAgentEvent].self, from: data)
            events = loaded
            logger.info("loaded \(loaded.count) events from disk")
        } catch {
            logger.warning("failed to load events: \(error.localizedDescription, privacy: .public)")
        }
    }

    private func scheduleSave() {
        isDirty = true
        saveTask?.cancel()
        saveTask = Task { [weak self] in
            try? await Task.sleep(nanoseconds: (self?.saveDebounceMs ?? 500) * 1_000_000)
            guard !Task.isCancelled else { return }
            self?.saveToDisk()
        }
    }

    private func saveToDisk() {
        guard isDirty else { return }

        let url = Self.fileURL()
        do {
            try FileManager.default.createDirectory(
                at: url.deletingLastPathComponent(),
                withIntermediateDirectories: true
            )

            let encoder = JSONEncoder()
            encoder.dateEncodingStrategy = .iso8601
            encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
            let data = try encoder.encode(events)
            try data.write(to: url, options: [.atomic])
            try? FileManager.default.setAttributes(
                [.posixPermissions: 0o600],
                ofItemAtPath: url.path
            )
            isDirty = false
            logger.debug("saved \(self.events.count) events to disk")
        } catch {
            logger.error("failed to save events: \(error.localizedDescription, privacy: .public)")
        }
    }

    // MARK: - Testing Support

    #if DEBUG
    static func _testClear() async {
        await MainActor.run {
            shared.events.removeAll()
            shared.sessionSummaries.removeAll()
        }
    }

    static func _testAppend(_ event: ControlAgentEvent) async {
        await MainActor.run {
            shared.append(event: event)
        }
    }
    #endif
}
