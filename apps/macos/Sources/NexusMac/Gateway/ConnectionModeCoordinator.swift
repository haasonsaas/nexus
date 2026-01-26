import Foundation
import OSLog

/// Coordinates local vs remote connection modes and gateway lifecycle.
/// Unifies the connection management between UI state and backend services.
@MainActor
@Observable
final class ConnectionModeCoordinator {
    static let shared = ConnectionModeCoordinator()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "connection")

    // MARK: - State

    enum ConnectionState: Equatable {
        case disconnected
        case connecting
        case connected
        case reconnecting
        case error(String)
    }

    private(set) var state: ConnectionState = .disconnected
    private(set) var lastConnectedAt: Date?
    private(set) var reconnectAttempts: Int = 0

    private var connectionTask: Task<Void, Never>?
    private var healthCheckTask: Task<Void, Never>?

    private init() {}

    // MARK: - Public API

    /// Apply the selected connection mode
    func apply(mode: ConnectionMode, paused: Bool = false) async {
        logger.info("applying connection mode: \(mode.rawValue), paused: \(paused)")

        // Cancel any existing connection
        connectionTask?.cancel()
        healthCheckTask?.cancel()

        if paused {
            await disconnect()
            return
        }

        switch mode {
        case .local:
            await connectLocal()
        case .remote:
            await connectRemote()
        case .unconfigured:
            await disconnect()
        }
    }

    /// Force reconnect
    func reconnect() async {
        let mode = AppStateStore.shared.connectionMode
        await apply(mode: mode, paused: AppStateStore.shared.isPaused)
    }

    /// Disconnect from gateway
    func disconnect() async {
        let wasConnected = state == .connected
        state = .disconnected
        connectionTask?.cancel()
        healthCheckTask?.cancel()

        // Stop gateway process if running locally
        GatewayProcessManager.shared.stop()

        // Disconnect control channel
        await ControlChannel.shared.disconnect()

        // Send notification if we were previously connected
        if wasConnected {
            NotificationService.shared.notifyGatewayDisconnected()
        }

        logger.info("disconnected from gateway")
    }

    // MARK: - Local Connection

    private func connectLocal() async {
        state = .connecting
        reconnectAttempts = 0

        connectionTask = Task { @MainActor in
            do {
                // Start the local gateway process
                GatewayProcessManager.shared.setActive(true)

                // Wait for gateway to be ready
                try await waitForGatewayReady()

                let port = AppStateStore.shared.gatewayPort
                updateBaseURL(host: "127.0.0.1", port: port)

                // Connect control channel
                await ControlChannel.shared.refreshEndpoint(reason: "connect-local")
                guard ControlChannel.shared.state == .connected else {
                    throw ConnectionError.gatewayUnavailable
                }

                state = .connected
                lastConnectedAt = Date()

                // Start health monitoring
                startHealthCheck()

                // Send notification
                NotificationService.shared.notifyGatewayConnected()

                logger.info("connected to local gateway on port \(port)")
            } catch {
                if !Task.isCancelled {
                    state = .error(error.localizedDescription)
                    logger.error("failed to connect local: \(error.localizedDescription)")
                    scheduleReconnect()
                }
            }
        }
    }

    // MARK: - Remote Connection

    private func connectRemote() async {
        state = .connecting
        reconnectAttempts = 0

        connectionTask = Task { @MainActor in
            do {
                guard let host = AppStateStore.shared.remoteHost else {
                    throw ConnectionError.noRemoteHost
                }

                // Check if Tailscale is available for VPN connection
                if TailscaleService.shared.isAvailable {
                    logger.info("using Tailscale for remote connection")
                }

                // Try SSH tunnel if configured
                if let identityFile = AppStateStore.shared.remoteIdentityFile {
                    try await setupSSHTunnel(
                        host: host,
                        user: AppStateStore.shared.remoteUser,
                        identityFile: identityFile
                    )
                }

                // Configure base URL for remote or tunnel
                let port = RemoteTunnelManager.shared.localPort ?? AppStateStore.shared.gatewayPort
                let baseHost = RemoteTunnelManager.shared.isConnected ? "127.0.0.1" : host
                updateBaseURL(host: baseHost, port: port)

                // Connect control channel to remote
                await ControlChannel.shared.refreshEndpoint(reason: "connect-remote")
                guard ControlChannel.shared.state == .connected else {
                    throw ConnectionError.gatewayUnavailable
                }

                state = .connected
                lastConnectedAt = Date()

                // Start health monitoring
                startHealthCheck()

                // Send notification
                NotificationService.shared.notifyGatewayConnected()

                logger.info("connected to remote gateway at \(host)")
            } catch {
                if !Task.isCancelled {
                    state = .error(error.localizedDescription)
                    logger.error("failed to connect remote: \(error.localizedDescription)")
                    scheduleReconnect()
                }
            }
        }
    }

    // MARK: - SSH Tunnel

    private func setupSSHTunnel(host: String, user: String, identityFile: String) async throws {
        // Use RemoteTunnelManager to establish SSH tunnel
        let config = RemoteTunnelManager.TunnelConfig(
            host: host,
            user: user,
            remotePort: AppStateStore.shared.gatewayPort,
            localPort: AppStateStore.shared.gatewayPort,
            identityFile: identityFile
        )
        try await RemoteTunnelManager.shared.connect(config: config)
        logger.info("SSH tunnel established to \(host)")
    }

    // MARK: - Health Check

    private func startHealthCheck() {
        healthCheckTask?.cancel()
        healthCheckTask = Task { @MainActor in
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(10))

                guard !Task.isCancelled else { break }

                // Check if control channel is still connected
                if ControlChannel.shared.state != .connected {
                    logger.warning("control channel disconnected, attempting reconnect")
                    state = .reconnecting
                    await reconnect()
                    break
                }

                // Ping the gateway
                do {
                    _ = try await ControlChannel.shared.health(timeout: 10)
                } catch {
                    logger.warning("health check failed: \(error.localizedDescription)")
                }
            }
        }
    }

    // MARK: - Helpers

    private func waitForGatewayReady(timeout: TimeInterval = 30) async throws {
        let deadline = Date().addingTimeInterval(timeout)

        let ready = await GatewayProcessManager.shared.waitForGatewayReady(timeout: timeout)
        if !ready {
            throw ConnectionError.gatewayTimeout
        }
    }

    private func scheduleReconnect() {
        guard reconnectAttempts < 5 else {
            logger.error("max reconnect attempts reached")
            return
        }

        reconnectAttempts += 1
        let delay = Double(reconnectAttempts * 2) // Exponential backoff

        Task { @MainActor in
            try? await Task.sleep(for: .seconds(delay))
            if state != .connected {
                await reconnect()
            }
        }
    }

    // MARK: - Errors

    enum ConnectionError: LocalizedError {
        case noRemoteHost
        case gatewayTimeout
        case tunnelFailed
        case gatewayUnavailable

        var errorDescription: String? {
            switch self {
            case .noRemoteHost:
                return "No remote host configured"
            case .gatewayTimeout:
                return "Gateway failed to start in time"
            case .tunnelFailed:
                return "SSH tunnel connection failed"
            case .gatewayUnavailable:
                return "Gateway did not respond to health check"
            }
        }
    }
}

// MARK: - Base URL Helpers

private extension ConnectionModeCoordinator {
    func updateBaseURL(host: String, port: Int) {
        var components = URLComponents()
        components.scheme = "http"
        components.host = host
        components.port = port
        if let url = components.url {
            UserDefaults.standard.set(url.absoluteString, forKey: "NexusBaseURL")
        }
    }
}

// MARK: - State Helpers

extension ConnectionModeCoordinator {
    var isConnected: Bool {
        state == .connected
    }

    var isConnecting: Bool {
        if case .connecting = state { return true }
        if case .reconnecting = state { return true }
        return false
    }

    var errorMessage: String? {
        if case .error(let message) = state {
            return message
        }
        return nil
    }

    var statusDescription: String {
        switch state {
        case .disconnected:
            return "Not connected"
        case .connecting:
            return "Connecting..."
        case .connected:
            return "Connected"
        case .reconnecting:
            return "Reconnecting..."
        case .error(let message):
            return "Error: \(message)"
        }
    }
}
