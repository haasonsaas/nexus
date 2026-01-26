import Foundation

enum LogLocator {
    private static var logDir: URL {
        if let override = ProcessInfo.processInfo.environment["NEXUS_LOG_DIR"], !override.isEmpty {
            return URL(fileURLWithPath: override)
        }
        return URL(fileURLWithPath: "/tmp/nexus")
    }

    private static var stdoutLog: URL {
        logDir.appendingPathComponent("nexus-stdout.log")
    }

    private static var gatewayLog: URL {
        logDir.appendingPathComponent("nexus-gateway.log")
    }

    private static func ensureLogDirExists() {
        try? FileManager.default.createDirectory(at: logDir, withIntermediateDirectories: true)
    }

    private static func modificationDate(for url: URL) -> Date {
        (try? url.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate) ?? .distantPast
    }

    /// Returns the log directory URL, creating it if needed.
    static func logsDirectory() -> URL {
        ensureLogDirExists()
        return logDir
    }

    /// Returns the newest log file under /tmp/nexus/, or nil if none exist
    static func bestLogFile() -> URL? {
        ensureLogDirExists()
        let fm = FileManager.default
        let files = (try? fm.contentsOfDirectory(
            at: logDir,
            includingPropertiesForKeys: [.contentModificationDateKey],
            options: [.skipsHiddenFiles])) ?? []

        return files
            .filter { $0.lastPathComponent.hasPrefix("nexus") && $0.pathExtension == "log" }
            .max { lhs, rhs in modificationDate(for: lhs) < modificationDate(for: rhs) }
    }

    /// Path for launchd stdout/err
    static var launchdLogPath: String {
        ensureLogDirExists()
        return stdoutLog.path
    }

    /// Path for Gateway launchd job stdout/err
    static var launchdGatewayLogPath: String {
        ensureLogDirExists()
        return gatewayLog.path
    }
}
