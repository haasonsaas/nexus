import AppKit
import Foundation
import OSLog

/// Coordinates node mode operation where the Mac acts as a controlled agent.
/// Enables remote control and command execution from a central gateway.
@MainActor
@Observable
final class NodeModeCoordinator {
    static let shared = NodeModeCoordinator()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "node.mode")

    private(set) var isEnabled = false
    private(set) var isConnected = false
    private(set) var nodeId: String?
    private(set) var controllerHost: String?
    private(set) var lastCommandAt: Date?
    private(set) var commandCount: Int = 0

    private var commandHandlers: [String: (NodeCommand) async throws -> NodeCommandResult] = [:]

    struct NodeCommand: Codable {
        let id: String
        let type: String
        let payload: [String: AnyCodable]
        let timestamp: Date
    }

    struct NodeCommandResult: Codable {
        let commandId: String
        let success: Bool
        let result: AnyCodable?
        let error: String?
    }

    // MARK: - Lifecycle

    /// Start node mode
    func start() {
        guard !isEnabled else { return }

        registerDefaultHandlers()

        Task {
            await connectToController()
        }

        isEnabled = true
        logger.info("node mode started")
    }

    /// Stop node mode
    func stop() {
        isEnabled = false
        isConnected = false
        nodeId = nil
        controllerHost = nil
        logger.info("node mode stopped")
    }

    // MARK: - Command Handling

    /// Register a command handler
    func registerHandler(for commandType: String, handler: @escaping (NodeCommand) async throws -> NodeCommandResult) {
        commandHandlers[commandType] = handler
        logger.debug("registered handler for command type=\(commandType)")
    }

    /// Process an incoming command
    func processCommand(_ command: NodeCommand) async -> NodeCommandResult {
        logger.info("processing command type=\(command.type) id=\(command.id)")

        lastCommandAt = Date()
        commandCount += 1

        guard let handler = commandHandlers[command.type] else {
            return NodeCommandResult(
                commandId: command.id,
                success: false,
                result: nil,
                error: "Unknown command type: \(command.type)"
            )
        }

        do {
            let result = try await handler(command)
            logger.debug("command completed id=\(command.id)")
            return result
        } catch {
            logger.error("command failed id=\(command.id) error=\(error.localizedDescription)")
            return NodeCommandResult(
                commandId: command.id,
                success: false,
                result: nil,
                error: error.localizedDescription
            )
        }
    }

    // MARK: - Private

    private func connectToController() async {
        // Register with gateway as a controllable node
        do {
            let deviceInfo = collectDeviceInfo()
            let data = try await ControlChannel.shared.request(
                method: "node.register",
                params: deviceInfo
            )

            let response = try JSONDecoder().decode(NodeRegistrationResponse.self, from: data)
            nodeId = response.nodeId
            controllerHost = response.controllerHost
            isConnected = true

            logger.info("registered as node id=\(response.nodeId)")

            // Start listening for commands
            startCommandListener()
        } catch {
            logger.error("failed to register as node: \(error.localizedDescription)")
            isConnected = false
        }
    }

    private func startCommandListener() {
        // Subscribe to node commands via gateway push
        Task {
            // This would hook into the gateway's push notification system
            // For now, we'll poll or use the existing ControlChannel subscription
        }
    }

    private func collectDeviceInfo() -> [String: AnyHashable] {
        let processInfo = ProcessInfo.processInfo
        let host = Host.current()

        return [
            "hostname": host.localizedName ?? "Unknown",
            "platform": "macos",
            "osVersion": processInfo.operatingSystemVersionString,
            "architecture": getArchitecture(),
            "capabilities": ["screen", "keyboard", "mouse", "shell", "files"],
            "appVersion": Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "0.0.0"
        ]
    }

    private func getArchitecture() -> String {
        #if arch(arm64)
        return "arm64"
        #else
        return "x86_64"
        #endif
    }

    private func registerDefaultHandlers() {
        // Screen capture
        registerHandler(for: "screen.capture") { command in
            let result = try await ScreenCaptureService.shared.capture()
            return NodeCommandResult(
                commandId: command.id,
                success: true,
                result: AnyCodable(["data": result.data.base64EncodedString()]),
                error: nil
            )
        }

        // Mouse click
        registerHandler(for: "mouse.click") { command in
            let x = command.payload["x"]?.value as? Int ?? 0
            let y = command.payload["y"]?.value as? Int ?? 0
            await MouseController.shared.clickAt(x: CGFloat(x), y: CGFloat(y))
            return NodeCommandResult(commandId: command.id, success: true, result: nil, error: nil)
        }

        // Keyboard type
        registerHandler(for: "keyboard.type") { command in
            guard let text = command.payload["text"]?.value as? String else {
                throw NodeModeError.invalidPayload("missing text")
            }
            await KeyboardController.shared.type(text)
            return NodeCommandResult(commandId: command.id, success: true, result: nil, error: nil)
        }

        // Shell execute
        registerHandler(for: "shell.execute") { command in
            guard let cmd = command.payload["command"]?.value as? String else {
                throw NodeModeError.invalidPayload("missing command")
            }

            let process = Process()
            let pipe = Pipe()

            process.executableURL = URL(fileURLWithPath: "/bin/bash")
            process.arguments = ["-c", cmd]
            process.standardOutput = pipe
            process.standardError = pipe

            try process.run()
            process.waitUntilExit()

            let data = pipe.fileHandleForReading.readDataToEndOfFile()
            let output = String(data: data, encoding: .utf8) ?? ""

            return NodeCommandResult(
                commandId: command.id,
                success: process.terminationStatus == 0,
                result: AnyCodable(["output": output, "exitCode": process.terminationStatus]),
                error: nil
            )
        }

        // Get system info
        registerHandler(for: "system.info") { command in
            let info = SystemIntegration.shared.systemStatus
            return NodeCommandResult(
                commandId: command.id,
                success: true,
                result: AnyCodable([
                    "cpuUsage": info.cpuUsage,
                    "memoryUsage": info.memoryUsage,
                    "batteryLevel": info.batteryLevel ?? -1,
                    "isOnBattery": info.isOnBattery
                ]),
                error: nil
            )
        }
    }
}

private struct NodeRegistrationResponse: Codable {
    let nodeId: String
    let controllerHost: String?
}

enum NodeModeError: LocalizedError {
    case invalidPayload(String)
    case notConnected
    case commandFailed(String)

    var errorDescription: String? {
        switch self {
        case .invalidPayload(let reason):
            return "Invalid command payload: \(reason)"
        case .notConnected:
            return "Not connected to controller"
        case .commandFailed(let reason):
            return "Command failed: \(reason)"
        }
    }
}
