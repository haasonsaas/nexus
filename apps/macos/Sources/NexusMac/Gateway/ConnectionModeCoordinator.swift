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
        state = .disconnected
        connectionTask?.cancel()
        healthCheckTask?.cancel()

        // Stop gateway process if running locally
        GatewayProcessManager.shared.stop()

        // Disconnect control channel
        ControlChannel.shared.disconnect()

        logger.info("disconnected from gateway")
    }

    // MARK: - Local Connection

    private func connectLocal() async {
        state = .connecting
        reconnectAttempts = 0

        connectionTask = Task {
            do {
                // Start the local gateway process
                try await GatewayProcessManager.shared.start()

                // Wait for gateway to be ready
                try await waitForGatewayReady()

                // Connect control channel
                let port = AppStateStore.shared.gatewayPort
                try await ControlChannel.shared.connect(to: "ws://127.0.0.1:\(port)/control")

                state = .connected
                lastConnectedAt = Date()

                // Start health monitoring
                startHealthCheck()

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

        connectionTask = Task {
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

                // Connect control channel to remote
                let port = AppStateStore.shared.gatewayPort
                try await ControlChannel.shared.connect(to: "ws://\(host):\(port)/control")

                state = .connected
                lastConnectedAt = Date()

                // Start health monitoring
                startHealthCheck()

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
        try await RemoteTunnelManager.shared.start(
            target: "\(user)@\(host)",
            identityFile: identityFile,
            localPort: AppStateStore.shared.gatewayPort
        )
        logger.info("SSH tunnel established to \(host)")
    }

    // MARK: - Health Check

    private func startHealthCheck() {
        healthCheckTask?.cancel()
        healthCheckTask = Task {
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(10))

                guard !Task.isCancelled else { break }

                // Check if control channel is still connected
                if !ControlChannel.shared.isConnected {
                    logger.warning("control channel disconnected, attempting reconnect")
                    state = .reconnecting
                    await reconnect()
                    break
                }

                // Ping the gateway
                do {
                    _ = try await ControlChannel.shared.send(method: "health.ping", params: [:])
                } catch {
                    logger.warning("health check failed: \(error.localizedDescription)")
                }
            }
        }
    }

    // MARK: - Helpers

    private func waitForGatewayReady(timeout: TimeInterval = 30) async throws {
        let deadline = Date().addingTimeInterval(timeout)

        while Date() < deadline {
            if GatewayProcessManager.shared.isRunning {
                // Give it a moment to fully initialize
                try await Task.sleep(for: .milliseconds(500))
                return
            }
            try await Task.sleep(for: .milliseconds(100))
        }

        throw ConnectionError.gatewayTimeout
    }

    private func scheduleReconnect() {
        guard reconnectAttempts < 5 else {
            logger.error("max reconnect attempts reached")
            return
        }

        reconnectAttempts += 1
        let delay = Double(reconnectAttempts * 2) // Exponential backoff

        Task {
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

        var errorDescription: String? {
            switch self {
            case .noRemoteHost:
                return "No remote host configured"
            case .gatewayTimeout:
                return "Gateway failed to start in time"
            case .tunnelFailed:
                return "SSH tunnel connection failed"
            }
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
