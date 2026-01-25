import Foundation
import Observation

/// Status of the launchd service.
enum LaunchdStatus: Equatable {
    /// Service is not installed (no plist).
    case notInstalled
    /// Plist exists but service is not loaded.
    case installed
    /// Service is loaded and running.
    case running
    /// Service is loaded but not currently running.
    case loaded
    /// Service failed with an error.
    case failed(String)

    var displayName: String {
        switch self {
        case .notInstalled:
            return "Not Installed"
        case .installed:
            return "Installed (Not Running)"
        case .running:
            return "Running"
        case .loaded:
            return "Loaded"
        case .failed(let reason):
            return "Failed: \(reason)"
        }
    }

    var isHealthy: Bool {
        switch self {
        case .running, .loaded:
            return true
        default:
            return false
        }
    }
}

/// Manages the Nexus Edge LaunchAgent service lifecycle.
@MainActor
@Observable
final class LaunchdManager {
    static let shared = LaunchdManager()

    // MARK: - Observable State

    private(set) var status: LaunchdStatus = .notInstalled
    private(set) var isOperating = false
    private(set) var lastError: String?
    private(set) var attachOnlyMode = false

    /// Whether auto-start (RunAtLoad) is enabled in the plist.
    var autoStartEnabled: Bool {
        // Read from UserDefaults as the source of truth for preference
        UserDefaults.standard.bool(forKey: "nexus.edge.autoStart")
    }

    // MARK: - Computed Properties

    /// Checks if the plist file exists.
    var isInstalled: Bool {
        LaunchAgentPlist.exists
    }

    /// Path to the stdout log file.
    var stdoutLogPath: String {
        LaunchAgentPlist.Paths.stdoutLog
    }

    /// Path to the stderr log file.
    var stderrLogPath: String {
        LaunchAgentPlist.Paths.stderrLog
    }

    // MARK: - Lifecycle Management

    /// Installs the LaunchAgent plist.
    /// - Parameters:
    ///   - autoStart: Whether the service should start at login.
    ///   - keepAlive: Whether the service should restart if it exits.
    func install(autoStart: Bool = true, keepAlive: Bool = true) async {
        guard !isOperating else { return }
        isOperating = true
        lastError = nil

        defer { isOperating = false }

        // Find the nexus binary
        guard let binaryPath = findNexusBinary() else {
            lastError = "nexus binary not found in PATH"
            status = .failed(lastError!)
            return
        }

        // Build arguments for edge mode
        var arguments = ["edge"]

        // Add attach-only flag if enabled
        if attachOnlyMode {
            arguments.append("--attach-only")
        }

        let config = LaunchAgentPlist.Config(
            programPath: binaryPath,
            arguments: arguments,
            runAtLoad: autoStart,
            keepAlive: keepAlive
        )

        do {
            // Unload existing service first if loaded
            if await Launchctl.list(label: LaunchAgentPlist.label) {
                _ = await Launchctl.unload(plist: LaunchAgentPlist.Paths.plistURL)
            }

            try LaunchAgentPlist.write(config: config)
            UserDefaults.standard.set(autoStart, forKey: "nexus.edge.autoStart")
            status = .installed
        } catch {
            lastError = "Failed to write plist: \(error.localizedDescription)"
            status = .failed(lastError!)
        }
    }

    /// Uninstalls the LaunchAgent (stops service and removes plist).
    func uninstall() async {
        guard !isOperating else { return }
        isOperating = true
        lastError = nil

        defer { isOperating = false }

        // Stop the service first
        _ = await Launchctl.stop(label: LaunchAgentPlist.label)

        // Unload from launchd
        if await Launchctl.list(label: LaunchAgentPlist.label) {
            let result = await Launchctl.unload(plist: LaunchAgentPlist.Paths.plistURL)
            if !result.success {
                // Try bootout as fallback
                _ = await Launchctl.bootout(label: LaunchAgentPlist.label)
            }
        }

        // Remove plist file
        do {
            try LaunchAgentPlist.remove()
            UserDefaults.standard.removeObject(forKey: "nexus.edge.autoStart")
            status = .notInstalled
        } catch {
            lastError = "Failed to remove plist: \(error.localizedDescription)"
        }
    }

    /// Starts the service via launchctl.
    func start() async {
        guard !isOperating else { return }
        isOperating = true
        lastError = nil

        defer { isOperating = false }

        // Ensure plist exists
        guard isInstalled else {
            lastError = "Service not installed"
            return
        }

        // Load if not already loaded
        let isLoaded = await Launchctl.list(label: LaunchAgentPlist.label)
        if !isLoaded {
            let loadResult = await Launchctl.load(plist: LaunchAgentPlist.Paths.plistURL)
            if !loadResult.success {
                lastError = "Failed to load service: \(loadResult.output)"
                status = .failed(lastError!)
                return
            }
        }

        // Start the service
        let result = await Launchctl.start(label: LaunchAgentPlist.label)
        if !result.success && !result.output.isEmpty {
            // start may return non-zero even if the service starts
            // Check if it's actually running
            try? await Task.sleep(nanoseconds: 500_000_000)
        }

        await refreshStatus()
    }

    /// Stops the service via launchctl.
    func stop() async {
        guard !isOperating else { return }
        isOperating = true
        lastError = nil

        defer { isOperating = false }

        let result = await Launchctl.stop(label: LaunchAgentPlist.label)
        if !result.success && !result.output.contains("No such process") {
            lastError = "Failed to stop service: \(result.output)"
        }

        await refreshStatus()
    }

    /// Refreshes the current status of the service.
    func refreshStatus() async {
        // Check if plist exists
        guard isInstalled else {
            status = .notInstalled
            return
        }

        // Check if loaded in launchd
        let isLoaded = await Launchctl.list(label: LaunchAgentPlist.label)
        if !isLoaded {
            status = .installed
            return
        }

        // Get detailed status
        let printResult = await Launchctl.print(label: LaunchAgentPlist.label)
        if printResult.success {
            // Parse output to determine if running
            if printResult.output.contains("state = running") {
                status = .running
            } else if printResult.output.contains("state = waiting") {
                status = .loaded
            } else {
                status = .loaded
            }
        } else {
            status = .loaded
        }
    }

    /// Returns the current status (refreshes first).
    func status() async -> LaunchdStatus {
        await refreshStatus()
        return status
    }

    // MARK: - Configuration

    /// Sets attach-only mode for the edge service.
    /// - Parameter enabled: Whether to enable attach-only mode.
    func setAttachOnlyMode(_ enabled: Bool) {
        attachOnlyMode = enabled
        UserDefaults.standard.set(enabled, forKey: "nexus.edge.attachOnly")
    }

    /// Sets whether the service should auto-start at login.
    /// - Parameter enabled: Whether to enable auto-start.
    func setAutoStart(_ enabled: Bool) async {
        guard isInstalled else { return }

        // Reinstall with new settings
        let wasRunning = status == .running || status == .loaded
        await install(autoStart: enabled, keepAlive: true)

        if wasRunning {
            await start()
        }
    }

    // MARK: - Logs

    /// Opens the stdout log file in Console.app.
    func openStdoutLog() {
        NSWorkspace.shared.open(URL(fileURLWithPath: stdoutLogPath))
    }

    /// Opens the stderr log file in Console.app.
    func openStderrLog() {
        NSWorkspace.shared.open(URL(fileURLWithPath: stderrLogPath))
    }

    /// Opens the logs directory in Finder.
    func openLogsFolder() {
        let logsDir = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Logs")
        NSWorkspace.shared.open(logsDir)
    }

    /// Reads the last N lines from the stdout log.
    /// - Parameter lines: Maximum number of lines to return.
    /// - Returns: Log content.
    func readStdoutLog(lines: Int = 100) -> String {
        readLogFile(path: stdoutLogPath, tailLines: lines)
    }

    /// Reads the last N lines from the stderr log.
    /// - Parameter lines: Maximum number of lines to return.
    /// - Returns: Log content.
    func readStderrLog(lines: Int = 100) -> String {
        readLogFile(path: stderrLogPath, tailLines: lines)
    }

    // MARK: - Private

    private init() {
        // Load attach-only preference
        attachOnlyMode = UserDefaults.standard.bool(forKey: "nexus.edge.attachOnly")

        // Initial status check
        Task {
            await refreshStatus()
        }
    }

    private func findNexusBinary() -> String? {
        // Check environment override
        if let envBin = ProcessInfo.processInfo.environment["NEXUS_BIN"] {
            let trimmed = envBin.trimmingCharacters(in: .whitespacesAndNewlines)
            if FileManager.default.isExecutableFile(atPath: trimmed) {
                return trimmed
            }
        }

        // Search PATH and common locations
        let pathDirs = (ProcessInfo.processInfo.environment["PATH"] ?? "")
            .split(separator: ":")
            .map(String.init)

        let searchPaths = pathDirs + [
            "/usr/local/bin",
            "/opt/homebrew/bin",
            NSHomeDirectory() + "/.local/bin",
            NSHomeDirectory() + "/go/bin",
        ]

        for dir in searchPaths {
            let candidate = (dir as NSString).appendingPathComponent("nexus")
            if FileManager.default.isExecutableFile(atPath: candidate) {
                return candidate
            }
        }

        return nil
    }

    private func readLogFile(path: String, tailLines: Int) -> String {
        guard FileManager.default.fileExists(atPath: path) else {
            return ""
        }

        guard let data = try? Data(contentsOf: URL(fileURLWithPath: path)) else {
            return ""
        }

        let text = String(data: data, encoding: .utf8) ?? ""
        let lines = text.components(separatedBy: .newlines)

        if lines.count <= tailLines {
            return text
        }

        return lines.suffix(tailLines).joined(separator: "\n")
    }
}
