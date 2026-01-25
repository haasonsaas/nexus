import Foundation
import OSLog

/// Manages SSH tunnels for remote gateway connections.
/// Enables connecting to remote nexus instances over SSH.
@MainActor
@Observable
final class RemoteTunnelManager {
    static let shared = RemoteTunnelManager()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "tunnel")

    private(set) var isConnected = false
    private(set) var remoteHost: String?
    private(set) var localPort: Int?
    private(set) var error: Error?

    private var tunnelProcess: Process?
    private var outputPipe: Pipe?

    struct TunnelConfig {
        let host: String
        let user: String
        let remotePort: Int
        let localPort: Int
        let identityFile: String?
        let sshOptions: [String]

        init(
            host: String,
            user: String = "root",
            remotePort: Int = 3000,
            localPort: Int = 3001,
            identityFile: String? = nil,
            sshOptions: [String] = ["-o", "StrictHostKeyChecking=no", "-o", "ServerAliveInterval=30"]
        ) {
            self.host = host
            self.user = user
            self.remotePort = remotePort
            self.localPort = localPort
            self.identityFile = identityFile
            self.sshOptions = sshOptions
        }
    }

    /// Connect to a remote gateway via SSH tunnel
    func connect(config: TunnelConfig) async throws {
        if isConnected {
            await disconnect()
        }

        logger.info("tunnel connecting to \(config.user)@\(config.host)")

        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/ssh")

        var arguments: [String] = []

        // SSH options
        arguments.append(contentsOf: config.sshOptions)

        // Identity file if provided
        if let identityFile = config.identityFile {
            arguments.append("-i")
            arguments.append(identityFile)
        }

        // Port forwarding
        arguments.append("-L")
        arguments.append("\(config.localPort):localhost:\(config.remotePort)")

        // Don't execute remote command, just forward
        arguments.append("-N")

        // User@host
        arguments.append("\(config.user)@\(config.host)")

        process.arguments = arguments

        // Capture output for monitoring
        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = pipe
        outputPipe = pipe

        // Handle termination
        process.terminationHandler = { [weak self] proc in
            Task { @MainActor in
                self?.handleTermination(exitCode: proc.terminationStatus)
            }
        }

        do {
            try process.run()
            tunnelProcess = process

            // Wait briefly to check if connection established
            try await Task.sleep(nanoseconds: 1_000_000_000) // 1 second

            if process.isRunning {
                isConnected = true
                remoteHost = config.host
                localPort = config.localPort
                error = nil
                logger.info("tunnel connected host=\(config.host) localPort=\(config.localPort)")
            } else {
                throw TunnelError.connectionFailed("SSH process terminated early")
            }
        } catch {
            self.error = error
            logger.error("tunnel connection failed: \(error.localizedDescription)")
            throw error
        }
    }

    /// Disconnect the current tunnel
    func disconnect() async {
        guard let process = tunnelProcess else { return }

        logger.info("tunnel disconnecting")

        if process.isRunning {
            process.terminate()
            // Give it time to clean up
            try? await Task.sleep(nanoseconds: 500_000_000)
            if process.isRunning {
                process.interrupt()
            }
        }

        tunnelProcess = nil
        outputPipe = nil
        isConnected = false
        remoteHost = nil
        localPort = nil
        logger.info("tunnel disconnected")
    }

    /// Get the gateway URL (local if connected, or default)
    func gatewayURL() -> URL? {
        if isConnected, let port = localPort {
            return URL(string: "http://localhost:\(port)")
        }
        return URL(string: "http://localhost:\(GatewayEnvironment.gatewayPort())")
    }

    // MARK: - Private

    private func handleTermination(exitCode: Int32) {
        if isConnected {
            logger.warning("tunnel terminated unexpectedly exitCode=\(exitCode)")
            isConnected = false
            error = TunnelError.unexpectedDisconnect(exitCode: Int(exitCode))
        }
    }
}

enum TunnelError: LocalizedError {
    case connectionFailed(String)
    case unexpectedDisconnect(exitCode: Int)
    case alreadyConnected

    var errorDescription: String? {
        switch self {
        case .connectionFailed(let reason):
            return "Failed to establish tunnel: \(reason)"
        case .unexpectedDisconnect(let code):
            return "Tunnel disconnected unexpectedly (exit code: \(code))"
        case .alreadyConnected:
            return "Already connected to a remote tunnel"
        }
    }
}
