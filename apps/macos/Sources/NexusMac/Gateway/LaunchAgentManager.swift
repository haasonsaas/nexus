import Foundation

let gatewayLaunchdLabel = "com.haasonsaas.nexus-gateway"

enum LaunchAgentManager {
    private static var plistURL: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/LaunchAgents/\(gatewayLaunchdLabel).plist")
    }

    private static var logPath: String {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Logs/nexus-gateway.log").path
    }

    /// Returns the path to the gateway log file.
    static func gatewayLogPath() -> String {
        logPath
    }

    /// Checks if the launch agent is currently loaded.
    static func isLoaded() async -> Bool {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/launchctl")
        process.arguments = ["list", gatewayLaunchdLabel]

        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = pipe

        do {
            try process.run()
            process.waitUntilExit()
            return process.terminationStatus == 0
        } catch {
            return false
        }
    }

    /// Enables or disables the launch agent.
    /// - Parameters:
    ///   - enabled: Whether to enable or disable the agent
    ///   - bundlePath: Path to the app bundle (unused but kept for API compatibility)
    ///   - port: The port for the gateway to listen on
    /// - Returns: An error message if the operation failed, nil on success.
    static func set(enabled: Bool, bundlePath: String, port: Int) async -> String? {
        if enabled {
            return await enable(port: port)
        } else {
            return await disable()
        }
    }

    private static func enable(port: Int) async -> String? {
        let resolution = GatewayEnvironment.resolveGatewayCommand()
        guard let command = resolution.command, !command.isEmpty else {
            return resolution.status.message
        }

        // Generate and write plist
        let plist = generatePlist(command: command, port: port)
        do {
            let launchAgentsDir = plistURL.deletingLastPathComponent()
            try FileManager.default.createDirectory(at: launchAgentsDir, withIntermediateDirectories: true)
            try plist.write(to: plistURL, atomically: true, encoding: .utf8)
        } catch {
            return "Failed to write plist: \(error.localizedDescription)"
        }

        // Load the agent
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/launchctl")
        process.arguments = ["load", "-w", plistURL.path]

        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = pipe

        do {
            try process.run()
            process.waitUntilExit()
            if process.terminationStatus != 0 {
                let data = pipe.fileHandleForReading.readDataToEndOfFile()
                let output = String(data: data, encoding: .utf8) ?? "Unknown error"
                return "launchctl load failed: \(output)"
            }
            return nil
        } catch {
            return "Failed to run launchctl: \(error.localizedDescription)"
        }
    }

    private static func disable() async -> String? {
        guard FileManager.default.fileExists(atPath: plistURL.path) else {
            return nil
        }

        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/launchctl")
        process.arguments = ["unload", plistURL.path]

        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = pipe

        do {
            try process.run()
            process.waitUntilExit()
            // Remove plist file
            try? FileManager.default.removeItem(at: plistURL)
            return nil
        } catch {
            return "Failed to unload agent: \(error.localizedDescription)"
        }
    }

    private static func generatePlist(command: [String], port: Int) -> String {
        let program = command[0]
        let args = command.dropFirst().map { "<string>\($0)</string>" }.joined(separator: "\n            ")

        return """
        <?xml version="1.0" encoding="UTF-8"?>
        <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
        <plist version="1.0">
        <dict>
            <key>Label</key>
            <string>\(gatewayLaunchdLabel)</string>
            <key>ProgramArguments</key>
            <array>
                <string>\(program)</string>
                \(args)
            </array>
            <key>RunAtLoad</key>
            <true/>
            <key>KeepAlive</key>
            <true/>
            <key>StandardOutPath</key>
            <string>\(logPath)</string>
            <key>StandardErrorPath</key>
            <string>\(logPath)</string>
            <key>EnvironmentVariables</key>
            <dict>
                <key>NEXUS_GATEWAY_PORT</key>
                <string>\(port)</string>
            </dict>
        </dict>
        </plist>
        """
    }
}
