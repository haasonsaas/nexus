import Foundation
import Observation

// MARK: - Event Types

struct ControlHeartbeatEvent: Codable, Sendable {
    let ts: Double
    let status: String
    let to: String?
    let preview: String?
    let durationMs: Double?
    let hasMedia: Bool?
    let reason: String?
}

struct ControlAgentEvent: Codable, Sendable, Identifiable {
    var id: String { "\(runId)-\(seq)" }
    let runId: String
    let seq: Int
    let stream: String
    let ts: Double
    let data: [String: AnyCodable]
    let summary: String?
}

enum ControlChannelError: Error, LocalizedError {
    case disconnected
    case badResponse(String)
    case timeout

    var errorDescription: String? {
        switch self {
        case .disconnected: "Control channel disconnected"
        case .badResponse(let msg): msg
        case .timeout: "Request timed out"
        }
    }
}

// MARK: - ControlChannel

@MainActor
@Observable
final class ControlChannel {
    static let shared = ControlChannel()

    enum ConnectionState: Equatable {
        case disconnected, connecting, connected, degraded(String)
    }

    private(set) var state: ConnectionState = .disconnected {
        didSet {
            guard oldValue != state else { return }
            if case .disconnected = state { scheduleRecovery(reason: "disconnected") }
            if case .degraded(let msg) = state { scheduleRecovery(reason: msg) }
        }
    }

    private(set) var lastPingMs: Double?
    var onRequest: (@Sendable (String, [String: Any]) async throws -> Data?)?
    private var eventTask: Task<Void, Never>?
    private var recoveryTask: Task<Void, Never>?
    private var lastRecoveryAt: Date?

    private init() { startEventStream() }

    func configure() async {
        await refreshEndpoint(reason: "configure")
    }

    func refreshEndpoint(reason: String) async {
        state = .connecting
        do {
            try await GatewayConnection.shared.refresh()
            let ok = try await GatewayConnection.shared.healthOK(timeoutMs: 5000)
            state = ok ? .connected : .degraded("gateway health not ok")
        } catch {
            state = .degraded(friendlyMessage(error))
        }
    }

    func disconnect() async {
        await GatewayConnection.shared.shutdown()
        state = .disconnected
        lastPingMs = nil
    }

    func health(timeout: TimeInterval = 15) async throws -> Data {
        let start = Date()
        let params: [String: AnyHashable]? = timeout != 15 ? ["timeout": Int(timeout * 1000)] : nil
        let payload = try await request(method: "health", params: params, timeoutMs: timeout * 1000)
        lastPingMs = Date().timeIntervalSince(start) * 1000
        state = .connected
        return payload
    }

    func request(method: String, params: [String: AnyHashable]? = nil, timeoutMs: Double? = nil) async throws -> Data {
        let rawParams = params?.reduce(into: [String: AnyCodable]()) { $0[$1.key] = AnyCodable($1.value.base) }
        return try await requestRaw(method: method, params: rawParams, timeoutMs: timeoutMs)
    }

    func requestAny(method: String, params: [String: Any]? = nil, timeoutMs: Double? = nil) async throws -> Data {
        let rawParams = params?.mapValues { AnyCodable($0) }
        return try await requestRaw(method: method, params: rawParams, timeoutMs: timeoutMs)
    }

    private func requestRaw(method: String, params: [String: AnyCodable]?, timeoutMs: Double? = nil) async throws -> Data {
        do {
            let data = try await GatewayConnection.shared.request(method: method, params: params, timeoutMs: timeoutMs)
            state = .connected
            return data
        } catch {
            state = .degraded(friendlyMessage(error))
            throw ControlChannelError.badResponse(friendlyMessage(error))
        }
    }

    func sendSystemEvent(_ text: String, params: [String: AnyHashable] = [:]) async throws {
        var merged = params
        merged["text"] = text
        _ = try await request(method: "system-event", params: merged)
    }

    private func scheduleRecovery(reason: String) {
        let now = Date()
        if let last = lastRecoveryAt, now.timeIntervalSince(last) < 10 { return }
        guard recoveryTask == nil else { return }
        lastRecoveryAt = now
        recoveryTask = Task { [weak self] in
            try? await Task.sleep(nanoseconds: 2_000_000_000)
            await self?.refreshEndpoint(reason: "recovery:\(reason)")
            self?.recoveryTask = nil
        }
    }

    private func startEventStream() {
        eventTask?.cancel()
        eventTask = Task { [weak self] in
            let stream = await GatewayConnection.shared.subscribe()
            for await push in stream {
                if Task.isCancelled { return }
                await MainActor.run { self?.handle(push: push) }
            }
        }
    }

    private func handle(push: GatewayPush) {
        switch push {
        case .event(let evt) where evt.event == "agent":
            if let p = evt.payload, let agent = try? JSONDecoder().decode(ControlAgentEvent.self, from: p) {
                NotificationCenter.default.post(name: .controlAgentEvent, object: agent)
            }
        case .event(let evt) where evt.event == "heartbeat":
            if let p = evt.payload, let hb = try? JSONDecoder().decode(ControlHeartbeatEvent.self, from: p) {
                NotificationCenter.default.post(name: .controlHeartbeat, object: hb)
            }
        case .event(let evt) where evt.event == "canvas.open":
            handleCanvasOpen(evt.payload)
        case .event(let evt) where evt.event == "canvas.update":
            handleCanvasUpdate(evt.payload)
        case .event(let evt) where evt.event == "canvas.close":
            handleCanvasClose(evt.payload)
        case .event(let evt) where evt.event == "shutdown":
            state = .degraded("gateway shutdown")
        case .snapshot:
            state = .connected
        default: break
        }
    }

    // MARK: - Canvas Event Handlers

    private func handleCanvasOpen(_ payload: Data?) {
        guard let payload,
              let json = try? JSONSerialization.jsonObject(with: payload) as? [String: Any],
              let canvasId = json["canvasId"] as? String else { return }

        let title = json["title"] as? String
        let html = json["html"] as? String
        let urlString = json["url"] as? String
        let width = json["width"] as? CGFloat ?? 800
        let height = json["height"] as? CGFloat ?? 600
        let size = CGSize(width: width, height: height)

        if let html {
            CanvasManager.shared.open(sessionId: canvasId, html: html, title: title, size: size)
        } else if let urlString, let url = URL(string: urlString) {
            CanvasManager.shared.openURL(sessionId: canvasId, url: url, title: title, size: size)
        }
    }

    private func handleCanvasUpdate(_ payload: Data?) {
        guard let payload,
              let json = try? JSONSerialization.jsonObject(with: payload) as? [String: Any],
              let canvasId = json["canvasId"] as? String else { return }

        if let message = json["message"] as? String {
            let data = json["data"] as? [String: Any]
            Task {
                await CanvasManager.shared.sendMessage(sessionId: canvasId, message: message, data: data)
            }
        } else if let script = json["script"] as? String {
            Task {
                _ = try? await CanvasManager.shared.executeJS(sessionId: canvasId, script: script)
            }
        }
    }

    private func handleCanvasClose(_ payload: Data?) {
        guard let payload,
              let json = try? JSONSerialization.jsonObject(with: payload) as? [String: Any],
              let canvasId = json["canvasId"] as? String else { return }

        CanvasManager.shared.close(sessionId: canvasId)
    }

    private func friendlyMessage(_ error: Error) -> String {
        if let ctrl = error as? ControlChannelError, let d = ctrl.errorDescription { return d }
        let port = GatewayEnvironment.gatewayPort()
        if let url = error as? URLError {
            switch url.code {
            case .cancelled: return "Gateway closed; start the gateway (localhost:\(port)) and retry."
            case .cannotFindHost, .cannotConnectToHost: return "Cannot reach gateway at localhost:\(port); ensure it is running."
            case .networkConnectionLost: return "Gateway connection dropped; retry."
            case .timedOut: return "Gateway request timed out; check gateway on localhost:\(port)."
            case .notConnectedToInternet: return "No network connectivity."
            default: break
            }
        }
        let ns = error as NSError
        let detail = ns.localizedDescription.isEmpty ? "unknown error" : ns.localizedDescription
        return detail.lowercased().hasPrefix("gateway") ? detail : "Gateway error: \(detail)"
    }
}

extension Notification.Name {
    static let controlHeartbeat = Notification.Name("nexus.control.heartbeat")
    static let controlAgentEvent = Notification.Name("nexus.control.agent")
}
