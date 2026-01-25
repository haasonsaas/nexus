import Foundation
import OSLog

// MARK: - Chat Transport Protocol

/// Protocol for chat transport implementations.
/// Provides an abstraction layer for different chat backend implementations.
protocol ChatTransportProtocol: AnyObject, Sendable {
    /// Connect to the transport
    func connect() async throws

    /// Disconnect from the transport
    func disconnect() async

    /// Send a message
    /// - Parameters:
    ///   - sessionId: The session identifier
    ///   - content: The message content
    ///   - attachments: Optional attachments
    /// - Returns: The message ID assigned by the server
    func send(sessionId: String, content: String, attachments: [ChatAttachment]?) async throws -> String

    /// Abort current generation
    /// - Parameter sessionId: The session identifier
    func abort(sessionId: String) async throws

    /// Load chat history
    /// - Parameters:
    ///   - sessionId: The session identifier
    ///   - limit: Optional limit on number of messages
    /// - Returns: Array of chat messages
    func loadHistory(sessionId: String, limit: Int?) async throws -> [ChatMessage]

    /// Subscribe to transport events
    /// - Returns: An async stream of transport events
    func subscribe() -> AsyncStream<ChatTransportEvent>

    /// Current connection state
    var isConnected: Bool { get }
}

// MARK: - Chat Attachment

/// Attachment for chat messages
struct ChatAttachment: Codable, Sendable, Identifiable, Hashable {
    let id: String
    let type: AttachmentType
    let name: String
    let data: Data
    let mimeType: String?

    enum AttachmentType: String, Codable, Sendable {
        case image
        case file
        case audio
        case video
    }

    init(
        id: String = UUID().uuidString,
        type: AttachmentType,
        name: String,
        data: Data,
        mimeType: String? = nil
    ) {
        self.id = id
        self.type = type
        self.name = name
        self.data = data
        self.mimeType = mimeType
    }
}

// MARK: - Transport Errors

enum ChatTransportError: Error, LocalizedError, Sendable {
    case notConnected
    case invalidResponse
    case sendFailed(String)
    case abortFailed(String)
    case historyLoadFailed(String)
    case connectionFailed(String)

    var errorDescription: String? {
        switch self {
        case .notConnected:
            return "Chat transport is not connected"
        case .invalidResponse:
            return "Invalid response from server"
        case .sendFailed(let reason):
            return "Failed to send message: \(reason)"
        case .abortFailed(let reason):
            return "Failed to abort generation: \(reason)"
        case .historyLoadFailed(let reason):
            return "Failed to load history: \(reason)"
        case .connectionFailed(let reason):
            return "Connection failed: \(reason)"
        }
    }
}

// MARK: - Gateway Chat Transport

/// Gateway-based chat transport implementation.
/// Uses the control channel and event stream for communication.
@MainActor
final class GatewayChatTransport: ChatTransportProtocol, @unchecked Sendable {
    private let logger = Logger(subsystem: "com.nexus.mac", category: "chat-transport")

    private var controlChannel: ControlChannel { ControlChannel.shared }
    private var eventContinuations: [UUID: AsyncStream<ChatTransportEvent>.Continuation] = [:]
    private var eventTask: Task<Void, Never>?

    var isConnected: Bool {
        controlChannel.state == .connected
    }

    // MARK: - Initialization

    init() {
        startEventForwarding()
    }

    deinit {
        eventTask?.cancel()
    }

    // MARK: - Connection

    func connect() async throws {
        logger.info("Connecting chat transport")

        do {
            await controlChannel.configure()

            if controlChannel.state != .connected {
                if case .degraded(let msg) = controlChannel.state {
                    throw ChatTransportError.connectionFailed(msg)
                }
                throw ChatTransportError.connectionFailed("Unknown connection error")
            }

            logger.info("Chat transport connected")
        } catch let error as ChatTransportError {
            throw error
        } catch {
            throw ChatTransportError.connectionFailed(error.localizedDescription)
        }
    }

    func disconnect() async {
        logger.info("Disconnecting chat transport")
        await controlChannel.disconnect()

        // Terminate all event streams
        for (id, continuation) in eventContinuations {
            continuation.finish()
            eventContinuations.removeValue(forKey: id)
        }
    }

    // MARK: - Messaging

    func send(sessionId: String, content: String, attachments: [ChatAttachment]?) async throws -> String {
        guard isConnected else {
            throw ChatTransportError.notConnected
        }

        var params: [String: AnyHashable] = [
            "session_id": sessionId,
            "content": content
        ]

        if let attachments = attachments, !attachments.isEmpty {
            let attachmentData = attachments.map { att -> [String: AnyHashable] in
                var dict: [String: AnyHashable] = [
                    "id": att.id,
                    "type": att.type.rawValue,
                    "name": att.name,
                    "data": att.data.base64EncodedString()
                ]
                if let mimeType = att.mimeType {
                    dict["mime_type"] = mimeType
                }
                return dict
            }
            params["attachments"] = attachmentData
        }

        logger.info("Sending message to session \(sessionId)")

        do {
            let data = try await controlChannel.request(method: "chat.send", params: params)

            struct Response: Decodable {
                let messageId: String?
                let message_id: String?

                var id: String? { messageId ?? message_id }
            }

            let response = try JSONDecoder().decode(Response.self, from: data)

            guard let messageId = response.id else {
                throw ChatTransportError.invalidResponse
            }

            logger.debug("Message sent successfully id=\(messageId)")
            return messageId
        } catch let error as ChatTransportError {
            throw error
        } catch {
            throw ChatTransportError.sendFailed(error.localizedDescription)
        }
    }

    func abort(sessionId: String) async throws {
        guard isConnected else {
            throw ChatTransportError.notConnected
        }

        logger.info("Aborting generation for session \(sessionId)")

        do {
            _ = try await controlChannel.request(
                method: "chat.abort",
                params: ["session_id": sessionId]
            )
            logger.debug("Abort request sent for session \(sessionId)")
        } catch {
            throw ChatTransportError.abortFailed(error.localizedDescription)
        }
    }

    // MARK: - History

    func loadHistory(sessionId: String, limit: Int? = nil) async throws -> [ChatMessage] {
        guard isConnected else {
            throw ChatTransportError.notConnected
        }

        var params: [String: AnyHashable] = ["session_id": sessionId]
        if let limit = limit {
            params["limit"] = limit
        }

        logger.debug("Loading history for session \(sessionId)")

        do {
            let data = try await controlChannel.request(method: "chat.history", params: params)

            guard let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let messagesData = json["messages"] as? [[String: Any]] else {
                return []
            }

            let messages = messagesData.compactMap { dict -> ChatMessage? in
                parseMessage(from: dict, sessionId: sessionId)
            }

            logger.debug("Loaded \(messages.count) messages for session \(sessionId)")
            return messages
        } catch let error as ChatTransportError {
            throw error
        } catch {
            throw ChatTransportError.historyLoadFailed(error.localizedDescription)
        }
    }

    private func parseMessage(from dict: [String: Any], sessionId: String) -> ChatMessage? {
        guard let id = dict["id"] as? String,
              let roleStr = dict["role"] as? String,
              let role = ChatMessage.MessageRole(rawValue: roleStr),
              let content = dict["content"] as? String else {
            return nil
        }

        let timestamp: Date
        if let ts = dict["timestamp"] as? String {
            timestamp = ISO8601DateFormatter().date(from: ts) ?? Date()
        } else if let ts = dict["created_at"] as? String {
            timestamp = ISO8601DateFormatter().date(from: ts) ?? Date()
        } else {
            timestamp = Date()
        }

        // Parse metadata if present
        var metadata: ChatMessageMetadata?
        if let metaDict = dict["metadata"] as? [String: Any] {
            let model = metaDict["model"] as? String

            var tokens: TokenUsage?
            if let tokensDict = metaDict["tokens"] as? [String: Any],
               let input = tokensDict["input"] as? Int,
               let output = tokensDict["output"] as? Int {
                tokens = TokenUsage(input: input, output: output)
            }

            metadata = ChatMessageMetadata(model: model, tokens: tokens, toolCalls: nil)
        }

        return ChatMessage(
            id: id,
            sessionId: sessionId,
            role: role,
            content: content,
            timestamp: timestamp,
            metadata: metadata
        )
    }

    // MARK: - Events

    func subscribe() -> AsyncStream<ChatTransportEvent> {
        let id = UUID()

        return AsyncStream { continuation in
            self.eventContinuations[id] = continuation

            continuation.onTermination = { [weak self] _ in
                Task { @MainActor in
                    self?.eventContinuations.removeValue(forKey: id)
                }
            }
        }
    }

    private func startEventForwarding() {
        eventTask = Task { [weak self] in
            let stream = await GatewayConnection.shared.subscribe()

            for await push in stream {
                guard !Task.isCancelled else { break }

                await MainActor.run {
                    self?.handlePush(push)
                }
            }
        }
    }

    private func handlePush(_ push: GatewayPush) {
        switch push {
        case .event(let frame):
            handleEventFrame(frame)

        case .snapshot:
            let health = ChatHealthState(state: .ok, summary: nil, lastSuccess: Date())
            broadcast(.health(health))
        }
    }

    private func handleEventFrame(_ frame: GatewayEventFrame) {
        switch frame.event {
        case "chat", "chat.message":
            if let payload = frame.payload,
               let message = parseChatMessageFromPayload(payload) {
                broadcast(.chat(message))
            }

        case "agent":
            if let payload = frame.payload,
               let agentEvent = parseAgentEvent(from: payload) {
                broadcast(.agent(agentEvent))
            }

        case "health":
            if let payload = frame.payload {
                let health = parseHealthState(from: payload)
                broadcast(.health(health))
            }

        case "tick":
            broadcast(.tick(frame.seq))

        default:
            logger.debug("Unhandled event: \(frame.event)")
        }
    }

    private func parseChatMessageFromPayload(_ data: Data) -> ChatMessage? {
        guard let dict = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let id = dict["id"] as? String,
              let sessionId = dict["session_id"] as? String,
              let roleStr = dict["role"] as? String,
              let role = ChatMessage.MessageRole(rawValue: roleStr),
              let content = dict["content"] as? String else {
            return nil
        }

        let timestamp: Date
        if let ts = dict["timestamp"] as? String {
            timestamp = ISO8601DateFormatter().date(from: ts) ?? Date()
        } else {
            timestamp = Date()
        }

        return ChatMessage(
            id: id,
            sessionId: sessionId,
            role: role,
            content: content,
            timestamp: timestamp,
            metadata: nil
        )
    }

    private func parseAgentEvent(from data: Data) -> AgentEvent? {
        guard let dict = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            return nil
        }

        let eventData: [String: AnyCodable]?
        if let rawData = dict["data"] as? [String: Any] {
            eventData = rawData.mapValues { AnyCodable($0) }
        } else {
            eventData = nil
        }

        return AgentEvent(
            eventType: dict["event_type"] as? String ?? "",
            stream: dict["stream"] as? String,
            data: eventData,
            toolCallId: dict["tool_call_id"] as? String,
            phase: dict["phase"] as? String
        )
    }

    private func parseHealthState(from data: Data) -> ChatHealthState {
        guard let dict = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let stateStr = dict["state"] as? String,
              let state = ChatHealthState.HealthStateType(rawValue: stateStr) else {
            return ChatHealthState(state: .unknown, summary: nil, lastSuccess: nil)
        }

        let lastSuccess: Date?
        if let lastSuccessStr = dict["last_success"] as? String {
            lastSuccess = ISO8601DateFormatter().date(from: lastSuccessStr)
        } else {
            lastSuccess = nil
        }

        return ChatHealthState(
            state: state,
            summary: dict["summary"] as? String,
            lastSuccess: lastSuccess
        )
    }

    private func broadcast(_ event: ChatTransportEvent) {
        for (_, continuation) in eventContinuations {
            continuation.yield(event)
        }
    }
}

// MARK: - Tool Call Tracker

/// Tracks tool calls during agent execution.
@MainActor
@Observable
final class ToolCallTracker {
    static let shared = ToolCallTracker()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "tool-tracker")

    private(set) var activeToolCalls: [ToolCall] = []
    private(set) var completedToolCalls: [ToolCall] = []

    struct ToolCall: Identifiable, Sendable {
        let id: String
        let runId: String
        let name: String
        let input: [String: Any]?
        var status: Status
        var output: String?
        var error: String?
        let startedAt: Date
        var completedAt: Date?

        enum Status: String, Sendable {
            case pending
            case running
            case completed
            case failed
        }

        var duration: TimeInterval? {
            guard let completedAt else { return nil }
            return completedAt.timeIntervalSince(startedAt)
        }
    }

    private init() {}

    // MARK: - Event Processing

    func processAgentEvent(_ event: AgentEvent) {
        guard let stream = event.stream else { return }

        switch stream {
        case "tool_use":
            handleToolUse(event)

        case "tool_result":
            handleToolResult(event)

        default:
            break
        }
    }

    func processControlAgentEvent(_ event: ControlAgentEvent) {
        switch event.stream {
        case "tool_use":
            handleControlToolUse(event)

        case "tool_result":
            handleControlToolResult(event)

        default:
            break
        }
    }

    private func handleToolUse(_ event: AgentEvent) {
        guard let data = event.data else { return }

        let toolName = data["name"]?.value as? String ?? "unknown"
        let toolId = data["id"]?.value as? String ?? event.toolCallId ?? UUID().uuidString
        let runId = data["run_id"]?.value as? String ?? ""

        var input: [String: Any]?
        if let inputData = data["input"]?.value as? [String: Any] {
            input = inputData
        }

        let call = ToolCall(
            id: toolId,
            runId: runId,
            name: toolName,
            input: input,
            status: .running,
            startedAt: Date()
        )

        activeToolCalls.append(call)
        logger.debug("Tool call started: \(toolName) id=\(toolId)")
    }

    private func handleToolResult(_ event: AgentEvent) {
        guard let data = event.data else { return }

        let toolId = data["tool_use_id"]?.value as? String ?? event.toolCallId ?? data["id"]?.value as? String

        guard let id = toolId,
              let index = activeToolCalls.firstIndex(where: { $0.id == id }) else {
            return
        }

        var call = activeToolCalls[index]
        call.completedAt = Date()

        if let error = data["error"]?.value as? String, !error.isEmpty {
            call.status = .failed
            call.error = error
        } else {
            call.status = .completed
            if let output = data["content"]?.value as? String {
                call.output = output
            }
        }

        activeToolCalls.remove(at: index)
        completedToolCalls.append(call)

        logger.debug("Tool call completed: \(call.name) status=\(call.status.rawValue)")
    }

    private func handleControlToolUse(_ event: ControlAgentEvent) {
        let toolName = event.data["name"]?.value as? String ?? "unknown"
        let toolId = event.data["id"]?.value as? String ?? UUID().uuidString

        var input: [String: Any]?
        if let inputData = event.data["input"]?.value as? [String: Any] {
            input = inputData
        }

        let call = ToolCall(
            id: toolId,
            runId: event.runId,
            name: toolName,
            input: input,
            status: .running,
            startedAt: Date()
        )

        activeToolCalls.append(call)
        logger.debug("Tool call started: \(toolName) id=\(toolId)")
    }

    private func handleControlToolResult(_ event: ControlAgentEvent) {
        let toolId = event.data["tool_use_id"]?.value as? String ?? event.data["id"]?.value as? String

        guard let id = toolId,
              let index = activeToolCalls.firstIndex(where: { $0.id == id }) else {
            return
        }

        var call = activeToolCalls[index]
        call.completedAt = Date()

        if let error = event.data["error"]?.value as? String, !error.isEmpty {
            call.status = .failed
            call.error = error
        } else {
            call.status = .completed
            if let output = event.data["content"]?.value as? String {
                call.output = output
            }
        }

        activeToolCalls.remove(at: index)
        completedToolCalls.append(call)

        logger.debug("Tool call completed: \(call.name) status=\(call.status.rawValue)")
    }

    // MARK: - Management

    func clearCompleted() {
        completedToolCalls.removeAll()
    }

    func clearAll() {
        activeToolCalls.removeAll()
        completedToolCalls.removeAll()
    }

    func getToolCalls(for runId: String) -> [ToolCall] {
        (activeToolCalls + completedToolCalls).filter { $0.runId == runId }
    }
}
