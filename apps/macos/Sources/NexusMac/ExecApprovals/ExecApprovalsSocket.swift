import Foundation

/// WebSocket client for exec approval requests
final class ExecApprovalsSocket {
    var onApprovalRequest: ((ExecApproval) -> Void)?
    var onError: ((String) -> Void)?

    private var webSocket: URLSessionWebSocketTask?
    private let baseURL: String
    private let apiKey: String
    private var isConnected = false

    init(baseURL: String, apiKey: String) {
        self.baseURL = baseURL
        self.apiKey = apiKey
    }

    func connect() {
        guard !isConnected else { return }
        var url = baseURL.trimmingCharacters(in: .whitespacesAndNewlines)
        if url.hasPrefix("http://") { url = "ws://" + url.dropFirst(7) }
        else if url.hasPrefix("https://") { url = "wss://" + url.dropFirst(8) }
        else if !url.hasPrefix("ws://") && !url.hasPrefix("wss://") { url = "ws://" + url }
        url += url.hasSuffix("/") ? "ws/exec-approvals" : "/ws/exec-approvals"

        guard let wsURL = URL(string: url) else { onError?("Invalid URL"); return }
        var req = URLRequest(url: wsURL)
        req.setValue(apiKey, forHTTPHeaderField: "X-API-Key")
        webSocket = URLSession(configuration: .default).webSocketTask(with: req)
        webSocket?.resume()
        isConnected = true
        receiveMessage()
    }

    func disconnect() {
        webSocket?.cancel(with: .goingAway, reason: nil)
        webSocket = nil
        isConnected = false
    }

    func sendResponse(id: String, approved: Bool) {
        guard let data = try? JSONSerialization.data(withJSONObject: ["type": "response", "id": id, "approved": approved]),
              let text = String(data: data, encoding: .utf8) else { return }
        webSocket?.send(.string(text)) { _ in }
    }

    private func receiveMessage() {
        webSocket?.receive { [weak self] result in
            switch result {
            case .success(let msg): self?.handleMessage(msg); self?.receiveMessage()
            case .failure(let err): self?.onError?(err.localizedDescription); self?.isConnected = false
            }
        }
    }

    private func handleMessage(_ message: URLSessionWebSocketTask.Message) {
        guard case .string(let text) = message,
              let data = text.data(using: .utf8),
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              json["type"] as? String == "approval_request",
              let id = json["id"] as? String, let cmd = json["command"] as? String else { return }
        onApprovalRequest?(ExecApproval(
            id: id, command: cmd, cwd: json["cwd"] as? String ?? "",
            timestamp: Date(), timeoutSeconds: json["timeout"] as? Int ?? 30
        ))
    }
}
