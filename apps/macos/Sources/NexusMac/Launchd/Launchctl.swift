import Foundation

/// Wrapper for launchctl commands to manage LaunchAgents.
enum Launchctl {
    /// Result of a launchctl operation.
    struct Result {
        let success: Bool
        let output: String
        let exitCode: Int32
    }

    // MARK: - Public API

    /// Loads a LaunchAgent plist file.
    /// - Parameter plist: Path to the plist file.
    /// - Returns: Result of the operation.
    static func load(plist: URL) async -> Result {
        await run(arguments: ["load", "-w", plist.path])
    }

    /// Unloads a LaunchAgent plist file.
    /// - Parameter plist: Path to the plist file.
    /// - Returns: Result of the operation.
    static func unload(plist: URL) async -> Result {
        await run(arguments: ["unload", plist.path])
    }

    /// Checks if a service with the given label is loaded.
    /// - Parameter label: The service label (e.g., "com.nexus.edge").
    /// - Returns: true if the service is loaded.
    static func list(label: String) async -> Bool {
        let result = await run(arguments: ["list", label])
        return result.success
    }

    /// Starts a service by label.
    /// - Parameter label: The service label.
    /// - Returns: Result of the operation.
    static func start(label: String) async -> Result {
        await run(arguments: ["start", label])
    }

    /// Stops a service by label.
    /// - Parameter label: The service label.
    /// - Returns: Result of the operation.
    static func stop(label: String) async -> Result {
        await run(arguments: ["stop", label])
    }

    /// Kickstarts a service (force immediate start).
    /// - Parameter label: The service label.
    /// - Returns: Result of the operation.
    static func kickstart(label: String) async -> Result {
        // Use gui domain for user LaunchAgents
        let uid = getuid()
        return await run(arguments: ["kickstart", "-k", "gui/\(uid)/\(label)"])
    }

    /// Bootstraps a service plist into the GUI domain.
    /// - Parameter plist: Path to the plist file.
    /// - Returns: Result of the operation.
    static func bootstrap(plist: URL) async -> Result {
        let uid = getuid()
        return await run(arguments: ["bootstrap", "gui/\(uid)", plist.path])
    }

    /// Bootout (remove) a service from the GUI domain.
    /// - Parameter label: The service label.
    /// - Returns: Result of the operation.
    static func bootout(label: String) async -> Result {
        let uid = getuid()
        return await run(arguments: ["bootout", "gui/\(uid)/\(label)"])
    }

    /// Prints detailed information about a service.
    /// - Parameter label: The service label.
    /// - Returns: Result containing service info in output.
    static func print(label: String) async -> Result {
        let uid = getuid()
        return await run(arguments: ["print", "gui/\(uid)/\(label)"])
    }

    // MARK: - Internal

    private static func run(arguments: [String]) async -> Result {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/launchctl")
        process.arguments = arguments

        let stdout = Pipe()
        let stderr = Pipe()
        process.standardOutput = stdout
        process.standardError = stderr

        do {
            try process.run()
            process.waitUntilExit()

            let outData = stdout.fileHandleForReading.readToEndSafely()
            let errData = stderr.fileHandleForReading.readToEndSafely()

            let outString = String(data: outData, encoding: .utf8) ?? ""
            let errString = String(data: errData, encoding: .utf8) ?? ""
            let combined = outString.isEmpty ? errString : outString

            return Result(
                success: process.terminationStatus == 0,
                output: combined.trimmingCharacters(in: .whitespacesAndNewlines),
                exitCode: process.terminationStatus
            )
        } catch {
            return Result(
                success: false,
                output: "Failed to run launchctl: \(error.localizedDescription)",
                exitCode: -1
            )
        }
    }
}

