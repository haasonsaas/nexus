import Foundation
import OSLog
import Compression

/// Log levels for diagnostics logging.
enum DiagnosticLogLevel: String, CaseIterable, Sendable {
    case debug = "DEBUG"
    case info = "INFO"
    case warning = "WARNING"
    case error = "ERROR"

    var osLogType: OSLogType {
        switch self {
        case .debug: return .debug
        case .info: return .info
        case .warning: return .default
        case .error: return .error
        }
    }
}

/// File-based diagnostics logger with rotation support.
/// Writes logs to ~/Library/Logs/Nexus/nexus-diagnostics.log with automatic rotation.
@MainActor
@Observable
final class DiagnosticsFileLogger {
    static let shared = DiagnosticsFileLogger()

    private let osLogger = Logger(subsystem: "com.nexus.mac", category: "diagnostics")
    private let fileManager = FileManager.default
    private let dateFormatter: ISO8601DateFormatter
    private let displayDateFormatter: DateFormatter

    // Configuration
    private let maxFileSizeBytes: UInt64 = 10 * 1024 * 1024 // 10MB
    private let maxBackupCount = 3

    // State
    private(set) var logFileSize: UInt64 = 0
    private(set) var lastLogTime: Date?
    private(set) var totalLogCount: Int = 0

    // MARK: - Paths

    private var logsDirectory: URL {
        fileManager.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Logs/Nexus")
    }

    private var logFilePath: URL {
        logsDirectory.appendingPathComponent("nexus-diagnostics.log")
    }

    var logFileLocation: String {
        logFilePath.path
    }

    // MARK: - Initialization

    private init() {
        dateFormatter = ISO8601DateFormatter()
        dateFormatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]

        displayDateFormatter = DateFormatter()
        displayDateFormatter.dateStyle = .medium
        displayDateFormatter.timeStyle = .medium

        ensureLogDirectoryExists()
        refreshFileStats()
    }

    // MARK: - Public API

    /// Log a message with the specified level and category.
    func log(_ message: String, level: DiagnosticLogLevel, category: String) {
        let timestamp = dateFormatter.string(from: Date())
        let line = "[\(timestamp)] [\(level.rawValue)] [\(category)] \(message)\n"

        // Also log to OSLog
        osLogger.log(level: level.osLogType, "[\(category)] \(message)")

        writeToFile(line)
        rotateIfNeeded()

        lastLogTime = Date()
        totalLogCount += 1
    }

    /// Convenience methods for different log levels.
    func debug(_ message: String, category: String = "app") {
        log(message, level: .debug, category: category)
    }

    func info(_ message: String, category: String = "app") {
        log(message, level: .info, category: category)
    }

    func warning(_ message: String, category: String = "app") {
        log(message, level: .warning, category: category)
    }

    func error(_ message: String, category: String = "app") {
        log(message, level: .error, category: category)
    }

    /// Export all log files as a zip archive.
    /// Returns the URL to the created zip file in the temporary directory.
    func exportLogs() -> URL? {
        let tempDir = fileManager.temporaryDirectory
        let exportName = "nexus-diagnostics-\(formattedExportDate()).zip"
        let exportURL = tempDir.appendingPathComponent(exportName)

        // Remove existing export if present
        try? fileManager.removeItem(at: exportURL)

        do {
            // Gather all log files
            let logFiles = gatherLogFiles()
            guard !logFiles.isEmpty else {
                osLogger.warning("No log files to export")
                return nil
            }

            // Create zip archive
            try createZipArchive(at: exportURL, containing: logFiles)

            osLogger.info("Exported logs to \(exportURL.path)")
            return exportURL
        } catch {
            osLogger.error("Failed to export logs: \(error.localizedDescription)")
            return nil
        }
    }

    /// Delete all log files.
    func clearLogs() {
        let logFiles = gatherLogFiles()
        for file in logFiles {
            try? fileManager.removeItem(at: file)
        }
        totalLogCount = 0
        refreshFileStats()
        osLogger.info("Cleared all diagnostic logs")
    }

    /// Read the last N lines from the log file.
    func tail(lines: Int) -> String {
        guard fileManager.fileExists(atPath: logFilePath.path) else {
            return ""
        }

        do {
            let content = try String(contentsOf: logFilePath, encoding: .utf8)
            let allLines = content.split(separator: "\n", omittingEmptySubsequences: false)
            let lastLines = allLines.suffix(lines)
            return lastLines.joined(separator: "\n")
        } catch {
            osLogger.error("Failed to read log file: \(error.localizedDescription)")
            return ""
        }
    }

    /// Refresh file statistics.
    func refreshFileStats() {
        do {
            if fileManager.fileExists(atPath: logFilePath.path) {
                let attributes = try fileManager.attributesOfItem(atPath: logFilePath.path)
                logFileSize = attributes[.size] as? UInt64 ?? 0
            } else {
                logFileSize = 0
            }
        } catch {
            logFileSize = 0
        }
    }

    // MARK: - System Info

    /// Gather system information for diagnostics.
    func gatherSystemInfo() -> DiagnosticSystemInfo {
        let appVersion = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "Unknown"
        let buildNumber = Bundle.main.infoDictionary?["CFBundleVersion"] as? String ?? "Unknown"

        let processInfo = ProcessInfo.processInfo
        let osVersion = processInfo.operatingSystemVersionString

        // Memory usage
        var taskInfo = mach_task_basic_info()
        var count = mach_msg_type_number_t(MemoryLayout<mach_task_basic_info>.size) / 4
        let result = withUnsafeMutablePointer(to: &taskInfo) {
            $0.withMemoryRebound(to: integer_t.self, capacity: 1) {
                task_info(mach_task_self_, task_flavor_t(MACH_TASK_BASIC_INFO), $0, &count)
            }
        }
        let memoryUsageBytes = result == KERN_SUCCESS ? taskInfo.resident_size : 0

        // Instance ID from UserDefaults or generate one
        let instanceId = UserDefaults.standard.string(forKey: "NexusInstanceID") ?? generateInstanceId()

        // Gateway status
        let gatewayStatus = getGatewayStatus()

        return DiagnosticSystemInfo(
            appVersion: "\(appVersion) (\(buildNumber))",
            macOSVersion: osVersion,
            memoryUsageBytes: memoryUsageBytes,
            memoryUsageFormatted: formatBytes(memoryUsageBytes),
            instanceId: instanceId,
            gatewayStatus: gatewayStatus,
            logFileSize: logFileSize,
            logFileSizeFormatted: formatBytes(logFileSize)
        )
    }

    /// Generate a diagnostic report as a string.
    func generateDiagnosticReport() -> String {
        let info = gatherSystemInfo()
        let timestamp = displayDateFormatter.string(from: Date())

        var report = """
        Nexus Diagnostic Report
        Generated: \(timestamp)
        ========================

        App Version: \(info.appVersion)
        macOS Version: \(info.macOSVersion)
        Instance ID: \(info.instanceId)

        Gateway Status: \(info.gatewayStatus)
        Memory Usage: \(info.memoryUsageFormatted)
        Log File Size: \(info.logFileSizeFormatted)

        Recent Logs:
        ------------
        """

        let recentLogs = tail(lines: 50)
        if recentLogs.isEmpty {
            report += "\n(No recent logs)"
        } else {
            report += "\n\(recentLogs)"
        }

        return report
    }

    // MARK: - Private Methods

    private func ensureLogDirectoryExists() {
        if !fileManager.fileExists(atPath: logsDirectory.path) {
            try? fileManager.createDirectory(at: logsDirectory, withIntermediateDirectories: true)
        }
    }

    private func writeToFile(_ line: String) {
        ensureLogDirectoryExists()

        guard let data = line.data(using: .utf8) else { return }

        if fileManager.fileExists(atPath: logFilePath.path) {
            if let handle = try? FileHandle(forWritingTo: logFilePath) {
                try? handle.seekToEnd()
                try? handle.write(contentsOf: data)
                try? handle.close()
            }
        } else {
            try? data.write(to: logFilePath)
        }

        logFileSize += UInt64(data.count)
    }

    private func rotateIfNeeded() {
        guard logFileSize >= maxFileSizeBytes else { return }

        osLogger.info("Rotating log file (size: \(self.logFileSize) bytes)")

        // Remove oldest backup if we have too many
        for i in stride(from: maxBackupCount, through: 1, by: -1) {
            let oldPath = logsDirectory.appendingPathComponent("nexus-diagnostics.\(i).log")
            let newPath = logsDirectory.appendingPathComponent("nexus-diagnostics.\(i + 1).log")

            if i == maxBackupCount {
                try? fileManager.removeItem(at: oldPath)
            } else if fileManager.fileExists(atPath: oldPath.path) {
                try? fileManager.moveItem(at: oldPath, to: newPath)
            }
        }

        // Move current log to .1
        let backupPath = logsDirectory.appendingPathComponent("nexus-diagnostics.1.log")
        try? fileManager.moveItem(at: logFilePath, to: backupPath)

        logFileSize = 0
    }

    private func gatherLogFiles() -> [URL] {
        var files: [URL] = []

        if fileManager.fileExists(atPath: logFilePath.path) {
            files.append(logFilePath)
        }

        for i in 1...maxBackupCount {
            let backupPath = logsDirectory.appendingPathComponent("nexus-diagnostics.\(i).log")
            if fileManager.fileExists(atPath: backupPath.path) {
                files.append(backupPath)
            }
        }

        return files
    }

    private func createZipArchive(at destination: URL, containing files: [URL]) throws {
        // Use Archive utility via Process for reliable zip creation
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/zip")
        process.currentDirectoryURL = logsDirectory
        process.arguments = ["-j", destination.path] + files.map { $0.lastPathComponent }

        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = pipe

        try process.run()
        process.waitUntilExit()

        guard process.terminationStatus == 0 else {
            throw NSError(
                domain: "DiagnosticsFileLogger",
                code: Int(process.terminationStatus),
                userInfo: [NSLocalizedDescriptionKey: "Failed to create zip archive"]
            )
        }
    }

    private func formattedExportDate() -> String {
        let formatter = DateFormatter()
        formatter.dateFormat = "yyyy-MM-dd-HHmmss"
        return formatter.string(from: Date())
    }

    private func formatBytes(_ bytes: UInt64) -> String {
        let formatter = ByteCountFormatter()
        formatter.countStyle = .file
        return formatter.string(fromByteCount: Int64(bytes))
    }

    private func generateInstanceId() -> String {
        let id = UUID().uuidString
        UserDefaults.standard.set(id, forKey: "NexusInstanceID")
        return id
    }

    private func getGatewayStatus() -> String {
        // Try to get gateway status from HealthStore
        let healthStore = HealthStore.shared
        switch healthStore.state {
        case .ok:
            return "Connected"
        case .degraded(let message):
            return "Degraded: \(message)"
        case .linkingNeeded:
            return "Linking Needed"
        case .unknown:
            return "Unknown"
        }
    }
}

// MARK: - DiagnosticSystemInfo

struct DiagnosticSystemInfo: Sendable {
    let appVersion: String
    let macOSVersion: String
    let memoryUsageBytes: UInt64
    let memoryUsageFormatted: String
    let instanceId: String
    let gatewayStatus: String
    let logFileSize: UInt64
    let logFileSizeFormatted: String
}
