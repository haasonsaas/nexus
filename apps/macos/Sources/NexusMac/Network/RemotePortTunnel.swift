import Foundation
import Network
import OSLog
#if canImport(Darwin)
import Darwin
#endif

// MARK: - Tunnel Types

/// State of an SSH tunnel connection.
enum TunnelState: Equatable {
    case disconnected
    case connecting
    case connected
    case error(String)

    var isActive: Bool {
        switch self {
        case .connecting, .connected:
            return true
        case .disconnected, .error:
            return false
        }
    }
}

/// Direction of port forwarding.
enum ForwardDirection: Equatable {
    /// Local port forwards to remote host (-L flag)
    case local
    /// Remote port forwards to local service (-R flag)
    case remote
}

/// Configuration for a single port forwarding rule.
struct PortForward: Identifiable, Equatable {
    let id: UUID
    let direction: ForwardDirection
    let localPort: Int
    let remoteHost: String
    let remotePort: Int
    let createdAt: Date

    init(
        direction: ForwardDirection,
        localPort: Int,
        remoteHost: String = "127.0.0.1",
        remotePort: Int
    ) {
        self.id = UUID()
        self.direction = direction
        self.localPort = localPort
        self.remoteHost = remoteHost
        self.remotePort = remotePort
        self.createdAt = Date()
    }

    /// SSH argument for this forward (e.g., "8080:127.0.0.1:3000")
    var sshArgument: String {
        switch direction {
        case .local:
            return "\(localPort):\(remoteHost):\(remotePort)"
        case .remote:
            return "\(remotePort):\(remoteHost):\(localPort)"
        }
    }

    /// SSH flag for this forward direction
    var sshFlag: String {
        switch direction {
        case .local:
            return "-L"
        case .remote:
            return "-R"
        }
    }
}

/// Active tunnel session containing the SSH process and metadata.
struct TunnelSession: Identifiable {
    let id: UUID
    let host: String
    let port: Int
    let user: String
    let identityFile: URL?
    let forwards: [PortForward]
    let process: Process
    let stderrHandle: FileHandle?
    let startedAt: Date
    var state: TunnelState

    init(
        host: String,
        port: Int,
        user: String,
        identityFile: URL?,
        forwards: [PortForward],
        process: Process,
        stderrHandle: FileHandle?
    ) {
        self.id = UUID()
        self.host = host
        self.port = port
        self.user = user
        self.identityFile = identityFile
        self.forwards = forwards
        self.process = process
        self.stderrHandle = stderrHandle
        self.startedAt = Date()
        self.state = .connecting
    }
}

/// Error types for tunnel operations.
enum RemotePortTunnelError: LocalizedError {
    case connectionFailed(String)
    case authenticationFailed
    case portUnavailable(Int)
    case noIdentityFile
    case processTerminated(exitCode: Int32)
    case timeout
    case alreadyConnected
    case invalidConfiguration(String)

    var errorDescription: String? {
        switch self {
        case .connectionFailed(let reason):
            return "Connection failed: \(reason)"
        case .authenticationFailed:
            return "SSH authentication failed"
        case .portUnavailable(let port):
            return "Port \(port) is unavailable"
        case .noIdentityFile:
            return "No SSH identity file specified"
        case .processTerminated(let code):
            return "SSH process terminated (exit code: \(code))"
        case .timeout:
            return "Connection timed out"
        case .alreadyConnected:
            return "Already connected to this host"
        case .invalidConfiguration(let reason):
            return "Invalid configuration: \(reason)"
        }
    }
}

// MARK: - RemotePortTunnel

/// Manages SSH tunnels for remote gateway access.
///
/// Supports both local port forwarding (access remote services locally)
/// and remote port forwarding (expose local services remotely).
@MainActor
@Observable
final class RemotePortTunnel {
    static let shared = RemotePortTunnel()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "remote-tunnel")

    // MARK: - Observable State

    /// Current tunnel state
    private(set) var state: TunnelState = .disconnected

    /// Connected host
    private(set) var connectedHost: String?

    /// Connected port
    private(set) var connectedPort: Int?

    /// Connected user
    private(set) var connectedUser: String?

    /// Active port forwards
    private(set) var activeForwards: [PortForward] = []

    /// Last error message
    private(set) var lastError: String?

    /// Time of last successful connection
    private(set) var lastConnectedAt: Date?

    /// Number of reconnection attempts
    private(set) var reconnectAttempts: Int = 0

    // MARK: - Configuration

    /// Whether to auto-reconnect on connection drop
    var autoReconnect: Bool = true

    /// Maximum reconnection attempts (0 = unlimited)
    var maxReconnectAttempts: Int = 5

    /// Delay between reconnection attempts (seconds)
    var reconnectDelay: TimeInterval = 5.0

    /// Connection timeout (seconds)
    var connectionTimeout: TimeInterval = 30.0

    // MARK: - Private State

    private var activeSessions: [UUID: TunnelSession] = [:]
    private var pendingForwards: [PortForward] = []
    private var reconnectTask: Task<Void, Never>?
    private var lastConnectionConfig: (host: String, port: Int, user: String, identityFile: URL?)?

    private init() {}

    // MARK: - Public API

    /// Connect to an SSH host and establish tunnel.
    ///
    /// - Parameters:
    ///   - host: Remote SSH host
    ///   - port: SSH port (default 22)
    ///   - user: SSH username
    ///   - identityFile: Path to SSH private key (optional, uses agent if nil)
    func connect(
        host: String,
        port: Int = 22,
        user: String,
        identityFile: URL? = nil
    ) async throws {
        logger.info("connecting to \(user)@\(host):\(port)")

        // Cancel any pending reconnect
        reconnectTask?.cancel()
        reconnectTask = nil

        // Disconnect existing connections
        if state.isActive {
            await disconnect()
        }

        state = .connecting
        lastError = nil

        // Store config for reconnection
        lastConnectionConfig = (host, port, user, identityFile)

        // Build SSH arguments
        var args: [String] = [
            "-o", "BatchMode=yes",
            "-o", "ExitOnForwardFailure=yes",
            "-o", "StrictHostKeyChecking=accept-new",
            "-o", "UpdateHostKeys=yes",
            "-o", "ServerAliveInterval=15",
            "-o", "ServerAliveCountMax=3",
            "-o", "TCPKeepAlive=yes",
            "-o", "ConnectTimeout=\(Int(connectionTimeout))",
            "-N", // Don't execute remote command
        ]

        // Add port if non-standard
        if port != 22 {
            args.append(contentsOf: ["-p", String(port)])
        }

        // Add identity file if provided
        if let identity = identityFile {
            args.append(contentsOf: ["-o", "IdentitiesOnly=yes"])
            args.append(contentsOf: ["-i", identity.path])
        }

        // Add pending forwards
        for forward in pendingForwards {
            args.append(forward.sshFlag)
            args.append(forward.sshArgument)
        }

        // Add user@host
        args.append("\(user)@\(host)")

        logger.debug("ssh args: \(args.joined(separator: " "))")

        // Create process
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/ssh")
        process.arguments = args

        // Capture stderr
        let stderrPipe = Pipe()
        process.standardError = stderrPipe
        let stderrHandle = stderrPipe.fileHandleForReading

        // Consume stderr asynchronously
        stderrHandle.readabilityHandler = { [weak self] handle in
            let data = handle.availableData
            guard !data.isEmpty else {
                handle.readabilityHandler = nil
                return
            }
            if let line = String(data: data, encoding: .utf8)?
                .trimmingCharacters(in: .whitespacesAndNewlines),
               !line.isEmpty {
                Task { @MainActor [weak self] in
                    self?.handleSSHOutput(line)
                }
            }
        }

        // Handle termination
        process.terminationHandler = { [weak self] proc in
            Task { @MainActor [weak self] in
                self?.handleProcessTermination(process: proc)
            }
        }

        // Start process
        do {
            try process.run()
        } catch {
            state = .error(error.localizedDescription)
            lastError = error.localizedDescription
            logger.error("failed to start ssh: \(error.localizedDescription)")
            throw RemotePortTunnelError.connectionFailed(error.localizedDescription)
        }

        // Create session
        let session = TunnelSession(
            host: host,
            port: port,
            user: user,
            identityFile: identityFile,
            forwards: pendingForwards,
            process: process,
            stderrHandle: stderrHandle
        )
        activeSessions[session.id] = session

        // Wait for connection to establish
        let connected = try await waitForConnection(process: process, timeout: connectionTimeout)

        if connected {
            state = .connected
            connectedHost = host
            connectedPort = port
            connectedUser = user
            activeForwards = pendingForwards
            lastConnectedAt = Date()
            reconnectAttempts = 0
            pendingForwards = []

            logger.info("tunnel connected to \(user)@\(host):\(port)")

            // Notify RemoteTunnelManager
            await notifyTunnelManagerConnected()
        } else {
            let error = "Connection timed out after \(Int(connectionTimeout)) seconds"
            state = .error(error)
            lastError = error
            process.terminate()
            activeSessions.removeValue(forKey: session.id)
            logger.error("tunnel connection timeout")
            throw RemotePortTunnelError.timeout
        }
    }

    /// Disconnect all active tunnels.
    func disconnect() {
        logger.info("disconnecting all tunnels")

        reconnectTask?.cancel()
        reconnectTask = nil

        for (_, session) in activeSessions {
            terminateSession(session)
        }

        activeSessions.removeAll()
        state = .disconnected
        connectedHost = nil
        connectedPort = nil
        connectedUser = nil
        activeForwards = []
        pendingForwards = []

        logger.info("all tunnels disconnected")
    }

    /// Add a local port forward (forward local port to remote host).
    ///
    /// - Parameters:
    ///   - localPort: Local port to listen on
    ///   - remoteHost: Remote host to forward to
    ///   - remotePort: Remote port to forward to
    func forwardLocal(localPort: Int, remoteHost: String = "127.0.0.1", remotePort: Int) {
        let forward = PortForward(
            direction: .local,
            localPort: localPort,
            remoteHost: remoteHost,
            remotePort: remotePort
        )
        pendingForwards.append(forward)
        logger.debug("queued local forward \(localPort) -> \(remoteHost):\(remotePort)")
    }

    /// Add a remote port forward (forward remote port to local service).
    ///
    /// - Parameters:
    ///   - remotePort: Remote port to listen on
    ///   - localHost: Local host to forward to
    ///   - localPort: Local port to forward to
    func forwardRemote(remotePort: Int, localHost: String = "127.0.0.1", localPort: Int) {
        let forward = PortForward(
            direction: .remote,
            localPort: localPort,
            remoteHost: localHost,
            remotePort: remotePort
        )
        pendingForwards.append(forward)
        logger.debug("queued remote forward \(remotePort) -> \(localHost):\(localPort)")
    }

    /// Clear all pending forwards.
    func clearPendingForwards() {
        pendingForwards.removeAll()
    }

    /// Manually trigger reconnection.
    func reconnect() async throws {
        guard let config = lastConnectionConfig else {
            throw RemotePortTunnelError.invalidConfiguration("No previous connection to reconnect")
        }

        // Restore pending forwards from active forwards
        pendingForwards = activeForwards

        try await connect(
            host: config.host,
            port: config.port,
            user: config.user,
            identityFile: config.identityFile
        )
    }

    // MARK: - Private Methods

    private func waitForConnection(process: Process, timeout: TimeInterval) async throws -> Bool {
        let deadline = Date().addingTimeInterval(timeout)
        let checkInterval: UInt64 = 200_000_000 // 200ms

        while Date() < deadline {
            if !process.isRunning {
                // Process exited early - connection failed
                let stderr = drainStderr(for: process)
                if stderr.contains("Permission denied") || stderr.contains("authentication") {
                    throw RemotePortTunnelError.authenticationFailed
                }
                throw RemotePortTunnelError.connectionFailed(stderr.isEmpty ? "SSH process exited" : stderr)
            }

            // Check if connection is established by verifying process is still running
            // after initial setup period (SSH exits quickly on failure)
            if Date().timeIntervalSince(Date()) > 2.0 {
                return true
            }

            try? await Task.sleep(nanoseconds: checkInterval)
        }

        // If still running after timeout, assume connected
        return process.isRunning
    }

    private func handleSSHOutput(_ line: String) {
        logger.debug("ssh output: \(line)")

        // Parse output for status updates
        if line.contains("Connection refused") {
            lastError = "Connection refused"
        } else if line.contains("Permission denied") {
            lastError = "Authentication failed"
            state = .error("Authentication failed")
        } else if line.contains("Host key verification failed") {
            lastError = "Host key verification failed"
        } else if line.contains("bind: Address already in use") {
            lastError = "Port already in use"
        } else if line.contains("Could not request local forwarding") {
            lastError = "Forward failed"
        } else if line.contains("Authenticated") || line.contains("authenticated") {
            logger.info("ssh authenticated")
        }
    }

    private func handleProcessTermination(process: Process) {
        let exitCode = process.terminationStatus

        // Find and remove the session
        if let sessionId = activeSessions.first(where: { $0.value.process === process })?.key {
            activeSessions.removeValue(forKey: sessionId)
        }

        if state == .connected {
            logger.warning("tunnel terminated unexpectedly exitCode=\(exitCode)")
            state = .error("Connection lost (exit code: \(exitCode))")
            lastError = "Connection lost"

            // Attempt reconnection if enabled
            if autoReconnect && (maxReconnectAttempts == 0 || reconnectAttempts < maxReconnectAttempts) {
                scheduleReconnect()
            }
        } else if state == .connecting {
            let stderr = drainStderr(for: process)
            let error = stderr.isEmpty ? "Connection failed" : stderr
            state = .error(error)
            lastError = error
            logger.error("tunnel connection failed: \(error)")
        }

        // Notify RemoteTunnelManager
        Task {
            await notifyTunnelManagerDisconnected()
        }
    }

    private func scheduleReconnect() {
        reconnectTask?.cancel()
        reconnectTask = Task { [weak self] in
            guard let self else { return }

            reconnectAttempts += 1
            logger.info("scheduling reconnect attempt \(self.reconnectAttempts) in \(self.reconnectDelay)s")

            try? await Task.sleep(for: .seconds(reconnectDelay))

            guard !Task.isCancelled else { return }

            do {
                try await reconnect()
            } catch {
                logger.error("reconnect failed: \(error.localizedDescription)")

                // Schedule another attempt if allowed
                if maxReconnectAttempts == 0 || reconnectAttempts < maxReconnectAttempts {
                    scheduleReconnect()
                }
            }
        }
    }

    private func terminateSession(_ session: TunnelSession) {
        // Clean up stderr handler
        if let handle = session.stderrHandle {
            handle.readabilityHandler = nil
            try? handle.close()
        }

        // Terminate process
        if session.process.isRunning {
            session.process.terminate()
            session.process.waitUntilExit()
        }

        logger.debug("terminated session \(session.id)")
    }

    private func drainStderr(for process: Process) -> String {
        // Find session with this process
        guard let session = activeSessions.values.first(where: { $0.process === process }),
              let handle = session.stderrHandle else {
            return ""
        }

        handle.readabilityHandler = nil
        defer { try? handle.close() }

        do {
            if let data = try handle.readToEnd(),
               let text = String(data: data, encoding: .utf8) {
                return text.trimmingCharacters(in: .whitespacesAndNewlines)
            }
        } catch {
            // Ignore
        }
        return ""
    }

    private func notifyTunnelManagerConnected() async {
        // RemoteTunnelManager integration:
        // The RemoteTunnelManager can observe RemotePortTunnel.shared.state
        // and read connectedHost/primaryLocalPort as needed.
        // This notification method exists for future extensions like
        // posting notifications or updating other observers.
        logger.debug("tunnel connected notification - host=\(self.connectedHost ?? "nil") port=\(self.primaryLocalPort ?? 0)")
    }

    private func notifyTunnelManagerDisconnected() async {
        // RemoteTunnelManager integration:
        // The RemoteTunnelManager can observe RemotePortTunnel.shared.state
        // to detect disconnection events.
        logger.debug("tunnel disconnected notification")
    }

    // MARK: - Port Availability

    /// Check if a local port is available.
    func isPortAvailable(_ port: Int) -> Bool {
        #if canImport(Darwin)
        return canBindIPv4(UInt16(port)) && canBindIPv6(UInt16(port))
        #else
        return true
        #endif
    }

    /// Find an available local port, optionally preferring a specific port.
    func findAvailablePort(preferred: Int? = nil) async throws -> Int {
        if let preferred, isPortAvailable(preferred) {
            return preferred
        }

        return try await withCheckedThrowingContinuation { cont in
            let queue = DispatchQueue(label: "com.nexus.mac.tunnel.port", qos: .utility)
            do {
                let listener = try NWListener(using: .tcp, on: .any)
                listener.newConnectionHandler = { connection in connection.cancel() }
                listener.stateUpdateHandler = { state in
                    switch state {
                    case .ready:
                        if let port = listener.port?.rawValue {
                            listener.stateUpdateHandler = nil
                            listener.cancel()
                            cont.resume(returning: Int(port))
                        }
                    case let .failed(error):
                        listener.stateUpdateHandler = nil
                        listener.cancel()
                        cont.resume(throwing: error)
                    default:
                        break
                    }
                }
                listener.start(queue: queue)
            } catch {
                cont.resume(throwing: error)
            }
        }
    }

    #if canImport(Darwin)
    private nonisolated func canBindIPv4(_ port: UInt16) -> Bool {
        let fd = socket(AF_INET, SOCK_STREAM, 0)
        guard fd >= 0 else { return false }
        defer { _ = Darwin.close(fd) }

        var one: Int32 = 1
        _ = setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &one, socklen_t(MemoryLayout.size(ofValue: one)))

        var addr = sockaddr_in()
        addr.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)
        addr.sin_family = sa_family_t(AF_INET)
        addr.sin_port = port.bigEndian
        addr.sin_addr = in_addr(s_addr: inet_addr("127.0.0.1"))

        let result = withUnsafePointer(to: &addr) { ptr in
            ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) { sa in
                Darwin.bind(fd, sa, socklen_t(MemoryLayout<sockaddr_in>.size))
            }
        }
        return result == 0
    }

    private nonisolated func canBindIPv6(_ port: UInt16) -> Bool {
        let fd = socket(AF_INET6, SOCK_STREAM, 0)
        guard fd >= 0 else { return false }
        defer { _ = Darwin.close(fd) }

        var one: Int32 = 1
        _ = setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &one, socklen_t(MemoryLayout.size(ofValue: one)))

        var addr = sockaddr_in6()
        addr.sin6_len = UInt8(MemoryLayout<sockaddr_in6>.size)
        addr.sin6_family = sa_family_t(AF_INET6)
        addr.sin6_port = port.bigEndian
        var loopback = in6_addr()
        _ = withUnsafeMutablePointer(to: &loopback) { ptr in
            inet_pton(AF_INET6, "::1", ptr)
        }
        addr.sin6_addr = loopback

        let result = withUnsafePointer(to: &addr) { ptr in
            ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) { sa in
                Darwin.bind(fd, sa, socklen_t(MemoryLayout<sockaddr_in6>.size))
            }
        }
        return result == 0
    }
    #endif

    // MARK: - Testing Support

    #if DEBUG
    static func _testInstance() -> RemotePortTunnel {
        RemotePortTunnel()
    }

    func _testSetState(_ newState: TunnelState) {
        state = newState
    }

    func _testAddForward(_ forward: PortForward) {
        activeForwards.append(forward)
    }
    #endif
}

// MARK: - Extensions

extension RemotePortTunnel {
    /// Quick check if tunnel is ready for use.
    var isReady: Bool {
        state == .connected && !activeForwards.isEmpty
    }

    /// Get the primary local forwarded port (for gateway access).
    var primaryLocalPort: Int? {
        activeForwards.first(where: { $0.direction == .local })?.localPort
    }

    /// Get gateway URL through tunnel.
    var gatewayURL: URL? {
        guard let port = primaryLocalPort else { return nil }
        return URL(string: "http://localhost:\(port)")
    }

    /// Human-readable connection description.
    var connectionDescription: String {
        guard let host = connectedHost, let user = connectedUser else {
            return "Not connected"
        }
        let portStr = connectedPort != 22 ? ":\(connectedPort ?? 22)" : ""
        return "\(user)@\(host)\(portStr)"
    }

    /// Whether a reconnection is possible (has previous connection config).
    var canReconnect: Bool {
        lastConnectionConfig != nil
    }
}

