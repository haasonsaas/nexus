import Foundation

enum GatewayEnvironmentKind: Equatable {
    case checking
    case ok
    case missingBinary
    case error(String)
}

struct GatewayEnvironmentStatus: Equatable {
    let kind: GatewayEnvironmentKind
    let binaryPath: String?
    let message: String

    static var checking: Self {
        .init(kind: .checking, binaryPath: nil, message: "Checking...")
    }
}

struct GatewayCommandResolution {
    let status: GatewayEnvironmentStatus
    let command: [String]?
}

enum GatewayEnvironment {
    private static let defaultPort = 8080

    /// Returns the configured gateway port from environment or defaults.
    static func gatewayPort() -> Int {
        if let raw = ProcessInfo.processInfo.environment["NEXUS_GATEWAY_PORT"],
           let port = Int(raw.trimmingCharacters(in: .whitespacesAndNewlines)),
           port > 0 {
            return port
        }
        let stored = UserDefaults.standard.integer(forKey: "gatewayPort")
        return stored > 0 ? stored : defaultPort
    }

    /// Checks the gateway environment and returns status.
    static func check() -> GatewayEnvironmentStatus {
        let resolution = resolveGatewayCommand()
        return resolution.status
    }

    /// Resolves the gateway command to execute.
    static func resolveGatewayCommand() -> GatewayCommandResolution {
        let binaryPath = findNexusBinary()

        guard let binary = binaryPath else {
            return GatewayCommandResolution(
                status: GatewayEnvironmentStatus(
                    kind: .missingBinary,
                    binaryPath: nil,
                    message: "nexus binary not found in app bundle or PATH; install the CLI."),
                command: nil)
        }

        let port = gatewayPort()
        let cmd = [binary, "serve", "--port", "\(port)"]

        return GatewayCommandResolution(
            status: GatewayEnvironmentStatus(
                kind: .ok,
                binaryPath: binary,
                message: "Gateway ready at \(binary)"),
            command: cmd)
    }

    /// Finds the nexus binary in PATH or common locations.
    private static func findNexusBinary() -> String? {
        // Check environment override first
        if let envBin = ProcessInfo.processInfo.environment["NEXUS_BIN"] {
            let trimmed = envBin.trimmingCharacters(in: .whitespacesAndNewlines)
            if FileManager.default.isExecutableFile(atPath: trimmed) {
                return trimmed
            }
        }

        if let bundled = BundledBinaryLocator.path(for: "nexus") {
            return bundled
        }

        // Search in PATH
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
}
