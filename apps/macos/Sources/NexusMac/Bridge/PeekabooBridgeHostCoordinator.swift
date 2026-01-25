import CryptoKit
import Foundation
import Network
import OSLog
import Security

// MARK: - Peer Info

/// Represents a connected peer device
public struct BridgePeerInfo: Identifiable, Sendable, Equatable {
    public let id: String
    public let displayName: String
    public let platform: String
    public let version: String
    public let ipAddress: String
    public let port: UInt16
    public let connectedAt: Date
    public var lastSeenAt: Date
    public var status: PeerConnectionStatus
    public var capabilities: Set<String>

    public enum PeerConnectionStatus: String, Sendable {
        case connecting
        case authenticating
        case connected
        case ready
        case paused
        case disconnecting
        case disconnected
    }
}

// MARK: - Bridge Events

/// Events emitted by the bridge coordinator
public enum BridgeEvent: Sendable {
    case peerDiscovered(BridgePeerInfo)
    case peerConnected(BridgePeerInfo)
    case peerDisconnected(String, reason: String?)
    case peerAuthenticated(String)
    case touchReceived(TouchFrame, from: String)
    case commandReceived(CommandFrame, from: String)
    case statusReceived(StatusFrame, from: String)
    case error(BridgeCoordinatorError)
}

/// Errors from the bridge coordinator
public enum BridgeCoordinatorError: Error, Sendable {
    case notStarted
    case alreadyStarted
    case peerNotFound(String)
    case connectionFailed(String)
    case authenticationFailed(String)
    case encryptionError(String)
    case sendFailed(String)
    case invalidFrame(String)
}

// MARK: - PeekabooBridgeHostCoordinator

/// Coordinates communication between macOS host and iOS companion app.
/// Handles peer discovery, connection management, frame routing, and encryption.
@MainActor
@Observable
public final class PeekabooBridgeHostCoordinator {
    public static let shared = PeekabooBridgeHostCoordinator()

    // MARK: - Configuration

    private static let serviceType = "_nexus-bridge._tcp"
    private static let serviceDomain = "local."
    private static let serviceName = "NexusBridge"
    private static let socketPath = "/tmp/nexus-bridge.sock"

    // MARK: - State

    public private(set) var isRunning = false
    public private(set) var peers: [String: BridgePeerInfo] = [:]
    public private(set) var discoveredServices: [String: NWBrowser.Result] = [:]

    // MARK: - Private Properties

    private let logger = Logger(subsystem: "com.nexus.mac", category: "peekaboo-bridge")

    private var listener: NWListener?
    private var browser: NWBrowser?
    private var connections: [String: NWConnection] = [:]
    private var pendingConnections: [String: NWConnection] = [:]

    private var eventContinuations: [UUID: AsyncStream<BridgeEvent>.Continuation] = [:]
    private var receiveTask: Task<Void, Never>?
    private var heartbeatTask: Task<Void, Never>?
    private var reconnectTasks: [String: Task<Void, Never>] = [:]

    private var sessionKeys: [String: SymmetricKey] = [:]
    private var privateKey: P256.KeyAgreement.PrivateKey?
    private var publicKeyData: Data?

    private var frameSequence: UInt32 = 0
    private var pendingCommands: [String: CheckedContinuation<CommandFrame, Error>] = [:]

    private let allowedTeamIDs: Set<String>
    private let requestTimeoutSeconds: TimeInterval = 10

    // MARK: - Initialization

    private init() {
        var teams: Set<String> = ["Y5PE65HELJ"] // Default team ID
        if let currentTeam = Self.currentTeamID() {
            teams.insert(currentTeam)
        }
        self.allowedTeamIDs = teams

        generateKeyPair()
    }

    // MARK: - Public API

    /// Start the bridge host coordinator
    public func start() async throws {
        guard !isRunning else {
            logger.warning("Bridge coordinator already running")
            throw BridgeCoordinatorError.alreadyStarted
        }

        logger.info("Starting Peekaboo Bridge Host Coordinator")

        do {
            try startListener()
            startBrowser()
            startHeartbeat()
            isRunning = true
            logger.info("Bridge coordinator started successfully")
        } catch {
            logger.error("Failed to start bridge coordinator: \(error.localizedDescription, privacy: .public)")
            throw error
        }
    }

    /// Stop the bridge host coordinator
    public func stop() async {
        guard isRunning else { return }

        logger.info("Stopping Peekaboo Bridge Host Coordinator")

        heartbeatTask?.cancel()
        heartbeatTask = nil

        for (_, task) in reconnectTasks {
            task.cancel()
        }
        reconnectTasks.removeAll()

        browser?.cancel()
        browser = nil

        listener?.cancel()
        listener = nil

        for (peerId, connection) in connections {
            connection.cancel()
            await notifyPeerDisconnected(peerId, reason: "Host stopped")
        }
        connections.removeAll()
        pendingConnections.removeAll()

        peers.removeAll()
        discoveredServices.removeAll()
        sessionKeys.removeAll()

        isRunning = false
        logger.info("Bridge coordinator stopped")
    }

    /// Set enabled state
    public func setEnabled(_ enabled: Bool) async {
        if enabled {
            try? await start()
        } else {
            await stop()
        }
    }

    /// Subscribe to bridge events
    public func subscribe() -> AsyncStream<BridgeEvent> {
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

    /// Send screen capture to all connected peers
    public func broadcastScreen(_ frame: ScreenFrame) async {
        for peerId in connections.keys {
            await sendFrame(frame, to: peerId)
        }
    }

    /// Send screen capture to specific peer
    public func sendScreen(_ frame: ScreenFrame, to peerId: String) async {
        await sendFrame(frame, to: peerId)
    }

    /// Send command to peer and wait for response
    public func sendCommand(_ command: String, params: String? = nil, to peerId: String) async throws -> CommandFrame {
        let frame = CommandFrame(command: command, paramsJSON: params)

        return try await withCheckedThrowingContinuation { continuation in
            self.pendingCommands[frame.commandId] = continuation

            Task {
                await self.sendFrame(frame, to: peerId)

                // Timeout
                try? await Task.sleep(nanoseconds: UInt64(self.requestTimeoutSeconds * 1_000_000_000))
                if let cont = self.pendingCommands.removeValue(forKey: frame.commandId) {
                    cont.resume(throwing: BridgeCoordinatorError.sendFailed("Command timed out"))
                }
            }
        }
    }

    /// Send status update to peer
    public func sendStatus(_ status: StatusFrame, to peerId: String) async {
        await sendFrame(status, to: peerId)
    }

    /// Disconnect a specific peer
    public func disconnectPeer(_ peerId: String) async {
        guard let connection = connections.removeValue(forKey: peerId) else { return }
        connection.cancel()
        await notifyPeerDisconnected(peerId, reason: "Disconnected by host")
    }

    /// Get connected peer by ID
    public func peer(_ id: String) -> BridgePeerInfo? {
        peers[id]
    }

    /// Get all connected peers
    public var connectedPeers: [BridgePeerInfo] {
        Array(peers.values.filter { $0.status == .connected || $0.status == .ready })
    }

    // MARK: - Listener

    private func startListener() throws {
        let parameters = NWParameters.tcp
        parameters.includePeerToPeer = true

        // Configure TLS
        let tlsOptions = NWProtocolTLS.Options()
        sec_protocol_options_set_min_tls_protocol_version(tlsOptions.securityProtocolOptions, .TLSv12)
        parameters.defaultProtocolStack.applicationProtocols.insert(tlsOptions, at: 0)

        let listener = try NWListener(using: parameters)

        listener.service = NWListener.Service(
            name: Self.serviceName,
            type: Self.serviceType,
            domain: Self.serviceDomain
        )

        listener.stateUpdateHandler = { [weak self] state in
            Task { @MainActor in
                self?.handleListenerStateChange(state)
            }
        }

        listener.newConnectionHandler = { [weak self] connection in
            Task { @MainActor in
                self?.handleNewConnection(connection)
            }
        }

        listener.start(queue: .main)
        self.listener = listener

        logger.info("Started listener for service: \(Self.serviceType, privacy: .public)")
    }

    private func handleListenerStateChange(_ state: NWListener.State) {
        switch state {
        case .ready:
            if let port = listener?.port {
                logger.info("Listener ready on port \(port.rawValue, privacy: .public)")
            }
        case .failed(let error):
            logger.error("Listener failed: \(error.localizedDescription, privacy: .public)")
            emit(.error(.connectionFailed("Listener failed: \(error.localizedDescription)")))
        case .cancelled:
            logger.info("Listener cancelled")
        default:
            break
        }
    }

    // MARK: - Browser

    private func startBrowser() {
        let parameters = NWParameters()
        parameters.includePeerToPeer = true

        let browser = NWBrowser(for: .bonjour(type: Self.serviceType, domain: Self.serviceDomain), using: parameters)

        browser.stateUpdateHandler = { [weak self] state in
            Task { @MainActor in
                self?.handleBrowserStateChange(state)
            }
        }

        browser.browseResultsChangedHandler = { [weak self] results, changes in
            Task { @MainActor in
                self?.handleBrowseResultsChanged(results, changes: changes)
            }
        }

        browser.start(queue: .main)
        self.browser = browser

        logger.info("Started browser for service: \(Self.serviceType, privacy: .public)")
    }

    private func handleBrowserStateChange(_ state: NWBrowser.State) {
        switch state {
        case .ready:
            logger.info("Browser ready")
        case .failed(let error):
            logger.error("Browser failed: \(error.localizedDescription, privacy: .public)")
        case .cancelled:
            logger.info("Browser cancelled")
        default:
            break
        }
    }

    private func handleBrowseResultsChanged(_ results: Set<NWBrowser.Result>, changes: Set<NWBrowser.Result.Change>) {
        for change in changes {
            switch change {
            case .added(let result):
                if case .service(let name, _, _, _) = result.endpoint {
                    logger.info("Discovered service: \(name, privacy: .public)")
                    discoveredServices[name] = result

                    // Create placeholder peer info
                    let peerInfo = BridgePeerInfo(
                        id: name,
                        displayName: name,
                        platform: "unknown",
                        version: "unknown",
                        ipAddress: "",
                        port: 0,
                        connectedAt: Date(),
                        lastSeenAt: Date(),
                        status: .connecting,
                        capabilities: []
                    )
                    emit(.peerDiscovered(peerInfo))
                }

            case .removed(let result):
                if case .service(let name, _, _, _) = result.endpoint {
                    logger.info("Service removed: \(name, privacy: .public)")
                    discoveredServices.removeValue(forKey: name)
                }

            default:
                break
            }
        }
    }

    // MARK: - Connection Management

    private func handleNewConnection(_ connection: NWConnection) {
        let connectionId = UUID().uuidString
        logger.info("New incoming connection: \(connectionId, privacy: .public)")

        pendingConnections[connectionId] = connection

        connection.stateUpdateHandler = { [weak self] state in
            Task { @MainActor in
                self?.handleConnectionStateChange(state, connectionId: connectionId)
            }
        }

        connection.start(queue: .main)
        startReceiving(from: connection, connectionId: connectionId)
    }

    private func handleConnectionStateChange(_ state: NWConnection.State, connectionId: String) {
        switch state {
        case .ready:
            logger.info("Connection ready: \(connectionId, privacy: .public)")
            // Connection is ready, wait for auth frame

        case .failed(let error):
            logger.error("Connection failed: \(connectionId, privacy: .public) - \(error.localizedDescription, privacy: .public)")
            cleanupConnection(connectionId)

        case .cancelled:
            logger.info("Connection cancelled: \(connectionId, privacy: .public)")
            cleanupConnection(connectionId)

        default:
            break
        }
    }

    private func cleanupConnection(_ connectionId: String) {
        pendingConnections.removeValue(forKey: connectionId)

        if let connection = connections.removeValue(forKey: connectionId) {
            connection.cancel()
        }

        if peers.removeValue(forKey: connectionId) != nil {
            Task {
                await notifyPeerDisconnected(connectionId, reason: nil)
            }
        }

        sessionKeys.removeValue(forKey: connectionId)
        reconnectTasks[connectionId]?.cancel()
        reconnectTasks.removeValue(forKey: connectionId)
    }

    // MARK: - Frame Sending

    private func sendFrame(_ frame: any BridgeFrame, to peerId: String) async {
        guard let connection = connections[peerId] ?? pendingConnections[peerId] else {
            logger.warning("Cannot send frame: peer not found \(peerId, privacy: .public)")
            return
        }

        do {
            var frameData = try frame.encode()

            // Encrypt if session key exists
            if let sessionKey = sessionKeys[peerId] {
                frameData = try encrypt(frameData, with: sessionKey)
            }

            connection.send(content: frameData, completion: .contentProcessed { [weak self] error in
                if let error {
                    self?.logger.error("Send error to \(peerId, privacy: .public): \(error.localizedDescription, privacy: .public)")
                }
            })
        } catch {
            logger.error("Failed to encode frame: \(error.localizedDescription, privacy: .public)")
        }
    }

    // MARK: - Frame Receiving

    private func startReceiving(from connection: NWConnection, connectionId: String) {
        connection.receive(minimumIncompleteLength: BridgeFrameHeader.size, maximumLength: 65536) { [weak self] content, _, isComplete, error in
            Task { @MainActor in
                if let error {
                    self?.logger.error("Receive error: \(error.localizedDescription, privacy: .public)")
                    self?.cleanupConnection(connectionId)
                    return
                }

                if let data = content {
                    await self?.handleReceivedData(data, from: connectionId)
                }

                if isComplete {
                    self?.cleanupConnection(connectionId)
                } else {
                    self?.startReceiving(from: connection, connectionId: connectionId)
                }
            }
        }
    }

    private func handleReceivedData(_ data: Data, from connectionId: String) async {
        var frameData = data

        // Decrypt if session key exists
        if let sessionKey = sessionKeys[connectionId] {
            do {
                frameData = try decrypt(frameData, with: sessionKey)
            } catch {
                logger.error("Decryption failed from \(connectionId, privacy: .public): \(error.localizedDescription, privacy: .public)")
                return
            }
        }

        do {
            let frame = try BridgeFrameUtils.decodeAnyFrame(from: frameData)
            await handleFrame(frame, from: connectionId)
        } catch {
            logger.error("Frame decode error: \(error.localizedDescription, privacy: .public)")
            emit(.error(.invalidFrame(error.localizedDescription)))
        }
    }

    private func handleFrame(_ frame: any BridgeFrame, from connectionId: String) async {
        switch frame {
        case let authFrame as AuthFrame:
            await handleAuthFrame(authFrame, from: connectionId)

        case let touchFrame as TouchFrame:
            emit(.touchReceived(touchFrame, from: connectionId))
            updatePeerLastSeen(connectionId)

        case let commandFrame as CommandFrame:
            if let responseId = commandFrame.responseId,
               let continuation = pendingCommands.removeValue(forKey: responseId) {
                continuation.resume(returning: commandFrame)
            } else {
                emit(.commandReceived(commandFrame, from: connectionId))
            }
            updatePeerLastSeen(connectionId)

        case let statusFrame as StatusFrame:
            emit(.statusReceived(statusFrame, from: connectionId))
            updatePeerLastSeen(connectionId)

        case let pingFrame as PingFrame:
            let pong = PongFrame(pingId: pingFrame.pingId)
            await sendFrame(pong, to: connectionId)
            updatePeerLastSeen(connectionId)

        case is PongFrame:
            updatePeerLastSeen(connectionId)

        case let errorFrame as ErrorFrame:
            logger.error("Error from \(connectionId, privacy: .public): \(errorFrame.code, privacy: .public) - \(errorFrame.message, privacy: .public)")

        default:
            logger.warning("Unhandled frame type from \(connectionId, privacy: .public)")
        }
    }

    // MARK: - Authentication

    private func handleAuthFrame(_ frame: AuthFrame, from connectionId: String) async {
        switch frame.phase {
        case .hello:
            await handleAuthHello(frame, from: connectionId)

        case .response:
            await handleAuthResponse(frame, from: connectionId)

        case .verified:
            logger.info("Peer verified: \(connectionId, privacy: .public)")
            finalizePeerConnection(connectionId, from: frame)

        case .rejected:
            logger.warning("Auth rejected from \(connectionId, privacy: .public)")
            cleanupConnection(connectionId)

        default:
            break
        }
    }

    private func handleAuthHello(_ frame: AuthFrame, from connectionId: String) async {
        logger.info("Auth hello from \(frame.peerId, privacy: .public)")

        // Generate challenge
        var challengeBytes = [UInt8](repeating: 0, count: 32)
        _ = SecRandomCopyBytes(kSecRandomDefault, 32, &challengeBytes)
        let challenge = Data(challengeBytes)

        // Send hello-ok with challenge
        let response = AuthFrame(
            phase: .challenge,
            peerId: getHostId(),
            displayName: Host.current().localizedName,
            platform: "macos",
            version: Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "1.0",
            publicKey: publicKeyData,
            challenge: challenge,
            capabilities: ["screen-capture", "touch-relay", "command-relay"]
        )

        await sendFrame(response, to: connectionId)

        // Store peer info temporarily
        var peerInfo = BridgePeerInfo(
            id: frame.peerId,
            displayName: frame.displayName ?? frame.peerId,
            platform: frame.platform ?? "unknown",
            version: frame.version ?? "unknown",
            ipAddress: "",
            port: 0,
            connectedAt: Date(),
            lastSeenAt: Date(),
            status: .authenticating,
            capabilities: Set(frame.capabilities ?? [])
        )
        peerInfo.status = .authenticating
        peers[connectionId] = peerInfo
    }

    private func handleAuthResponse(_ frame: AuthFrame, from connectionId: String) async {
        guard let peerPublicKey = frame.publicKey,
              let signature = frame.signature else {
            logger.error("Invalid auth response from \(connectionId, privacy: .public)")
            await sendAuthRejected(to: connectionId, reason: "Missing credentials")
            return
        }

        // Derive session key from ECDH
        do {
            let sessionKey = try deriveSessionKey(from: peerPublicKey)
            sessionKeys[connectionId] = sessionKey

            // Move connection from pending to active
            if let connection = pendingConnections.removeValue(forKey: connectionId) {
                connections[frame.peerId] = connection

                // Update peer info with real ID
                if var peerInfo = peers.removeValue(forKey: connectionId) {
                    peerInfo.status = .connected
                    peers[frame.peerId] = peerInfo
                }
            }

            // Send verified
            let verified = AuthFrame(
                phase: .verified,
                peerId: getHostId()
            )
            await sendFrame(verified, to: frame.peerId)

            emit(.peerAuthenticated(frame.peerId))
            emit(.peerConnected(peers[frame.peerId]!))

            logger.info("Peer authenticated: \(frame.peerId, privacy: .public)")

        } catch {
            logger.error("Key derivation failed: \(error.localizedDescription, privacy: .public)")
            await sendAuthRejected(to: connectionId, reason: "Key derivation failed")
        }
    }

    private func sendAuthRejected(to connectionId: String, reason: String) async {
        let rejected = AuthFrame(
            phase: .rejected,
            peerId: getHostId()
        )
        await sendFrame(rejected, to: connectionId)
        cleanupConnection(connectionId)
    }

    private func finalizePeerConnection(_ connectionId: String, from frame: AuthFrame) {
        if var peerInfo = peers[connectionId] {
            peerInfo.status = .ready
            peers[connectionId] = peerInfo
        }
    }

    // MARK: - Encryption

    private func generateKeyPair() {
        privateKey = P256.KeyAgreement.PrivateKey()
        publicKeyData = privateKey?.publicKey.x963Representation
    }

    private func deriveSessionKey(from peerPublicKeyData: Data) throws -> SymmetricKey {
        guard let privateKey else {
            throw BridgeCoordinatorError.encryptionError("No private key")
        }

        let peerPublicKey = try P256.KeyAgreement.PublicKey(x963Representation: peerPublicKeyData)
        let sharedSecret = try privateKey.sharedSecretFromKeyAgreement(with: peerPublicKey)

        return sharedSecret.hkdfDerivedSymmetricKey(
            using: SHA256.self,
            salt: "nexus-bridge-v1".data(using: .utf8)!,
            sharedInfo: Data(),
            outputByteCount: 32
        )
    }

    private func encrypt(_ data: Data, with key: SymmetricKey) throws -> Data {
        let sealedBox = try AES.GCM.seal(data, using: key)
        guard let combined = sealedBox.combined else {
            throw BridgeCoordinatorError.encryptionError("Failed to combine sealed box")
        }
        return combined
    }

    private func decrypt(_ data: Data, with key: SymmetricKey) throws -> Data {
        let sealedBox = try AES.GCM.SealedBox(combined: data)
        return try AES.GCM.open(sealedBox, using: key)
    }

    // MARK: - Heartbeat

    private func startHeartbeat() {
        heartbeatTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 30_000_000_000) // 30 seconds
                await self?.sendHeartbeats()
                await self?.checkStaleConnections()
            }
        }
    }

    private func sendHeartbeats() async {
        let ping = PingFrame()
        for peerId in connections.keys {
            await sendFrame(ping, to: peerId)
        }
    }

    private func checkStaleConnections() async {
        let staleThreshold: TimeInterval = 90 // 90 seconds

        for (peerId, peerInfo) in peers {
            if Date().timeIntervalSince(peerInfo.lastSeenAt) > staleThreshold {
                logger.warning("Peer stale, disconnecting: \(peerId, privacy: .public)")
                await disconnectPeer(peerId)
                scheduleReconnect(peerId)
            }
        }
    }

    private func scheduleReconnect(_ peerId: String) {
        reconnectTasks[peerId]?.cancel()

        reconnectTasks[peerId] = Task { [weak self] in
            try? await Task.sleep(nanoseconds: 5_000_000_000) // 5 seconds

            guard !Task.isCancelled else { return }

            if let service = await self?.discoveredServices[peerId] {
                await self?.connectToService(service)
            }
        }
    }

    private func connectToService(_ service: NWBrowser.Result) async {
        let parameters = NWParameters.tcp
        parameters.includePeerToPeer = true

        let connection = NWConnection(to: service.endpoint, using: parameters)
        let connectionId = UUID().uuidString

        pendingConnections[connectionId] = connection

        connection.stateUpdateHandler = { [weak self] state in
            Task { @MainActor in
                self?.handleConnectionStateChange(state, connectionId: connectionId)
            }
        }

        connection.start(queue: .main)
        startReceiving(from: connection, connectionId: connectionId)
    }

    // MARK: - Helpers

    private func updatePeerLastSeen(_ connectionId: String) {
        if var peerInfo = peers[connectionId] {
            peerInfo.lastSeenAt = Date()
            peers[connectionId] = peerInfo
        }
    }

    private func emit(_ event: BridgeEvent) {
        for (_, continuation) in eventContinuations {
            continuation.yield(event)
        }
    }

    private func notifyPeerDisconnected(_ peerId: String, reason: String?) async {
        emit(.peerDisconnected(peerId, reason: reason))
    }

    private func getHostId() -> String {
        if let hostId = UserDefaults.standard.string(forKey: "NexusBridgeHostId") {
            return hostId
        }
        let newId = UUID().uuidString
        UserDefaults.standard.set(newId, forKey: "NexusBridgeHostId")
        return newId
    }

    private static func currentTeamID() -> String? {
        var code: SecCode?
        guard SecCodeCopySelf(SecCSFlags(), &code) == errSecSuccess,
              let code else { return nil }

        var staticCode: SecStaticCode?
        guard SecCodeCopyStaticCode(code, SecCSFlags(), &staticCode) == errSecSuccess,
              let staticCode else { return nil }

        var infoCF: CFDictionary?
        guard SecCodeCopySigningInformation(
            staticCode,
            SecCSFlags(rawValue: kSecCSSigningInformation),
            &infoCF) == errSecSuccess,
            let info = infoCF as? [String: Any] else { return nil }

        return info[kSecCodeInfoTeamIdentifier as String] as? String
    }
}
