import Foundation

// MARK: - FileHandle Safe Read Extension

extension FileHandle {
    /// Reads until EOF using the throwing FileHandle API and returns empty `Data` on failure.
    ///
    /// Important: Avoid legacy, non-throwing FileHandle read APIs (e.g. `readDataToEndOfFile()` and
    /// `availableData`). They can raise Objective-C exceptions when the handle is closed/invalid, which
    /// will abort the process.
    func readToEndSafely() -> Data {
        do {
            return try self.readToEnd() ?? Data()
        } catch {
            return Data()
        }
    }

    /// Reads up to `count` bytes using the throwing FileHandle API and returns empty `Data` on failure/EOF.
    ///
    /// Important: Use this instead of `availableData` in callbacks like `readabilityHandler` to avoid
    /// Objective-C exceptions terminating the process.
    func readSafely(upToCount count: Int) -> Data {
        do {
            return try self.read(upToCount: count) ?? Data()
        } catch {
            return Data()
        }
    }
}

// MARK: - ShellExecutor

enum ShellExecutor {
    struct ShellResult {
        var stdout: String
        var stderr: String
        var exitCode: Int?
        var timedOut: Bool
        var success: Bool
        var errorMessage: String?
    }

    struct Response {
        var ok: Bool
        var message: String?
        /// Optional payload (PNG bytes, stdout text, etc.).
        var payload: Data?

        init(ok: Bool, message: String? = nil, payload: Data? = nil) {
            self.ok = ok
            self.message = message
            self.payload = payload
        }
    }

    static func runDetailed(
        command: [String],
        cwd: String? = nil,
        env: [String: String]? = nil,
        timeout: Double? = nil
    ) async -> ShellResult {
        guard !command.isEmpty else {
            return ShellResult(
                stdout: "",
                stderr: "",
                exitCode: nil,
                timedOut: false,
                success: false,
                errorMessage: "empty command")
        }

        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
        process.arguments = command
        if let cwd { process.currentDirectoryURL = URL(fileURLWithPath: cwd) }
        if let env { process.environment = env }

        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()
        process.standardOutput = stdoutPipe
        process.standardError = stderrPipe

        do {
            try process.run()
        } catch {
            return ShellResult(
                stdout: "",
                stderr: "",
                exitCode: nil,
                timedOut: false,
                success: false,
                errorMessage: "failed to start: \(error.localizedDescription)")
        }

        let outTask = Task { stdoutPipe.fileHandleForReading.readToEndSafely() }
        let errTask = Task { stderrPipe.fileHandleForReading.readToEndSafely() }

        let waitTask = Task { () -> ShellResult in
            process.waitUntilExit()
            let out = await outTask.value
            let err = await errTask.value
            let status = Int(process.terminationStatus)
            return ShellResult(
                stdout: String(bytes: out, encoding: .utf8) ?? "",
                stderr: String(bytes: err, encoding: .utf8) ?? "",
                exitCode: status,
                timedOut: false,
                success: status == 0,
                errorMessage: status == 0 ? nil : "exit \(status)")
        }

        if let timeout, timeout > 0 {
            let nanos = UInt64(timeout * 1_000_000_000)
            let result = await withTaskGroup(of: ShellResult.self) { group in
                group.addTask { await waitTask.value }
                group.addTask {
                    try? await Task.sleep(nanoseconds: nanos)
                    if process.isRunning { process.terminate() }
                    _ = await waitTask.value  // drain pipes after termination
                    return ShellResult(
                        stdout: "",
                        stderr: "",
                        exitCode: nil,
                        timedOut: true,
                        success: false,
                        errorMessage: "timeout")
                }
                let first = await group.next()!
                group.cancelAll()
                return first
            }
            return result
        }

        return await waitTask.value
    }

    static func run(
        command: [String],
        cwd: String? = nil,
        env: [String: String]? = nil,
        timeout: Double? = nil
    ) async -> Response {
        let result = await runDetailed(command: command, cwd: cwd, env: env, timeout: timeout)
        let combined = result.stdout.isEmpty ? result.stderr : result.stdout
        let payload = combined.isEmpty ? nil : Data(combined.utf8)
        return Response(ok: result.success, message: result.errorMessage, payload: payload)
    }
}
