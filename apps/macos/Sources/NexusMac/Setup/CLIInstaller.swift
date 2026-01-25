import AppKit
import Foundation
import OSLog

/// Manages CLI tool installation for Nexus.
/// Supports multiple installation methods: direct binary copy, Homebrew, and Go install.
@MainActor
@Observable
final class CLIInstaller {
    static let shared = CLIInstaller()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "cli-installer")

    // MARK: - State

    private(set) var state: InstallationState = .notInstalled
    private(set) var isChecking = false
    private(set) var isInstalling = false
    private(set) var installProgress: String?
    private(set) var error: CLIInstallerError?

    /// Common paths where CLI might be installed
    static let searchPaths = [
        "/usr/local/bin/nexus",
        "\(NSHomeDirectory())/.local/bin/nexus",
        "/opt/homebrew/bin/nexus",
        "\(NSHomeDirectory())/go/bin/nexus",
    ]

    /// Default installation directory
    static let defaultInstallDir = "\(NSHomeDirectory())/.local/bin"

    // MARK: - Types

    enum InstallationState: Equatable {
        case notInstalled
        case installed(version: String, path: String)
        case outdated(installed: String, latest: String, path: String)
        case installing

        var isInstalled: Bool {
            switch self {
            case .installed, .outdated:
                return true
            case .notInstalled, .installing:
                return false
            }
        }

        var installedPath: String? {
            switch self {
            case .installed(_, let path), .outdated(_, _, let path):
                return path
            case .notInstalled, .installing:
                return nil
            }
        }

        var displayVersion: String? {
            switch self {
            case .installed(let version, _):
                return version
            case .outdated(let installed, _, _):
                return installed
            case .notInstalled, .installing:
                return nil
            }
        }
    }

    enum InstallMethod: String, CaseIterable, Identifiable {
        case direct = "Direct"
        case homebrew = "Homebrew"
        case goInstall = "Go Install"

        var id: String { rawValue }

        var icon: String {
            switch self {
            case .direct: return "doc.zipper"
            case .homebrew: return "mug"
            case .goInstall: return "chevron.left.forwardslash.chevron.right"
            }
        }

        var description: String {
            switch self {
            case .direct:
                return "Copy from app bundle to ~/.local/bin"
            case .homebrew:
                return "Install via Homebrew package manager"
            case .goInstall:
                return "Build from source using Go"
            }
        }

        var command: String {
            switch self {
            case .direct:
                return "Copy binary"
            case .homebrew:
                return "brew install nexus"
            case .goInstall:
                return "go install github.com/haasonsaas/nexus@latest"
            }
        }
    }

    enum CLIInstallerError: LocalizedError {
        case binaryNotInBundle
        case installDirectoryCreationFailed(String)
        case copyFailed(String)
        case permissionsFailed(String)
        case homebrewNotFound
        case homebrewInstallFailed(String)
        case goNotFound
        case goInstallFailed(String)
        case uninstallFailed(String)
        case versionCheckFailed(String)

        var errorDescription: String? {
            switch self {
            case .binaryNotInBundle:
                return "Nexus CLI binary not found in app bundle"
            case .installDirectoryCreationFailed(let msg):
                return "Failed to create install directory: \(msg)"
            case .copyFailed(let msg):
                return "Failed to copy binary: \(msg)"
            case .permissionsFailed(let msg):
                return "Failed to set permissions: \(msg)"
            case .homebrewNotFound:
                return "Homebrew is not installed"
            case .homebrewInstallFailed(let msg):
                return "Homebrew installation failed: \(msg)"
            case .goNotFound:
                return "Go is not installed"
            case .goInstallFailed(let msg):
                return "Go install failed: \(msg)"
            case .uninstallFailed(let msg):
                return "Uninstall failed: \(msg)"
            case .versionCheckFailed(let msg):
                return "Version check failed: \(msg)"
            }
        }
    }

    // MARK: - Initialization

    private init() {}

    // MARK: - Installation Check

    /// Check current installation status
    @discardableResult
    func checkInstallation() async -> InstallationState {
        isChecking = true
        defer { isChecking = false }

        logger.info("checking CLI installation")

        // Search for existing installation
        for path in Self.searchPaths {
            if FileManager.default.isExecutableFile(atPath: path) {
                logger.debug("found CLI at path=\(path)")

                // Get version
                let version = await getVersion(at: path)

                if let version {
                    // Check if outdated
                    let latestVersion = await fetchLatestVersion()
                    if let latest = latestVersion, isNewerVersion(latest, than: version) {
                        state = .outdated(installed: version, latest: latest, path: path)
                        logger.info("CLI outdated installed=\(version) latest=\(latest)")
                    } else {
                        state = .installed(version: version, path: path)
                        logger.info("CLI installed version=\(version) path=\(path)")
                    }
                } else {
                    // Found binary but couldn't get version
                    state = .installed(version: "unknown", path: path)
                    logger.warning("CLI found but version unknown path=\(path)")
                }

                return state
            }
        }

        // Also check if it's in PATH
        let whichResult = await ShellExecutor.runDetailed(command: ["which", "nexus"], timeout: 5)
        if whichResult.success, let path = whichResult.stdout.trimmingCharacters(in: .whitespacesAndNewlines).components(separatedBy: "\n").first, !path.isEmpty {
            let version = await getVersion(at: path)
            state = .installed(version: version ?? "unknown", path: path)
            logger.info("CLI found in PATH version=\(version ?? "unknown") path=\(path)")
            return state
        }

        state = .notInstalled
        logger.info("CLI not installed")
        return state
    }

    /// Get CLI version at specified path
    private func getVersion(at path: String) async -> String? {
        let result = await ShellExecutor.runDetailed(command: [path, "version"], timeout: 5)
        guard result.success else { return nil }

        // Parse version from output (e.g., "nexus version 1.2.3" or just "1.2.3")
        let output = result.stdout.trimmingCharacters(in: .whitespacesAndNewlines)
        if let match = output.range(of: #"\d+\.\d+\.\d+"#, options: .regularExpression) {
            return String(output[match])
        }
        return output.isEmpty ? nil : output
    }

    /// Fetch latest version from GitHub
    private func fetchLatestVersion() async -> String? {
        guard let url = URL(string: "https://api.github.com/repos/haasonsaas/nexus/releases/latest") else {
            return nil
        }

        var request = URLRequest(url: url)
        request.setValue("application/vnd.github.v3+json", forHTTPHeaderField: "Accept")

        do {
            let (data, response) = try await URLSession.shared.data(for: request)
            guard let httpResponse = response as? HTTPURLResponse, httpResponse.statusCode == 200 else {
                return nil
            }

            struct Release: Codable {
                let tagName: String
                enum CodingKeys: String, CodingKey {
                    case tagName = "tag_name"
                }
            }

            let release = try JSONDecoder().decode(Release.self, from: data)
            return release.tagName.trimmingCharacters(in: CharacterSet(charactersIn: "v"))
        } catch {
            logger.warning("failed to fetch latest version: \(error.localizedDescription)")
            return nil
        }
    }

    // MARK: - Installation

    /// Install CLI using specified method
    func install(method: InstallMethod) async throws {
        guard !isInstalling else { return }

        isInstalling = true
        state = .installing
        error = nil
        installProgress = "Starting installation..."

        defer {
            isInstalling = false
            installProgress = nil
        }

        logger.info("installing CLI method=\(method.rawValue)")

        do {
            switch method {
            case .direct:
                try await installDirect()
            case .homebrew:
                try await installHomebrew()
            case .goInstall:
                try await installGo()
            }

            // Verify installation
            await checkInstallation()

            if state.isInstalled {
                logger.info("CLI installation completed successfully")
            } else {
                throw CLIInstallerError.copyFailed("Installation completed but binary not found")
            }
        } catch let installerError as CLIInstallerError {
            error = installerError
            state = .notInstalled
            logger.error("CLI installation failed: \(installerError.localizedDescription)")
            throw installerError
        } catch {
            let installerError = CLIInstallerError.copyFailed(error.localizedDescription)
            self.error = installerError
            state = .notInstalled
            logger.error("CLI installation failed: \(error.localizedDescription)")
            throw installerError
        }
    }

    /// Install by copying binary from app bundle
    private func installDirect() async throws {
        installProgress = "Locating binary in app bundle..."

        // Find binary in bundle
        guard let bundlePath = Bundle.main.path(forResource: "nexus", ofType: nil) else {
            throw CLIInstallerError.binaryNotInBundle
        }

        let installDir = Self.defaultInstallDir
        let installPath = "\(installDir)/nexus"

        installProgress = "Creating install directory..."

        // Create install directory if needed
        let fm = FileManager.default
        if !fm.fileExists(atPath: installDir) {
            do {
                try fm.createDirectory(atPath: installDir, withIntermediateDirectories: true)
                logger.debug("created install directory path=\(installDir)")
            } catch {
                throw CLIInstallerError.installDirectoryCreationFailed(error.localizedDescription)
            }
        }

        installProgress = "Copying binary..."

        // Remove existing file if present
        if fm.fileExists(atPath: installPath) {
            do {
                try fm.removeItem(atPath: installPath)
            } catch {
                throw CLIInstallerError.copyFailed("Failed to remove existing binary: \(error.localizedDescription)")
            }
        }

        // Copy binary
        do {
            try fm.copyItem(atPath: bundlePath, toPath: installPath)
            logger.debug("copied binary to path=\(installPath)")
        } catch {
            throw CLIInstallerError.copyFailed(error.localizedDescription)
        }

        installProgress = "Setting permissions..."

        // Set executable permissions
        do {
            try fm.setAttributes([.posixPermissions: 0o755], ofItemAtPath: installPath)
            logger.debug("set executable permissions")
        } catch {
            throw CLIInstallerError.permissionsFailed(error.localizedDescription)
        }

        // Check if PATH needs updating
        installProgress = "Checking PATH configuration..."
        await updateShellProfile()
    }

    /// Install via Homebrew
    private func installHomebrew() async throws {
        installProgress = "Checking for Homebrew..."

        // Check if Homebrew is available
        let brewCheck = await ShellExecutor.runDetailed(command: ["which", "brew"], timeout: 5)
        guard brewCheck.success else {
            throw CLIInstallerError.homebrewNotFound
        }

        let brewPath = brewCheck.stdout.trimmingCharacters(in: .whitespacesAndNewlines)
        logger.debug("found homebrew at path=\(brewPath)")

        installProgress = "Installing via Homebrew..."

        let result = await ShellExecutor.runDetailed(
            command: [brewPath, "install", "haasonsaas/tap/nexus"],
            timeout: 300 // 5 minutes
        )

        if !result.success {
            let errorMsg = result.stderr.isEmpty ? result.stdout : result.stderr
            throw CLIInstallerError.homebrewInstallFailed(errorMsg)
        }
    }

    /// Install via Go
    private func installGo() async throws {
        installProgress = "Checking for Go..."

        // Check if Go is available
        let goCheck = await ShellExecutor.runDetailed(command: ["which", "go"], timeout: 5)
        guard goCheck.success else {
            throw CLIInstallerError.goNotFound
        }

        let goPath = goCheck.stdout.trimmingCharacters(in: .whitespacesAndNewlines)
        logger.debug("found go at path=\(goPath)")

        installProgress = "Building from source..."

        let result = await ShellExecutor.runDetailed(
            command: [goPath, "install", "github.com/haasonsaas/nexus@latest"],
            timeout: 600 // 10 minutes
        )

        if !result.success {
            let errorMsg = result.stderr.isEmpty ? result.stdout : result.stderr
            throw CLIInstallerError.goInstallFailed(errorMsg)
        }
    }

    // MARK: - Update

    /// Update to latest version
    func update() async throws {
        guard case .outdated(_, _, let path) = state else { return }

        logger.info("updating CLI")

        // Determine install method based on path
        let method: InstallMethod
        if path.contains("homebrew") || path.contains("/opt/homebrew") {
            method = .homebrew
        } else if path.contains("go/bin") {
            method = .goInstall
        } else {
            method = .direct
        }

        try await install(method: method)
    }

    // MARK: - Uninstall

    /// Uninstall CLI
    func uninstall() async throws {
        guard let path = state.installedPath else { return }

        logger.info("uninstalling CLI path=\(path)")

        // Check if it was installed via Homebrew
        if path.contains("homebrew") || path.contains("/opt/homebrew") {
            let brewCheck = await ShellExecutor.runDetailed(command: ["which", "brew"], timeout: 5)
            if brewCheck.success {
                let brewPath = brewCheck.stdout.trimmingCharacters(in: .whitespacesAndNewlines)
                let result = await ShellExecutor.runDetailed(
                    command: [brewPath, "uninstall", "nexus"],
                    timeout: 60
                )
                if !result.success {
                    throw CLIInstallerError.uninstallFailed(result.stderr)
                }
            }
        } else {
            // Direct removal
            do {
                try FileManager.default.removeItem(atPath: path)
            } catch {
                throw CLIInstallerError.uninstallFailed(error.localizedDescription)
            }
        }

        state = .notInstalled
        logger.info("CLI uninstalled")
    }

    // MARK: - Shell Integration

    /// Update shell profile to include install directory in PATH
    private func updateShellProfile() async {
        let installDir = Self.defaultInstallDir
        let shellProfiles = [
            "\(NSHomeDirectory())/.zshrc",
            "\(NSHomeDirectory())/.bashrc",
            "\(NSHomeDirectory())/.bash_profile",
        ]

        let pathExport = "export PATH=\"\(installDir):$PATH\""

        for profile in shellProfiles {
            if FileManager.default.fileExists(atPath: profile) {
                // Check if PATH is already configured
                if let content = try? String(contentsOfFile: profile, encoding: .utf8) {
                    if content.contains(installDir) {
                        logger.debug("PATH already configured in \(profile)")
                        continue
                    }
                }

                // Append PATH configuration
                do {
                    let handle = try FileHandle(forWritingTo: URL(fileURLWithPath: profile))
                    handle.seekToEndOfFile()
                    let addition = "\n# Added by Nexus\n\(pathExport)\n"
                    if let data = addition.data(using: .utf8) {
                        handle.write(data)
                    }
                    try handle.close()
                    logger.info("updated PATH in \(profile)")
                } catch {
                    logger.warning("failed to update \(profile): \(error.localizedDescription)")
                }
            }
        }
    }

    /// Get shell profile paths that may need PATH update
    func shellProfilesNeedingUpdate() -> [String] {
        let installDir = Self.defaultInstallDir
        var needsUpdate: [String] = []

        let profiles = [
            "\(NSHomeDirectory())/.zshrc",
            "\(NSHomeDirectory())/.bashrc",
        ]

        for profile in profiles {
            if FileManager.default.fileExists(atPath: profile) {
                if let content = try? String(contentsOfFile: profile, encoding: .utf8) {
                    if !content.contains(installDir) {
                        needsUpdate.append(profile)
                    }
                }
            }
        }

        return needsUpdate
    }

    // MARK: - Terminal

    /// Open Terminal.app at specified path
    func openTerminal(at path: String? = nil) {
        let script: String
        if let path {
            let dir = (path as NSString).deletingLastPathComponent
            script = """
            tell application "Terminal"
                activate
                do script "cd '\(dir)' && nexus --help"
            end tell
            """
        } else {
            script = """
            tell application "Terminal"
                activate
                do script "nexus --help"
            end tell
            """
        }

        if let appleScript = NSAppleScript(source: script) {
            var errorInfo: NSDictionary?
            appleScript.executeAndReturnError(&errorInfo)
            if let error = errorInfo {
                logger.warning("failed to open terminal: \(error)")
            }
        }
    }

    // MARK: - Availability Checks

    /// Check if Homebrew is available
    func isHomebrewAvailable() async -> Bool {
        let result = await ShellExecutor.runDetailed(command: ["which", "brew"], timeout: 5)
        return result.success
    }

    /// Check if Go is available
    func isGoAvailable() async -> Bool {
        let result = await ShellExecutor.runDetailed(command: ["which", "go"], timeout: 5)
        return result.success
    }

    /// Check if binary is in app bundle
    func isBundledBinaryAvailable() -> Bool {
        Bundle.main.path(forResource: "nexus", ofType: nil) != nil
    }

    // MARK: - Helpers

    private func isNewerVersion(_ version1: String, than version2: String) -> Bool {
        let v1 = parseVersion(version1)
        let v2 = parseVersion(version2)

        for i in 0..<max(v1.count, v2.count) {
            let c1 = i < v1.count ? v1[i] : 0
            let c2 = i < v2.count ? v2[i] : 0

            if c1 > c2 { return true }
            if c1 < c2 { return false }
        }

        return false
    }

    private func parseVersion(_ version: String) -> [Int] {
        version
            .split(separator: ".")
            .compactMap { Int($0.trimmingCharacters(in: .letters)) }
    }
}
