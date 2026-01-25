import Foundation
import OSLog

// MARK: - NodeServiceStatus

/// Status of the remote node service.
enum NodeServiceStatus: Equatable {
    case running
    case stopped
    case notLoaded
    case error(String)

    var displayName: String {
        switch self {
        case .running:
            return "Running"
        case .stopped:
            return "Stopped"
        case .notLoaded:
            return "Not Loaded"
        case .error(let message):
            return "Error: \(message)"
        }
    }

    var isOperational: Bool {
        switch self {
        case .running:
            return true
        default:
            return false
        }
    }
}

// MARK: - NodeServiceManager

/// Manages the remote node service via nexus-edge CLI commands.
/// Provides async methods to start, stop, and query status of the node service.
@MainActor
final class NodeServiceManager: ObservableObject {
    // MARK: - Constants

    private enum Constants {
        static let startTimeout: Double = 20.0
        static let stopTimeout: Double = 15.0
        static let statusTimeout: Double = 10.0
        static let maxMessageLength = 200
    }

    // MARK: - Published State

    @Published private(set) var currentStatus: NodeServiceStatus = .notLoaded
    @Published private(set) var isLoading: Bool = false
    @Published private(set) var lastError: String?

    // MARK: - Private

    private let logger = Logger(subsystem: "com.nexus.mac", category: "node.service")
    private let edgeBinary: String

    // MARK: - JSON Response Types

    private struct ServiceResponse: Codable {
        let ok: Bool
        let result: String?
        let message: String?
        let error: String?
        let hints: [String]?
    }

    // MARK: - Init

    init(edgeBinary: String? = nil) {
        self.edgeBinary = edgeBinary ?? ProcessInfo.processInfo.environment["NEXUS_EDGE_BIN"] ?? "nexus-edge"
    }

    // MARK: - Public Methods

    /// Starts the node service.
    /// - Returns: Error message if failed, nil on success.
    func start() async -> String? {
        logger.info("starting node service")
        isLoading = true
        defer { isLoading = false }

        let result = await runServiceCommand(
            arguments: ["service", "start", "--json"],
            timeout: Constants.startTimeout
        )

        if let error = result.error {
            lastError = error
            logger.error("failed to start node service: \(error)")
            return error
        }

        await refreshStatus()
        lastError = nil
        logger.info("node service started successfully")
        return nil
    }

    /// Stops the node service.
    /// - Returns: Error message if failed, nil on success.
    func stop() async -> String? {
        logger.info("stopping node service")
        isLoading = true
        defer { isLoading = false }

        let result = await runServiceCommand(
            arguments: ["service", "stop", "--json"],
            timeout: Constants.stopTimeout
        )

        if let error = result.error {
            lastError = error
            logger.error("failed to stop node service: \(error)")
            return error
        }

        await refreshStatus()
        lastError = nil
        logger.info("node service stopped successfully")
        return nil
    }

    /// Gets the current status of the node service.
    /// - Returns: Current service status.
    func status() async -> NodeServiceStatus {
        logger.debug("checking node service status")

        let result = await runServiceCommand(
            arguments: ["service", "status", "--json"],
            timeout: Constants.statusTimeout
        )

        let status = parseStatusFromResult(result)
        currentStatus = status
        return status
    }

    /// Refreshes the current status and updates published state.
    func refreshStatus() async {
        _ = await status()
    }

    // MARK: - Private Methods

    private struct CommandResult {
        let stdout: String
        let stderr: String
        let exitCode: Int32?
        let timedOut: Bool
        let error: String?
        let response: ServiceResponse?
    }

    private func runServiceCommand(arguments: [String], timeout: Double) async -> CommandResult {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
        process.arguments = [edgeBinary] + arguments

        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()
        process.standardOutput = stdoutPipe
        process.standardError = stderrPipe

        do {
            try process.run()
        } catch {
            let errorMessage = "Failed to start process: \(error.localizedDescription)"
            logger.error("\(errorMessage)")
            return CommandResult(
                stdout: "",
                stderr: "",
                exitCode: nil,
                timedOut: false,
                error: errorMessage,
                response: nil
            )
        }

        // Read output asynchronously
        let outTask = Task { stdoutPipe.fileHandleForReading.readToEndSafely() }
        let errTask = Task { stderrPipe.fileHandleForReading.readToEndSafely() }

        // Wait for process with timeout
        let waitTask = Task { () -> CommandResult in
            process.waitUntilExit()

            let outData = await outTask.value
            let errData = await errTask.value
            let stdout = String(data: outData, encoding: .utf8) ?? ""
            let stderr = String(data: errData, encoding: .utf8) ?? ""
            let exitCode = process.terminationStatus

            // Try to parse JSON response
            let response = parseJSONResponse(from: stdout.isEmpty ? stderr : stdout)

            // Determine error message
            var errorMessage: String?
            if exitCode != 0 {
                errorMessage = buildErrorMessage(
                    response: response,
                    stdout: stdout,
                    stderr: stderr,
                    exitCode: exitCode
                )
            }

            return CommandResult(
                stdout: stdout,
                stderr: stderr,
                exitCode: exitCode,
                timedOut: false,
                error: errorMessage,
                response: response
            )
        }

        // Apply timeout
        let nanos = UInt64(timeout * 1_000_000_000)
        let result = await withTaskGroup(of: CommandResult.self) { group in
            group.addTask { await waitTask.value }
            group.addTask {
                try? await Task.sleep(nanoseconds: nanos)
                if process.isRunning {
                    process.terminate()
                    self.logger.warning("process timed out after \(timeout)s")
                }
                // Wait for the main task to complete after termination
                _ = await waitTask.value
                return CommandResult(
                    stdout: "",
                    stderr: "",
                    exitCode: nil,
                    timedOut: true,
                    error: "Operation timed out after \(Int(timeout)) seconds",
                    response: nil
                )
            }
            let first = await group.next()!
            group.cancelAll()
            return first
        }

        return result
    }

    private func parseJSONResponse(from output: String) -> ServiceResponse? {
        // Find JSON in output (may have non-JSON prefix/suffix)
        guard let jsonStart = output.firstIndex(of: "{"),
              let jsonEnd = output.lastIndex(of: "}") else {
            return nil
        }

        let jsonString = String(output[jsonStart...jsonEnd])
        guard let jsonData = jsonString.data(using: .utf8) else {
            return nil
        }

        do {
            return try JSONDecoder().decode(ServiceResponse.self, from: jsonData)
        } catch {
            logger.debug("failed to parse JSON response: \(error.localizedDescription)")
            return nil
        }
    }

    private func buildErrorMessage(
        response: ServiceResponse?,
        stdout: String,
        stderr: String,
        exitCode: Int32
    ) -> String {
        // Prefer structured error from JSON response
        if let response = response {
            var message = response.error ?? response.message ?? "Unknown error"

            // Merge hints into error message
            if let hints = response.hints, !hints.isEmpty {
                let hintsText = hints.joined(separator: "; ")
                message = "\(message) (\(hintsText))"
            }

            return summarize(message)
        }

        // Fall back to raw output
        let rawOutput = stderr.isEmpty ? stdout : stderr
        if !rawOutput.isEmpty {
            return summarize(rawOutput.trimmingCharacters(in: .whitespacesAndNewlines))
        }

        return "Command failed with exit code \(exitCode)"
    }

    private func parseStatusFromResult(_ result: CommandResult) -> NodeServiceStatus {
        // Check for timeout
        if result.timedOut {
            return .error("Status check timed out")
        }

        // Check for process errors
        if let error = result.error, result.response == nil {
            return .error(summarize(error))
        }

        // Parse from JSON response
        if let response = result.response {
            if response.ok {
                let statusResult = response.result?.lowercased() ?? ""
                switch statusResult {
                case "running", "active":
                    return .running
                case "stopped", "inactive":
                    return .stopped
                case "not_loaded", "notloaded", "not loaded":
                    return .notLoaded
                default:
                    // Check message field as fallback
                    if let message = response.message?.lowercased() {
                        if message.contains("running") {
                            return .running
                        } else if message.contains("stopped") {
                            return .stopped
                        } else if message.contains("not loaded") || message.contains("not_loaded") {
                            return .notLoaded
                        }
                    }
                    return .running // Assume running if ok=true with unknown result
                }
            } else {
                let errorMessage = response.error ?? response.message ?? "Unknown error"
                var fullMessage = errorMessage

                // Merge hints
                if let hints = response.hints, !hints.isEmpty {
                    fullMessage = "\(errorMessage) (\(hints.joined(separator: "; ")))"
                }

                // Detect not loaded state from error
                if fullMessage.lowercased().contains("not loaded") ||
                   fullMessage.lowercased().contains("not_loaded") {
                    return .notLoaded
                }

                return .error(summarize(fullMessage))
            }
        }

        // Parse from exit code
        if let exitCode = result.exitCode {
            switch exitCode {
            case 0:
                return .running
            case 1:
                return .stopped
            case 2:
                return .notLoaded
            default:
                let output = result.stderr.isEmpty ? result.stdout : result.stderr
                return .error(summarize(output.isEmpty ? "Exit code \(exitCode)" : output))
            }
        }

        return .error("Unable to determine status")
    }

    private func summarize(_ text: String) -> String {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.count <= Constants.maxMessageLength {
            return trimmed
        }
        let truncated = String(trimmed.prefix(Constants.maxMessageLength - 3))
        return "\(truncated)..."
    }
}

// MARK: - FileHandle Extension (from ShellExecutor)

private extension FileHandle {
    /// Reads until EOF using the throwing FileHandle API and returns empty Data on failure.
    func readToEndSafely() -> Data {
        do {
            return try self.readToEnd() ?? Data()
        } catch {
            return Data()
        }
    }
}
