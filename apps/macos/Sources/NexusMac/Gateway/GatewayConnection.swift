import Foundation

// MARK: - Gateway Push Types

enum GatewayPush: Sendable {
    case snapshot(HelloOkPayload)
    case event(GatewayEventFrame)
}

struct HelloOkPayload: Sendable {
    let serverId: String
    let protocolVersion: Int
    let canvasHostUrl: String?
}

struct GatewayEventFrame: Sendable {
    let event: String
    let seq: Int64
    let payload: Data?
}

// MARK: - GatewayConnection

actor GatewayConnection {
    static let shared = GatewayConnection()

    private var webSocket: URLSessionWebSocketTask?
    private var session: URLSession?
    private var requestCounter: Int = 0
    private var pendingRequests: [String: CheckedContinuation<Data, Error>] = [:]
    private var subscribers: [UUID: AsyncStream<GatewayPush>.Continuation] = [:]
    private var lastHello: HelloOkPayload?
    private var receiveTask: Task<Void, Never>?
    private var expectedSeq: Int64 = 0

    private init() {}

    private func gatewayURL() -> URL? {
        let port = GatewayEnvironment.gatewayPort()
        let base = UserDefaults.standard.string(forKey: "NexusBaseURL") ?? "http://localhost:\(port)"
        var url = base.trimmingCharacters(in: .whitespacesAndNewlines)
        if url.hasPrefix("http://") { url = "ws://" + url.dropFirst(7) }
        else if url.hasPrefix("https://") { url = "wss://" + url.dropFirst(8) }
        else if !url.hasPrefix("ws://") && !url.hasPrefix("wss://") { url = "ws://" + url }
        if !url.hasSuffix("/ws") { url += url.hasSuffix("/") ? "ws" : "/ws" }
        return URL(string: url)
    }

    private func gatewayToken() -> String { KeychainStore().read() ?? "" }

    func refresh() async throws {
        await shutdown()
        guard let url = gatewayURL() else {
            throw NSError(domain: "Gateway", code: 1, userInfo: [NSLocalizedDescriptionKey: "Invalid URL"])
        }
        var req = URLRequest(url: url)
        req.setValue(gatewayToken(), forHTTPHeaderField: "X-API-Key")
        req.timeoutInterval = 30
        session = URLSession(configuration: .default)
        webSocket = session?.webSocketTask(with: req)
        webSocket?.resume()
        expectedSeq = 0
        startReceiving()
        try await performHandshake()
    }

    func shutdown() async {
        receiveTask?.cancel()
        receiveTask = nil
        webSocket?.cancel(with: .goingAway, reason: nil)
        webSocket = nil
        session = nil
        lastHello = nil
        for (_, cont) in pendingRequests { cont.resume(throwing: ControlChannelError.disconnected) }
        pendingRequests.removeAll()
    }

    func healthOK(timeoutMs: Int = 8000) async throws -> Bool {
        let data = try await request(method: "health", params: nil, timeoutMs: Double(timeoutMs))
        struct R: Decodable { let ok: Bool? }
        return (try? JSONDecoder().decode(R.self, from: data))?.ok ?? true
    }

    func request(method: String, params: [String: AnyCodable]?, timeoutMs: Double? = nil) async throws -> Data {
        guard let ws = webSocket else { throw ControlChannelError.disconnected }
        let id = nextRequestId()
        let frame = WSRequestFrame(type: "req", id: id, method: method, params: params?.mapValues { $0.value as Any })
        let data = try JSONEncoder().encode(frame)
        guard let text = String(data: data, encoding: .utf8) else {
            throw NSError(domain: "Gateway", code: 2, userInfo: [NSLocalizedDescriptionKey: "Encode failed"])
        }
        let timeout = timeoutMs ?? 15000

        return try await withCheckedThrowingContinuation { continuation in
            self.pendingRequests[id] = continuation
            ws.send(.string(text)) { error in
                if let error {
                    Task {
                        await self.failPendingRequest(id: id, error: error)
                    }
                }
            }
            Task {
                try? await Task.sleep(nanoseconds: UInt64(timeout * 1_000_000))
                self.failPendingRequest(id: id, error: ControlChannelError.timeout)
            }
        }
    }

    func subscribe(bufferingNewest: Int = 100) -> AsyncStream<GatewayPush> {
        let id = UUID()
        let snapshot = lastHello
        return AsyncStream(bufferingPolicy: .bufferingNewest(bufferingNewest)) { continuation in
            if let s = snapshot { continuation.yield(.snapshot(s)) }
            self.subscribers[id] = continuation
            continuation.onTermination = { _ in Task { await self.removeSubscriber(id) } }
        }
    }

    private func nextRequestId() -> String { requestCounter += 1; return "req-\(requestCounter)" }
    private func removePendingRequest(_ id: String) { pendingRequests.removeValue(forKey: id) }
    private func removeSubscriber(_ id: UUID) { subscribers.removeValue(forKey: id) }
    private func failPendingRequest(id: String, error: Error) {
        guard let continuation = pendingRequests[id] else { return }
        pendingRequests.removeValue(forKey: id)
        continuation.resume(throwing: error)
    }

    private func broadcast(_ push: GatewayPush) {
        if case .snapshot(let h) = push { lastHello = h }
        for (_, cont) in subscribers { cont.yield(push) }
    }

    private func performHandshake() async throws {
        let params: [String: Any] = [
            "minProtocol": 1, "maxProtocol": 1,
            "client": ["id": UUID().uuidString, "version": Bundle.main.infoDictionary?["CFBundleShortVersionString"] ?? "1.0", "platform": "macos", "mode": "desktop"],
            "auth": ["token": gatewayToken()]
        ]
        let resp = try await request(method: "connect", params: params.mapValues { AnyCodable($0) })
        struct Hello: Decodable {
            let ok: Bool?
            let server: Server?
            struct Server: Decodable { let id: String?; let canvasHostUrl: String? }
            let `protocol`: Int?
        }
        let hello = try JSONDecoder().decode(Hello.self, from: resp)
        guard hello.ok ?? false else {
            throw NSError(domain: "Gateway", code: 3, userInfo: [NSLocalizedDescriptionKey: "Hello failed"])
        }
        let payload = HelloOkPayload(serverId: hello.server?.id ?? "", protocolVersion: hello.protocol ?? 1, canvasHostUrl: hello.server?.canvasHostUrl)
        lastHello = payload
        broadcast(.snapshot(payload))
    }

    private func startReceiving() {
        receiveTask = Task { [weak self] in
            while !Task.isCancelled {
                guard let ws = await self?.webSocket else { break }
                do {
                    let msg = try await ws.receive()
                    await self?.handleMessage(msg)
                } catch { break }
            }
        }
    }

    private func handleMessage(_ msg: URLSessionWebSocketTask.Message) {
        switch msg {
        case .string(let t): parseFrame(t)
        case .data(let d): if let t = String(data: d, encoding: .utf8) { parseFrame(t) }
        @unknown default: break
        }
    }

    private func parseFrame(_ text: String) {
        guard let data = text.data(using: .utf8),
              let frame = try? JSONDecoder().decode(WSResponseFrame.self, from: data) else { return }

        if frame.type == "res", let id = frame.id, let cont = pendingRequests[id] {
            pendingRequests.removeValue(forKey: id)
            if let err = frame.error {
                cont.resume(throwing: NSError(domain: "Gateway", code: 4, userInfo: [NSLocalizedDescriptionKey: err.message]))
            } else if let p = frame.payload, let pd = try? JSONSerialization.data(withJSONObject: p.value) {
                cont.resume(returning: pd)
            } else {
                cont.resume(returning: Data())
            }
        } else if frame.type == "event", let event = frame.event {
            let seq = frame.seq ?? 0
            if seq > 0 && seq != expectedSeq + 1 { print("[Gateway] seq gap") }
            expectedSeq = seq
            let pd = frame.payload.flatMap { try? JSONSerialization.data(withJSONObject: $0.value) }
            broadcast(.event(GatewayEventFrame(event: event, seq: seq, payload: pd)))
        }
    }
}
