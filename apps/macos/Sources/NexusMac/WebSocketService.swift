import Foundation

// MARK: - WebSocket Frame Types

/// Frame sent to the server
struct WSRequestFrame: Encodable {
    let type: String
    let id: String
    let method: String
    let params: [String: Any]?

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(type, forKey: .type)
        try container.encode(id, forKey: .id)
        try container.encode(method, forKey: .method)
        if let params = params {
            let data = try JSONSerialization.data(withJSONObject: params)
            let raw = try JSONDecoder().decode(AnyCodable.self, from: data)
            try container.encode(raw, forKey: .params)
        }
    }

    private enum CodingKeys: String, CodingKey {
        case type, id, method, params
    }
}

/// Frame received from the server
struct WSResponseFrame: Decodable {
    let type: String
    let id: String?
    let ok: Bool?
    let payload: AnyCodable?
    let error: WSError?
    let event: String?
    let seq: Int64?
}

struct WSError: Decodable {
    let code: String
    let message: String
}

/// Helper for encoding/decoding arbitrary JSON
struct AnyCodable: Codable {
    let value: Any

    init(_ value: Any) {
        self.value = value
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() {
            value = NSNull()
        } else if let bool = try? container.decode(Bool.self) {
            value = bool
        } else if let int = try? container.decode(Int.self) {
            value = int
        } else if let int64 = try? container.decode(Int64.self) {
            value = int64
        } else if let double = try? container.decode(Double.self) {
            value = double
        } else if let string = try? container.decode(String.self) {
            value = string
        } else if let array = try? container.decode([AnyCodable].self) {
            value = array.map { $0.value }
        } else if let dict = try? container.decode([String: AnyCodable].self) {
            value = dict.mapValues { $0.value }
        } else {
            throw DecodingError.dataCorruptedError(in: container, debugDescription: "Cannot decode value")
        }
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch value {
        case is NSNull:
            try container.encodeNil()
        case let bool as Bool:
            try container.encode(bool)
        case let int as Int:
            try container.encode(int)
        case let int64 as Int64:
            try container.encode(int64)
        case let double as Double:
            try container.encode(double)
        case let string as String:
            try container.encode(string)
        case let array as [Any]:
            try container.encode(array.map { AnyCodable($0) })
        case let dict as [String: Any]:
            try container.encode(dict.mapValues { AnyCodable($0) })
        default:
            throw EncodingError.invalidValue(value, EncodingError.Context(codingPath: encoder.codingPath, debugDescription: "Cannot encode value"))
        }
    }
}

// MARK: - Server Event Types

/// Events received from the WebSocket server
enum ServerEvent: Equatable {
    case connected(HelloPayload)
    case tick(timestamp: Int64)
    case healthUpdate(HealthSnapshot)
    case chatChunk(ChatChunkEvent)
    case chatComplete(ChatCompleteEvent)
    case toolCall(ToolCallEvent)
    case sessionEvent(SessionEventPayload)
    case error(ErrorEvent)
    case pong(timestamp: Int64)
    case disconnected(reason: String?)

    static func == (lhs: ServerEvent, rhs: ServerEvent) -> Bool {
        switch (lhs, rhs) {
        case (.connected(let a), .connected(let b)):
            return a.serverId == b.serverId
        case (.tick(let a), .tick(let b)):
            return a == b
        case (.disconnected(let a), .disconnected(let b)):
            return a == b
        case (.pong(let a), .pong(let b)):
            return a == b
        case (.error(let a), .error(let b)):
            return a.code == b.code && a.message == b.message
        default:
            return false
        }
    }
}

struct HelloPayload {
    let serverId: String
    let protocol_: Int
    let canvasHostUrl: String?
    let methods: [String]
    let events: [String]
    let maxPayloadBytes: Int
    let tickIntervalMs: Int64
    let healthSnapshot: HealthSnapshot?
}

struct HealthSnapshot: Equatable {
    let uptimeMs: Int64
    let status: String
    let channels: [ChannelHealthStatus]

    static func == (lhs: HealthSnapshot, rhs: HealthSnapshot) -> Bool {
        return lhs.uptimeMs == rhs.uptimeMs && lhs.status == rhs.status
    }
}

struct ChannelHealthStatus: Equatable {
    let channel: String
    let connected: Bool
    let error: String?
    let lastPing: Int64?
}

struct ChatChunkEvent: Equatable {
    let requestId: String
    let messageId: String
    let sessionId: String
    let content: String
    let sequence: Int
    let type: String

    static func == (lhs: ChatChunkEvent, rhs: ChatChunkEvent) -> Bool {
        return lhs.requestId == rhs.requestId && lhs.sequence == rhs.sequence
    }
}

struct ChatCompleteEvent: Equatable {
    let requestId: String
    let messageId: String
    let sessionId: String
    let message: [String: Any]?

    static func == (lhs: ChatCompleteEvent, rhs: ChatCompleteEvent) -> Bool {
        return lhs.requestId == rhs.requestId && lhs.messageId == rhs.messageId
    }
}

struct ToolCallEvent: Equatable {
    let toolCallId: String?
    let toolName: String?
    let input: [String: Any]?

    static func == (lhs: ToolCallEvent, rhs: ToolCallEvent) -> Bool {
        return lhs.toolCallId == rhs.toolCallId && lhs.toolName == rhs.toolName
    }
}

struct SessionEventPayload: Equatable {
    let sessionId: String
    let eventType: String
    let data: [String: Any]?

    static func == (lhs: SessionEventPayload, rhs: SessionEventPayload) -> Bool {
        return lhs.sessionId == rhs.sessionId && lhs.eventType == rhs.eventType
    }
}

struct ErrorEvent: Equatable {
    let requestId: String?
    let code: String
    let message: String
}

// MARK: - WebSocket Service

@MainActor
class WebSocketService: ObservableObject {
    @Published var isConnected = false
    @Published var lastEvent: ServerEvent?
    @Published var connectionError: String?
    @Published var healthSnapshot: HealthSnapshot?
    @Published var activeToolCalls: [ToolCallEvent] = []
    @Published var recentSessionEvents: [SessionEventPayload] = []

    private var webSocket: URLSessionWebSocketTask?
    private var session: URLSession?
    private let baseURL: String
    private let apiKey: String
    private var requestCounter: Int = 0
    private var reconnectAttempts: Int = 0
    private let maxReconnectAttempts = 5
    private let reconnectBaseDelay: TimeInterval = 1.0
    private var isReconnecting = false
    private var shouldStayConnected = false

    /// Event handler for external consumers
    var onEvent: ((ServerEvent) -> Void)?

    init(baseURL: String, apiKey: String) {
        self.baseURL = baseURL
        self.apiKey = apiKey
    }

    func connect() {
        guard !isConnected && !isReconnecting else { return }
        shouldStayConnected = true
        reconnectAttempts = 0
        performConnect()
    }

    func disconnect() {
        shouldStayConnected = false
        webSocket?.cancel(with: .goingAway, reason: nil)
        webSocket = nil
        isConnected = false
        lastEvent = .disconnected(reason: "User disconnected")
    }

    private func performConnect() {
        connectionError = nil

        // Build WebSocket URL
        var urlString = baseURL.trimmingCharacters(in: .whitespacesAndNewlines)
        if urlString.hasPrefix("http://") {
            urlString = "ws://" + urlString.dropFirst(7)
        } else if urlString.hasPrefix("https://") {
            urlString = "wss://" + urlString.dropFirst(8)
        } else if !urlString.hasPrefix("ws://") && !urlString.hasPrefix("wss://") {
            urlString = "ws://" + urlString
        }

        if !urlString.hasSuffix("/ws") {
            urlString = urlString.hasSuffix("/") ? urlString + "ws" : urlString + "/ws"
        }

        guard let url = URL(string: urlString) else {
            connectionError = "Invalid WebSocket URL"
            return
        }

        var request = URLRequest(url: url)
        request.setValue(apiKey, forHTTPHeaderField: "X-API-Key")
        request.timeoutInterval = 30

        let configuration = URLSessionConfiguration.default
        session = URLSession(configuration: configuration)
        webSocket = session?.webSocketTask(with: request)
        webSocket?.resume()

        // Send connect handshake
        sendConnectHandshake()

        // Start receiving messages
        receiveMessage()
    }

    private func sendConnectHandshake() {
        let connectParams: [String: Any] = [
            "minProtocol": 1,
            "maxProtocol": 1,
            "client": [
                "id": UUID().uuidString,
                "version": Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "1.0",
                "platform": "macos",
                "mode": "desktop"
            ],
            "auth": [
                "token": apiKey
            ]
        ]

        sendRequest(method: "connect", params: connectParams) { [weak self] result in
            Task { @MainActor in
                switch result {
                case .success(let response):
                    self?.handleConnectResponse(response)
                case .failure(let error):
                    self?.connectionError = error.localizedDescription
                    self?.scheduleReconnect()
                }
            }
        }
    }

    private func handleConnectResponse(_ response: WSResponseFrame) {
        guard let ok = response.ok, ok else {
            connectionError = response.error?.message ?? "Connection rejected"
            scheduleReconnect()
            return
        }

        isConnected = true
        reconnectAttempts = 0

        // Parse hello payload
        if let payload = response.payload?.value as? [String: Any] {
            let serverInfo = payload["server"] as? [String: Any]
            let features = payload["features"] as? [String: Any]
            let policy = payload["policy"] as? [String: Any]
            let snapshot = payload["snapshot"] as? [String: Any]

            let hello = HelloPayload(
                serverId: serverInfo?["id"] as? String ?? "",
                protocol_: payload["protocol"] as? Int ?? 1,
                canvasHostUrl: serverInfo?["canvasHostUrl"] as? String,
                methods: features?["methods"] as? [String] ?? [],
                events: features?["events"] as? [String] ?? [],
                maxPayloadBytes: policy?["maxPayloadBytes"] as? Int ?? 1048576,
                tickIntervalMs: policy?["tickIntervalMs"] as? Int64 ?? 15000,
                healthSnapshot: parseHealthSnapshot(snapshot)
            )

            if let snapshot = hello.healthSnapshot {
                healthSnapshot = snapshot
            }

            lastEvent = .connected(hello)
            onEvent?(.connected(hello))
        }
    }

    private func parseHealthSnapshot(_ data: [String: Any]?) -> HealthSnapshot? {
        guard let data = data else { return nil }
        let healthInfo = data["health"] as? [String: Any]
        let channelsData = data["channels"] as? [[String: Any]] ?? []

        let channels = channelsData.map { ch in
            ChannelHealthStatus(
                channel: ch["channel"] as? String ?? "",
                connected: ch["connected"] as? Bool ?? false,
                error: ch["error"] as? String,
                lastPing: ch["lastPing"] as? Int64
            )
        }

        return HealthSnapshot(
            uptimeMs: data["uptimeMs"] as? Int64 ?? 0,
            status: healthInfo?["status"] as? String ?? "unknown",
            channels: channels
        )
    }

    private func receiveMessage() {
        webSocket?.receive { [weak self] result in
            Task { @MainActor in
                switch result {
                case .success(let message):
                    self?.handleMessage(message)
                    self?.receiveMessage()
                case .failure(let error):
                    self?.handleDisconnect(error: error)
                }
            }
        }
    }

    private func handleMessage(_ message: URLSessionWebSocketTask.Message) {
        switch message {
        case .string(let text):
            parseFrame(text)
        case .data(let data):
            if let text = String(data: data, encoding: .utf8) {
                parseFrame(text)
            }
        @unknown default:
            break
        }
    }

    private func parseFrame(_ text: String) {
        guard let data = text.data(using: .utf8) else { return }

        do {
            let frame = try JSONDecoder().decode(WSResponseFrame.self, from: data)
            handleFrame(frame)
        } catch {
            // Silent parse error for malformed frames
        }
    }

    private func handleFrame(_ frame: WSResponseFrame) {
        if frame.type == "event" {
            handleEvent(frame)
        }
        // Responses are handled via completion handlers in sendRequest
    }

    private func handleEvent(_ frame: WSResponseFrame) {
        guard let event = frame.event else { return }
        let payload = frame.payload?.value as? [String: Any]

        switch event {
        case "tick":
            let timestamp = payload?["timestamp"] as? Int64 ?? 0
            lastEvent = .tick(timestamp: timestamp)
            onEvent?(.tick(timestamp: timestamp))

        case "chat.chunk":
            let chunk = ChatChunkEvent(
                requestId: payload?["requestId"] as? String ?? "",
                messageId: payload?["messageId"] as? String ?? "",
                sessionId: payload?["sessionId"] as? String ?? "",
                content: payload?["content"] as? String ?? "",
                sequence: payload?["sequence"] as? Int ?? 0,
                type: payload?["type"] as? String ?? ""
            )
            lastEvent = .chatChunk(chunk)
            onEvent?(.chatChunk(chunk))

        case "chat.complete":
            let complete = ChatCompleteEvent(
                requestId: payload?["requestId"] as? String ?? "",
                messageId: payload?["messageId"] as? String ?? "",
                sessionId: payload?["sessionId"] as? String ?? "",
                message: payload?["message"] as? [String: Any]
            )
            lastEvent = .chatComplete(complete)
            onEvent?(.chatComplete(complete))

        case "tool.call":
            let toolCall = ToolCallEvent(
                toolCallId: payload?["tool_call_id"] as? String,
                toolName: payload?["tool_name"] as? String,
                input: payload?["input"] as? [String: Any]
            )
            activeToolCalls.append(toolCall)
            // Keep only recent tool calls
            if activeToolCalls.count > 10 {
                activeToolCalls.removeFirst()
            }
            lastEvent = .toolCall(toolCall)
            onEvent?(.toolCall(toolCall))

        case "session.event":
            let sessionEvent = SessionEventPayload(
                sessionId: payload?["session_id"] as? String ?? "",
                eventType: payload?["event_type"] as? String ?? "",
                data: payload?["data"] as? [String: Any]
            )
            recentSessionEvents.append(sessionEvent)
            // Keep only recent events
            if recentSessionEvents.count > 20 {
                recentSessionEvents.removeFirst()
            }
            lastEvent = .sessionEvent(sessionEvent)
            onEvent?(.sessionEvent(sessionEvent))

        case "error":
            let errorEvent = ErrorEvent(
                requestId: payload?["requestId"] as? String,
                code: payload?["code"] as? String ?? "unknown",
                message: payload?["message"] as? String ?? "Unknown error"
            )
            lastEvent = .error(errorEvent)
            onEvent?(.error(errorEvent))

        case "pong":
            let timestamp = payload?["timestamp"] as? Int64 ?? 0
            lastEvent = .pong(timestamp: timestamp)
            onEvent?(.pong(timestamp: timestamp))

        default:
            break
        }
    }

    private func handleDisconnect(error: Error) {
        isConnected = false
        connectionError = error.localizedDescription
        lastEvent = .disconnected(reason: error.localizedDescription)
        onEvent?(.disconnected(reason: error.localizedDescription))

        if shouldStayConnected {
            scheduleReconnect()
        }
    }

    private func scheduleReconnect() {
        guard shouldStayConnected && reconnectAttempts < maxReconnectAttempts else {
            isReconnecting = false
            return
        }

        isReconnecting = true
        reconnectAttempts += 1

        let delay = reconnectBaseDelay * pow(2.0, Double(reconnectAttempts - 1))
        let cappedDelay = min(delay, 30.0)

        Task {
            try? await Task.sleep(nanoseconds: UInt64(cappedDelay * 1_000_000_000))
            await MainActor.run {
                self.isReconnecting = false
                if self.shouldStayConnected && !self.isConnected {
                    self.performConnect()
                }
            }
        }
    }

    // MARK: - Request Methods

    private func nextRequestId() -> String {
        requestCounter += 1
        return "req-\(requestCounter)"
    }

    func sendRequest(method: String, params: [String: Any]? = nil, completion: @escaping (Result<WSResponseFrame, Error>) -> Void) {
        let requestId = nextRequestId()
        let frame = WSRequestFrame(type: "req", id: requestId, method: method, params: params)

        do {
            let data = try JSONEncoder().encode(frame)
            guard let text = String(data: data, encoding: .utf8) else {
                completion(.failure(NSError(domain: "WebSocket", code: 1, userInfo: [NSLocalizedDescriptionKey: "Failed to encode request"])))
                return
            }

            webSocket?.send(.string(text)) { error in
                if let error = error {
                    completion(.failure(error))
                }
                // Note: Response will come via receiveMessage
                // For simplicity, we complete immediately for requests that don't need responses
            }
        } catch {
            completion(.failure(error))
        }
    }

    /// Send a ping request
    func sendPing() {
        sendRequest(method: "ping") { _ in }
    }

    /// Request health snapshot
    func requestHealth() {
        sendRequest(method: "health") { [weak self] result in
            Task { @MainActor in
                if case .success(let response) = result,
                   let ok = response.ok, ok,
                   let payload = response.payload?.value as? [String: Any] {
                    self?.healthSnapshot = self?.parseHealthSnapshot(payload)
                }
            }
        }
    }

    /// Send a chat message
    func sendChat(sessionId: String?, content: String, metadata: [String: String]? = nil) {
        var params: [String: Any] = ["content": content]
        if let sessionId = sessionId {
            params["sessionId"] = sessionId
        }
        if let metadata = metadata {
            params["metadata"] = metadata
        }
        params["idempotencyKey"] = UUID().uuidString

        sendRequest(method: "chat.send", params: params) { _ in }
    }

    /// Fetch chat history
    func fetchChatHistory(sessionId: String, limit: Int = 50) {
        let params: [String: Any] = [
            "sessionId": sessionId,
            "limit": limit
        ]
        sendRequest(method: "chat.history", params: params) { _ in }
    }

    /// Abort a chat session
    func abortChat(sessionId: String) {
        let params: [String: Any] = ["sessionId": sessionId]
        sendRequest(method: "chat.abort", params: params) { _ in }
    }

    /// List sessions
    func listSessions(agentId: String? = nil, channel: String? = nil, limit: Int = 50, offset: Int = 0) {
        var params: [String: Any] = [
            "limit": limit,
            "offset": offset
        ]
        if let agentId = agentId {
            params["agentId"] = agentId
        }
        if let channel = channel {
            params["channel"] = channel
        }
        sendRequest(method: "sessions.list", params: params) { _ in }
    }
}
