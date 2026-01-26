import Foundation
import Logging
import OSLog

/// Centralized logging configuration for Nexus.
/// Configures both OSLog and file-based logging.
enum NexusLogging {
    private static var isBootstrapped = false

    /// Bootstrap the logging system
    static func bootstrapIfNeeded() {
        guard !isBootstrapped else { return }
        isBootstrapped = true

        LoggingSystem.bootstrap { label in
            NexusLogHandler(label: label)
        }
    }
}

/// Custom log handler that routes to OSLog
struct NexusLogHandler: LogHandler {
    let label: String

    var metadata: Logging.Logger.Metadata = [:]
    var logLevel: Logging.Logger.Level = .info

    private let osLogger: os.Logger

    init(label: String) {
        self.label = label
        self.osLogger = os.Logger(subsystem: "com.nexus.mac", category: label)
    }

    subscript(metadataKey key: String) -> Logging.Logger.Metadata.Value? {
        get { metadata[key] }
        set { metadata[key] = newValue }
    }

    func log(
        level: Logging.Logger.Level,
        message: Logging.Logger.Message,
        metadata: Logging.Logger.Metadata?,
        source: String,
        file: String,
        function: String,
        line: UInt
    ) {
        let mergedMetadata = self.metadata.merging(metadata ?? [:]) { _, new in new }
        let metadataString = mergedMetadata.isEmpty ? "" : " \(mergedMetadata)"

        let osLogLevel: OSLogType
        switch level {
        case .trace, .debug:
            osLogLevel = .debug
        case .info, .notice:
            osLogLevel = .info
        case .warning:
            osLogLevel = .default
        case .error:
            osLogLevel = .error
        case .critical:
            osLogLevel = .fault
        }

        osLogger.log(level: osLogLevel, "\(message)\(metadataString)")

        // Also write to file if error or above
        if level >= .error {
            writeToFile(level: level, message: "\(message)\(metadataString)")
        }
    }

    private func writeToFile(level: Logging.Logger.Level, message: String) {
        let logFile = getLogFileURL()
        let timestamp = ISO8601DateFormatter().string(from: Date())
        let line = "[\(timestamp)] [\(level)] \(label): \(message)\n"

        if let data = line.data(using: .utf8) {
            if FileManager.default.fileExists(atPath: logFile.path) {
                if let handle = try? FileHandle(forWritingTo: logFile) {
                    handle.seekToEndOfFile()
                    handle.write(data)
                    handle.closeFile()
                }
            } else {
                try? data.write(to: logFile)
            }
        }
    }

    private func getLogFileURL() -> URL {
        let logsDir = FileManager.default.urls(for: .libraryDirectory, in: .userDomainMask).first!
            .appendingPathComponent("Logs/Nexus")
        try? FileManager.default.createDirectory(at: logsDir, withIntermediateDirectories: true)
        return logsDir.appendingPathComponent("nexus.log")
    }
}
