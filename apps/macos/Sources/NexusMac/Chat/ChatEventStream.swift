import Foundation
import OSLog

// MARK: - Event Types

/// Event types from chat transport stream
enum ChatTransportEvent: Sendable {
    case health(ChatHealthState)
    case tick(Int64)
    case chat(ChatMessage)
    case agent(AgentEvent)
    case seqGap(expected: Int64, received: Int64)
    case error(Error)
}

/// Health state from gateway
struct ChatHealthState: Codable, Sendable {
    let state: HealthStateType
    let summary: String?
    let lastSuccess: Date?

    enum HealthStateType: String, Codable, Sendable {
        case ok
        case degraded
        case linkingNeeded
        case unknown
    }
}

/// Agent event from streaming
struct AgentEvent: Codable, Sendable {
    let eventType: String
    let stream: String?
    let data: [String: AnyCodable]?
    let toolCallId: String?
    let phase: String?

    enum CodingKeys: String, CodingKey {
        case eventType = "event_type"
        case stream, data
        case toolCallId = "tool_call_id"
        case phase
    }
}

/// Chat message from history or stream
struct ChatMessage: Codable, Sendable, Identifiable {
    let id: String
    let sessionId: String
    let role: MessageRole
    let content: String
    let timestamp: Date
    let metadata: ChatMessageMetadata?

    enum MessageRole: String, Codable, Sendable {
        case user
        case assistant
        case system
        case tool
    }
}

struct ChatMessageMetadata: Codable, Sendable {
    let model: String?
    let tokens: TokenUsage?
    let toolCalls: [ToolCallInfo]?
}

struct TokenUsage: Codable, Sendable {
    let input: Int
    let output: Int
}

struct ToolCallInfo: Codable, Sendable {
    let id: String
    let name: String
    let arguments: String?
    let result: String?
}

// MARK: - ChatEventStream

/// Manages chat event streaming from gateway
@MainActor
@Observable
final class ChatEventStream {
    static let shared = ChatEventStream()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "chat-events")

    // Stream state
    private(set) var isStreaming = false
    private(set) var lastSeq: Int64 = 0
    private(set) var healthState: ChatHealthState?

    // Pending runs
    private(set) var pendingRuns: Set<String> = []
    private var runStartTimes: [String: Date] = [:]
    private let runTimeout: TimeInterval = 120 // 120 seconds

    // Streaming text accumulator
    private(set) var streamingText: String = ""
    private(set) var isStreamingAssistant = false

    // Subscribers
    private var subscribers: [UUID: AsyncStream<ChatTransportEvent>.Continuation] = [:]

    // Event stream task
    private var eventTask: Task<Void, Never>?

    private init() {}

    // MARK: - Subscription

    func subscribe() -> AsyncStream<ChatTransportEvent> {
        let id = UUID()
        return AsyncStream { continuation in
            self.subscribers[id] = continuation
            continuation.onTermination = { _ in
                Task { @MainActor in
                    self.subscribers.removeValue(forKey: id)
                }
            }
        }
    }

    // MARK: - Event Processing

    func processEvent(_ frame: GatewayEventFrame) {
        // Sequence validation
        let seq = frame.seq
        if lastSeq > 0 && seq > lastSeq + 1 {
            emit(.seqGap(expected: lastSeq + 1, received: seq))
            logger.warning("Sequence gap detected: expected \(self.lastSeq + 1), got \(seq)")
        }
        lastSeq = seq

        // Route by event type
        switch frame.event {
        case "health":
            processHealthEvent(frame)
        case "tick":
            processTick(frame)
        case "chat":
            processChatEvent(frame)
        case "agent":
            processAgentEvent(frame)
        default:
            logger.debug("Unknown event type: \(frame.event)")
        }
    }

    private func processHealthEvent(_ frame: GatewayEventFrame) {
        guard let payload = frame.payload,
              let data = try? JSONSerialization.jsonObject(with: payload) as? [String: Any],
              let stateStr = data["state"] as? String,
              let state = ChatHealthState.HealthStateType(rawValue: stateStr) else {
            return
        }

        let lastSuccess: Date?
        if let lastSuccessStr = data["last_success"] as? String {
            lastSuccess = ISO8601DateFormatter().date(from: lastSuccessStr)
        } else {
            lastSuccess = nil
        }

        let health = ChatHealthState(
            state: state,
            summary: data["summary"] as? String,
            lastSuccess: lastSuccess
        )
        healthState = health
        emit(.health(health))
    }

    private func processTick(_ frame: GatewayEventFrame) {
        emit(.tick(frame.seq))
        checkPendingRunTimeouts()
    }

    private func processChatEvent(_ frame: GatewayEventFrame) {
        guard let payload = frame.payload,
              let data = try? JSONSerialization.jsonObject(with: payload) as? [String: Any],
              let id = data["id"] as? String,
              let sessionId = data["session_id"] as? String,
              let roleStr = data["role"] as? String,
              let role = ChatMessage.MessageRole(rawValue: roleStr),
              let content = data["content"] as? String else {
            return
        }

        let timestamp: Date
        if let tsStr = data["timestamp"] as? String {
            timestamp = ISO8601DateFormatter().date(from: tsStr) ?? Date()
        } else {
            timestamp = Date()
        }

        let message = ChatMessage(
            id: id,
            sessionId: sessionId,
            role: role,
            content: content,
            timestamp: timestamp,
            metadata: nil
        )
        emit(.chat(message))
    }

    private func processAgentEvent(_ frame: GatewayEventFrame) {
        guard let payload = frame.payload,
              let data = try? JSONSerialization.jsonObject(with: payload) as? [String: Any] else {
            return
        }

        let eventData: [String: AnyCodable]?
        if let rawData = data["data"] as? [String: Any] {
            eventData = rawData.mapValues { AnyCodable($0) }
        } else {
            eventData = nil
        }

        let event = AgentEvent(
            eventType: data["event_type"] as? String ?? "",
            stream: data["stream"] as? String,
            data: eventData,
            toolCallId: data["tool_call_id"] as? String,
            phase: data["phase"] as? String
        )

        // Handle streaming assistant text
        if event.stream == "assistant",
           let dataDict = event.data,
           let textValue = dataDict["text"]?.value as? String {
            streamingText += textValue
            isStreamingAssistant = true
        }

        // Track run state
        if let runId = data["run_id"] as? String {
            if event.eventType == "run_start" {
                pendingRuns.insert(runId)
                runStartTimes[runId] = Date()
            } else if event.eventType == "run_end" {
                pendingRuns.remove(runId)
                runStartTimes.removeValue(forKey: runId)
                streamingText = ""
                isStreamingAssistant = false
            }
        }

        emit(.agent(event))
    }

    private func checkPendingRunTimeouts() {
        let now = Date()
        for (runId, startTime) in runStartTimes {
            if now.timeIntervalSince(startTime) > runTimeout {
                logger.warning("Run \(runId) timed out after \(self.runTimeout)s")
                pendingRuns.remove(runId)
                runStartTimes.removeValue(forKey: runId)
            }
        }
    }

    // MARK: - Control

    func startStreaming() {
        guard !isStreaming else { return }
        isStreaming = true
        logger.info("Chat event streaming started")

        eventTask?.cancel()
        eventTask = Task { [weak self] in
            let stream = await GatewayConnection.shared.subscribe()
            for await push in stream {
                guard !Task.isCancelled else { break }
                await MainActor.run {
                    if case .event(let frame) = push {
                        self?.processEvent(frame)
                    }
                }
            }
        }
    }

    func stopStreaming() {
        eventTask?.cancel()
        eventTask = nil
        isStreaming = false
        lastSeq = 0
        pendingRuns.removeAll()
        runStartTimes.removeAll()
        streamingText = ""
        isStreamingAssistant = false
        logger.info("Chat event streaming stopped")
    }

    func clearStreamingText() {
        streamingText = ""
        isStreamingAssistant = false
    }

    // MARK: - Emission

    private func emit(_ event: ChatTransportEvent) {
        for continuation in subscribers.values {
            continuation.yield(event)
        }
    }
}

// MARK: - Notification Names

extension Notification.Name {
    static let chatEventStreamMessage = Notification.Name("nexus.chat.message")
    static let chatEventStreamAgent = Notification.Name("nexus.chat.agent")
}
