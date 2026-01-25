import Foundation

/// Generates LaunchAgent plist configuration for the Nexus Edge service.
enum LaunchAgentPlist {
    /// Service label for the edge LaunchAgent.
    static let label = "com.nexus.edge"

    /// Default paths for the LaunchAgent.
    enum Paths {
        static var plistURL: URL {
            FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent("Library/LaunchAgents/\(label).plist")
        }

        static var stdoutLog: String {
            FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent("Library/Logs/nexus-edge.log").path
        }

        static var stderrLog: String {
            FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent("Library/Logs/nexus-edge.err.log").path
        }

        static var launchAgentsDir: URL {
            FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent("Library/LaunchAgents")
        }
    }

    /// Configuration options for the LaunchAgent.
    struct Config {
        /// Full path to the nexus binary.
        var programPath: String
        /// Additional program arguments (after the binary path).
        var arguments: [String]
        /// Whether to start the service at login.
        var runAtLoad: Bool
        /// Whether to restart the service if it exits.
        var keepAlive: Bool
        /// Environment variables to set.
        var environmentVariables: [String: String]
        /// Working directory for the service.
        var workingDirectory: String?
        /// Path for stdout logging.
        var stdoutPath: String
        /// Path for stderr logging.
        var stderrPath: String

        init(
            programPath: String,
            arguments: [String] = ["edge"],
            runAtLoad: Bool = true,
            keepAlive: Bool = true,
            environmentVariables: [String: String] = [:],
            workingDirectory: String? = nil,
            stdoutPath: String = Paths.stdoutLog,
            stderrPath: String = Paths.stderrLog
        ) {
            self.programPath = programPath
            self.arguments = arguments
            self.runAtLoad = runAtLoad
            self.keepAlive = keepAlive
            self.environmentVariables = environmentVariables
            self.workingDirectory = workingDirectory
            self.stdoutPath = stdoutPath
            self.stderrPath = stderrPath
        }
    }

    /// Generates plist XML content for the given configuration.
    /// - Parameter config: The configuration for the LaunchAgent.
    /// - Returns: Plist XML string.
    static func generate(config: Config) -> String {
        var xml = """
        <?xml version="1.0" encoding="UTF-8"?>
        <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
        <plist version="1.0">
        <dict>
            <key>Label</key>
            <string>\(label)</string>
            <key>ProgramArguments</key>
            <array>
                <string>\(escapeXML(config.programPath))</string>
        """

        for arg in config.arguments {
            xml += "\n        <string>\(escapeXML(arg))</string>"
        }

        xml += """

            </array>
            <key>RunAtLoad</key>
            <\(config.runAtLoad ? "true" : "false")/>
            <key>KeepAlive</key>
            <\(config.keepAlive ? "true" : "false")/>
            <key>StandardOutPath</key>
            <string>\(escapeXML(config.stdoutPath))</string>
            <key>StandardErrorPath</key>
            <string>\(escapeXML(config.stderrPath))</string>
        """

        if let workDir = config.workingDirectory {
            xml += """

                <key>WorkingDirectory</key>
                <string>\(escapeXML(workDir))</string>
            """
        }

        if !config.environmentVariables.isEmpty {
            xml += """

                <key>EnvironmentVariables</key>
                <dict>
            """
            for (key, value) in config.environmentVariables.sorted(by: { $0.key < $1.key }) {
                xml += """

                    <key>\(escapeXML(key))</key>
                    <string>\(escapeXML(value))</string>
            """
            }
            xml += """

                </dict>
            """
        }

        xml += """

        </dict>
        </plist>
        """

        return xml
    }

    /// Writes the plist to the default location.
    /// - Parameter config: The configuration for the LaunchAgent.
    /// - Throws: If the file cannot be written.
    static func write(config: Config) throws {
        let content = generate(config: config)
        let plistURL = Paths.plistURL

        // Ensure LaunchAgents directory exists
        try FileManager.default.createDirectory(
            at: Paths.launchAgentsDir,
            withIntermediateDirectories: true
        )

        try content.write(to: plistURL, atomically: true, encoding: .utf8)
    }

    /// Removes the plist file if it exists.
    static func remove() throws {
        let plistURL = Paths.plistURL
        if FileManager.default.fileExists(atPath: plistURL.path) {
            try FileManager.default.removeItem(at: plistURL)
        }
    }

    /// Checks if the plist file exists.
    static var exists: Bool {
        FileManager.default.fileExists(atPath: Paths.plistURL.path)
    }

    // MARK: - Private

    private static func escapeXML(_ string: String) -> String {
        string
            .replacingOccurrences(of: "&", with: "&amp;")
            .replacingOccurrences(of: "<", with: "&lt;")
            .replacingOccurrences(of: ">", with: "&gt;")
            .replacingOccurrences(of: "\"", with: "&quot;")
            .replacingOccurrences(of: "'", with: "&apos;")
    }
}
