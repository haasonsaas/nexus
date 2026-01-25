import Foundation

/// A pending command execution approval request
struct ExecApproval: Identifiable, Equatable {
    let id: String
    let command: String
    let cwd: String
    let timestamp: Date
    let timeoutSeconds: Int
    var isExpired: Bool { Date().timeIntervalSince(timestamp) > Double(timeoutSeconds) }
}

/// Observable manager for command execution approvals
@MainActor
final class ExecApprovalsManager: ObservableObject {
    @Published private(set) var pendingApprovals: [ExecApproval] = []
    @Published var lastError: String?

    private let socket: ExecApprovalsSocket
    private var expiryTimer: Timer?
    private var alwaysAllowPatterns: Set<String> {
        get { Set(UserDefaults.standard.stringArray(forKey: "ExecApprovals_AlwaysAllow") ?? []) }
        set { UserDefaults.standard.set(Array(newValue), forKey: "ExecApprovals_AlwaysAllow") }
    }

    init(baseURL: String, apiKey: String) {
        self.socket = ExecApprovalsSocket(baseURL: baseURL, apiKey: apiKey)
        socket.onApprovalRequest = { [weak self] a in Task { @MainActor in self?.handleRequest(a) } }
        socket.onError = { [weak self] e in Task { @MainActor in self?.lastError = e } }
        expiryTimer = Timer.scheduledTimer(withTimeInterval: 1.0, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.removeExpired() }
        }
    }

    func connect() { socket.connect() }
    func disconnect() { socket.disconnect() }

    func approve(id: String) {
        guard let i = pendingApprovals.firstIndex(where: { $0.id == id }) else { return }
        socket.sendResponse(id: pendingApprovals.remove(at: i).id, approved: true)
    }

    func reject(id: String) {
        guard let i = pendingApprovals.firstIndex(where: { $0.id == id }) else { return }
        socket.sendResponse(id: pendingApprovals.remove(at: i).id, approved: false)
    }

    func rejectAll() {
        pendingApprovals.forEach { socket.sendResponse(id: $0.id, approved: false) }
        pendingApprovals.removeAll()
    }

    func alwaysAllow(id: String) {
        guard let a = pendingApprovals.first(where: { $0.id == id }) else { return }
        var p = alwaysAllowPatterns; p.insert(extractPattern(a.command)); alwaysAllowPatterns = p
        approve(id: id)
    }

    private func handleRequest(_ approval: ExecApproval) {
        if alwaysAllowPatterns.contains(extractPattern(approval.command)) {
            socket.sendResponse(id: approval.id, approved: true); return
        }
        pendingApprovals.append(approval)
        let body = approval.command.count > 80 ? String(approval.command.prefix(77)) + "..." : approval.command
        NotificationService.shared.sendNotification(title: "Command Approval Required", body: body, category: .execApproval)
    }

    private func extractPattern(_ cmd: String) -> String {
        String(cmd.trimmingCharacters(in: .whitespaces).split(separator: " ").first ?? "")
    }

    private func removeExpired() {
        pendingApprovals.filter { $0.isExpired }.forEach { socket.sendResponse(id: $0.id, approved: false) }
        pendingApprovals.removeAll { $0.isExpired }
    }

    deinit { expiryTimer?.invalidate() }
}
