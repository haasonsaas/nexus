import Darwin
import Foundation
import Network
import OSLog

// MARK: - Protocol Messages

/// Protocol for approval socket messages based on Clawdbot's exec approval socket protocol
struct ApprovalSocketMessage: Codable, Sendable {
    let type: MessageType
    let requestId: String?
    let command: String?
    let workingDirectory: String?
    let decision: ApprovalDecision?
    let token: String?

    enum MessageType: String, Codable, Sendable {
        case request // Client requesting approval
        case response // Server sending decision
        case auth // Client sending auth token
        case authOk // Server confirming auth
        case authFail // Server rejecting auth
        case ping
        case pong
    }

    enum ApprovalDecision: String, Codable, Sendable {
        case allow
        case allowAlways
        case deny
        case pending
    }

    init(
        type: MessageType,
        requestId: String? = nil,
        command: String? = nil,
        workingDirectory: String? = nil,
        decision: ApprovalDecision? = nil,
        token: String? = nil
    ) {
        self.type = type
        self.requestId = requestId
        self.command = command
        self.workingDirectory = workingDirectory
        self.decision = decision
        self.token = token
    }
}

// MARK: - Approval Request

extension ExecApprovalSocket {
    /// A pending approval request from a connected client
    struct ApprovalRequest: Identifiable, Sendable {
        let id: String
        let command: String
        let workingDirectory: String?
        let connectionId: UUID
        let requestedAt: Date

        var isExpired: Bool {
            Date().timeIntervalSince(requestedAt) > 60
        }
    }
}

// MARK: - Connected Client

private final class ConnectedClient: @unchecked Sendable {
    let id: UUID
    let connection: NWConnection
    var isAuthenticated: Bool = false
    var pendingData: Data = Data()

    init(connection: NWConnection) {
        self.id = UUID()
        self.connection = connection
    }
}

// MARK: - Exec Approval Socket Server

/// Unix domain socket server for exec approvals using Network framework.
/// Implements Clawdbot's exec approval socket protocol with token-based authentication.
@MainActor
@Observable
final class ExecApprovalSocket {
    static let shared = ExecApprovalSocket()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "approval-socket")

    // MARK: - State

    private(set) var isRunning = false
    private(set) var connectedClients: Int = 0
    private(set) var pendingRequests: [String: ApprovalRequest] = [:]

    // MARK: - Socket

    private var listener: NWListener?
    private var clients: [UUID: ConnectedClient] = [:]
    private let clientsLock = NSLock()

    // MARK: - Configuration

    let socketPath: String
    private let authToken: String

    // MARK: - Initialization

    private init() {
        // Socket path in Application Support directory
        let approvals = ExecApprovalsStore.resolve(agentId: nil)
        socketPath = approvals.socketPath
        authToken = approvals.token
    }

    // MARK: - Lifecycle

    func start() throws {
        guard !isRunning else { return }

        // Remove existing socket file
        try? FileManager.default.removeItem(atPath: socketPath)

        // Ensure parent directory exists
        let parentDir = URL(fileURLWithPath: socketPath).deletingLastPathComponent()
        try FileManager.default.createDirectory(at: parentDir, withIntermediateDirectories: true)

        // Create Unix socket endpoint
        let endpoint = NWEndpoint.unix(path: socketPath)

        // Create parameters for local Unix socket
        let parameters = NWParameters()
        parameters.allowLocalEndpointReuse = true
        parameters.acceptLocalOnly = true
        parameters.requiredLocalEndpoint = endpoint

        // Create listener
        do {
            listener = try NWListener(using: parameters)
        } catch {
            logger.error("Failed to create listener: \(error.localizedDescription, privacy: .public)")
            throw SocketError.failedToCreateListener
        }

        listener?.newConnectionHandler = { [weak self] connection in
            Task { @MainActor in
                self?.handleNewConnection(connection)
            }
        }

        listener?.stateUpdateHandler = { [weak self] state in
            Task { @MainActor in
                self?.handleListenerState(state)
            }
        }

        // Start listener
        listener?.start(queue: .main)

        // Set socket permissions (owner only) after a brief delay to ensure file exists
        Task {
            try? await Task.sleep(nanoseconds: 100_000_000) // 0.1 seconds
            try? FileManager.default.setAttributes(
                [.posixPermissions: 0o600],
                ofItemAtPath: socketPath
            )
        }

        isRunning = true
        logger.info("Approval socket started at \(self.socketPath, privacy: .public)")
    }

    func stop() {
        listener?.cancel()
        listener = nil

        clientsLock.withLock {
            for (_, client) in clients {
                client.connection.cancel()
            }
            clients.removeAll()
        }

        pendingRequests.removeAll()

        // Remove socket file
        try? FileManager.default.removeItem(atPath: socketPath)

        isRunning = false
        connectedClients = 0
        logger.info("Approval socket stopped")
    }

    // MARK: - Connection Handling

    private func handleListenerState(_ state: NWListener.State) {
        switch state {
        case .ready:
            logger.debug("Listener ready")
        case .failed(let error):
            logger.error("Listener failed: \(error.localizedDescription, privacy: .public)")
            isRunning = false
        case .cancelled:
            logger.debug("Listener cancelled")
        case .waiting(let error):
            logger.warning("Listener waiting: \(error.localizedDescription, privacy: .public)")
        default:
            break
        }
    }

    private func handleNewConnection(_ connection: NWConnection) {
        logger.debug("New client connection")

        let client = ConnectedClient(connection: connection)

        connection.stateUpdateHandler = { [weak self, clientId = client.id] state in
            Task { @MainActor in
                self?.handleConnectionState(clientId: clientId, state: state)
            }
        }

        connection.start(queue: .main)

        let count = clientsLock.withLock {
            clients[client.id] = client
            return clients.count
        }
        connectedClients = count

        // Start receiving
        receiveMessage(from: client)
    }

    private func handleConnectionState(clientId: UUID, state: NWConnection.State) {
        switch state {
        case .ready:
            logger.debug("Client \(clientId.uuidString.prefix(8), privacy: .public) connected")
        case .failed(let error):
            logger.warning("Client connection failed: \(error.localizedDescription, privacy: .public)")
            removeClient(clientId)
        case .cancelled:
            removeClient(clientId)
        default:
            break
        }
    }

    private func removeClient(_ clientId: UUID) {
        let count = clientsLock.withLock {
            clients.removeValue(forKey: clientId)
            return clients.count
        }
        connectedClients = count

        // Remove pending requests from this client
        pendingRequests = pendingRequests.filter { $0.value.connectionId != clientId }
    }

    // MARK: - Message Handling

    private func receiveMessage(from client: ConnectedClient) {
        client.connection.receive(minimumIncompleteLength: 1, maximumLength: 65536) { [weak self, clientId = client.id] data, _, isComplete, error in
            guard let self else { return }

            Task { @MainActor in
                if let data, !data.isEmpty {
                    self.processReceivedData(data, clientId: clientId)
                }

                if let error {
                    self.logger.warning("Receive error: \(error.localizedDescription, privacy: .public)")
                    return
                }

                if isComplete {
                    self.removeClient(clientId)
                    return
                }

                // Continue receiving
                let client = self.clientsLock.withLock { self.clients[clientId] }

                if let client {
                    self.receiveMessage(from: client)
                }
            }
        }
    }

    private func processReceivedData(_ data: Data, clientId: UUID) {
        let clientData = clientsLock.withLock { () -> (ConnectedClient, Data)? in
            guard let client = clients[clientId] else {
                return nil
            }
            client.pendingData.append(data)
            return (client, client.pendingData)
        }
        guard let (client, pendingData) = clientData else { return }

        // Process complete messages (newline-delimited JSON)
        var remaining = pendingData
        while let newlineIndex = remaining.firstIndex(of: 0x0A) {
            let messageData = remaining.subdata(in: 0 ..< newlineIndex)
            remaining = remaining.subdata(in: remaining.index(after: newlineIndex) ..< remaining.endIndex)

            processMessage(messageData, from: client)
        }

        // Update remaining data
        clientsLock.withLock {
            clients[clientId]?.pendingData = remaining
        }
    }

    private func processMessage(_ data: Data, from client: ConnectedClient) {
        do {
            let message = try JSONDecoder().decode(ApprovalSocketMessage.self, from: data)

            switch message.type {
            case .auth:
                handleAuth(message, from: client)
            case .request:
                handleApprovalRequest(message, from: client)
            case .ping:
                sendPong(to: client)
            default:
                logger.warning("Unexpected message type: \(message.type.rawValue, privacy: .public)")
            }
        } catch {
            logger.error("Failed to decode message: \(error.localizedDescription, privacy: .public)")
        }
    }

    private func handleAuth(_ message: ApprovalSocketMessage, from client: ConnectedClient) {
        let response: ApprovalSocketMessage

        if message.token == authToken {
            clientsLock.withLock {
                clients[client.id]?.isAuthenticated = true
            }

            response = ApprovalSocketMessage(type: .authOk)
            logger.debug("Client \(client.id.uuidString.prefix(8), privacy: .public) authenticated")
        } else {
            response = ApprovalSocketMessage(type: .authFail)
            logger.warning("Client auth failed")
        }

        sendMessage(response, to: client)
    }

    private func handleApprovalRequest(_ message: ApprovalSocketMessage, from client: ConnectedClient) {
        guard client.isAuthenticated else {
            logger.warning("Unauthenticated client sent approval request")
            let response = ApprovalSocketMessage(
                type: .response,
                requestId: message.requestId,
                decision: .deny
            )
            sendMessage(response, to: client)
            return
        }

        guard let requestId = message.requestId,
              let command = message.command
        else {
            logger.warning("Invalid approval request: missing required fields")
            return
        }

        let request = ApprovalRequest(
            id: requestId,
            command: command,
            workingDirectory: message.workingDirectory,
            connectionId: client.id,
            requestedAt: Date()
        )

        pendingRequests[requestId] = request
        logger.info("Approval request: \(command.prefix(100), privacy: .public)")

        // Check against allowlist first
        let resolved = ExecApprovalsStore.resolve(agentId: nil)

        if resolved.agent.security == .full {
            // Full access mode - auto allow
            respondToRequest(requestId, decision: .allow)
        } else if resolved.agent.security == .deny {
            // Deny all mode
            respondToRequest(requestId, decision: .deny)
        } else {
            // Check allowlist
            let resolution = ExecCommandResolution.resolve(
                command: [command],
                rawCommand: command,
                cwd: message.workingDirectory,
                env: nil
            )
            let match = ExecAllowlistMatcher.match(entries: resolved.allowlist, resolution: resolution)

            if match != nil {
                respondToRequest(requestId, decision: .allow)
                if let resolution {
                    ExecApprovalsStore.recordAllowlistUse(
                        agentId: nil,
                        pattern: match!.pattern,
                        command: command,
                        resolvedPath: resolution.resolvedPath
                    )
                }
            } else {
                // Need user approval - show prompt
                Task {
                    ExecApprovalPrompter.shared.showPrompt(for: request)
                }
            }
        }
    }

    // MARK: - Response

    func respondToRequest(_ requestId: String, decision: ApprovalSocketMessage.ApprovalDecision) {
        guard let request = pendingRequests.removeValue(forKey: requestId) else {
            logger.warning("No pending request with ID: \(requestId, privacy: .public)")
            return
        }

        let client = clientsLock.withLock { clients[request.connectionId] }

        guard let client else {
            logger.warning("Client disconnected for request: \(requestId, privacy: .public)")
            return
        }

        let response = ApprovalSocketMessage(
            type: .response,
            requestId: requestId,
            decision: decision
        )

        sendMessage(response, to: client)
        logger.info("Approval decision: \(decision.rawValue, privacy: .public) for \(request.command.prefix(50), privacy: .public)")

        // If allow always, add to allowlist
        if decision == .allowAlways {
            ExecApprovalsStore.addAllowlistEntry(agentId: nil, pattern: request.command)
        }
    }

    private func sendPong(to client: ConnectedClient) {
        let pong = ApprovalSocketMessage(type: .pong)
        sendMessage(pong, to: client)
    }

    private func sendMessage(_ message: ApprovalSocketMessage, to client: ConnectedClient) {
        do {
            var data = try JSONEncoder().encode(message)
            data.append(0x0A) // Newline delimiter

            client.connection.send(content: data, completion: .contentProcessed { [weak self] error in
                if let error {
                    self?.logger.warning("Send error: \(error.localizedDescription, privacy: .public)")
                }
            })
        } catch {
            logger.error("Failed to encode message: \(error.localizedDescription, privacy: .public)")
        }
    }

    // MARK: - Token Access

    func getAuthToken() -> String {
        authToken
    }

    func writeTokenFile() throws {
        let tokenPath = URL(fileURLWithPath: socketPath)
            .deletingLastPathComponent()
            .appendingPathComponent("nexus-exec-token")

        try authToken.write(to: tokenPath, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: tokenPath.path)
        logger.debug("Token file written to \(tokenPath.path, privacy: .public)")
    }

    // MARK: - Errors

    enum SocketError: LocalizedError {
        case failedToCreateListener
        case failedToBind
        case invalidSocketPath

        var errorDescription: String? {
            switch self {
            case .failedToCreateListener:
                return "Failed to create socket listener"
            case .failedToBind:
                return "Failed to bind to socket path"
            case .invalidSocketPath:
                return "Invalid socket path"
            }
        }
    }
}

// MARK: - Compatibility Extension

extension ExecApprovalSocket {
    /// Start the socket server if not already running
    func ensureRunning() {
        guard !isRunning else { return }
        do {
            try start()
        } catch {
            logger.error("Failed to start approval socket: \(error.localizedDescription, privacy: .public)")
        }
    }

    /// Get pending request by ID
    func getPendingRequest(_ requestId: String) -> ApprovalRequest? {
        pendingRequests[requestId]
    }

    /// Get all pending requests as array
    var pendingRequestsList: [ApprovalRequest] {
        Array(pendingRequests.values).sorted { $0.requestedAt < $1.requestedAt }
    }

    /// Remove expired requests
    func cleanupExpiredRequests() {
        let expiredIds = pendingRequests.filter { $0.value.isExpired }.map { $0.key }
        for id in expiredIds {
            respondToRequest(id, decision: .deny)
        }
    }
}
