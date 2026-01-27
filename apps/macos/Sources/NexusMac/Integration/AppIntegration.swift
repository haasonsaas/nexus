import AppKit
import Foundation
import OSLog

/// Integrates with macOS applications for enhanced agent capabilities.
/// Provides app-specific context and actions.
@MainActor
@Observable
final class AppIntegration {
    static let shared = AppIntegration()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "app.integration")

    private(set) var supportedApps: [AppProfile] = []
    private(set) var activeIntegration: AppProfile?

    struct AppProfile: Identifiable, Codable {
        let id: String
        let bundleId: String
        let name: String
        let capabilities: [Capability]
        let contextExtractors: [String]

        enum Capability: String, Codable {
            case textExtraction
            case documentAccess
            case browserIntegration
            case terminalIntegration
            case ideIntegration
            case codeNavigation
            case fileSystem
        }
    }

    init() {
        registerBuiltInProfiles()
    }

    // MARK: - App Detection

    /// Detect and activate integration for frontmost app
    func detectActiveApp() {
        guard let frontApp = NSWorkspace.shared.frontmostApplication,
              let bundleId = frontApp.bundleIdentifier else {
            activeIntegration = nil
            return
        }

        if let profile = supportedApps.first(where: { $0.bundleId == bundleId }) {
            activeIntegration = profile
            logger.debug("integration activated app=\(profile.name)")
        } else {
            activeIntegration = nil
        }
    }

    /// Check if an app is supported
    func isSupported(bundleId: String) -> Bool {
        supportedApps.contains { $0.bundleId == bundleId }
    }

    // MARK: - App Actions

    /// Open a URL or file path
    func open(url: URL) async throws {
        let config = NSWorkspace.OpenConfiguration()
        try await NSWorkspace.shared.open(url, configuration: config)
        logger.debug("opened url=\(url.absoluteString)")
    }

    /// Open a file with a specific application
    func open(file: URL, with bundleId: String) async throws {
        guard let appURL = NSWorkspace.shared.urlForApplication(withBundleIdentifier: bundleId) else {
            throw IntegrationError.appNotFound(bundleId)
        }

        let config = NSWorkspace.OpenConfiguration()
        try await NSWorkspace.shared.open([file], withApplicationAt: appURL, configuration: config)
        logger.debug("opened file=\(file.path) with=\(bundleId)")
    }

    /// Launch an application
    func launch(bundleId: String) async throws {
        guard let appURL = NSWorkspace.shared.urlForApplication(withBundleIdentifier: bundleId) else {
            throw IntegrationError.appNotFound(bundleId)
        }

        let config = NSWorkspace.OpenConfiguration()
        config.activates = true
        try await NSWorkspace.shared.openApplication(at: appURL, configuration: config)
        logger.debug("launched app=\(bundleId)")
    }

    /// Activate an already running application
    func activate(bundleId: String) -> Bool {
        guard let app = NSRunningApplication.runningApplications(withBundleIdentifier: bundleId).first else {
            return false
        }
        return app.activate(options: [.activateAllWindows])
    }

    /// Get running applications
    func runningApps() -> [RunningAppInfo] {
        NSWorkspace.shared.runningApplications.compactMap { app in
            guard let bundleId = app.bundleIdentifier,
                  let name = app.localizedName else { return nil }
            return RunningAppInfo(
                bundleId: bundleId,
                name: name,
                isActive: app.isActive,
                isHidden: app.isHidden,
                pid: app.processIdentifier
            )
        }
    }

    struct RunningAppInfo: Identifiable {
        let bundleId: String
        let name: String
        let isActive: Bool
        let isHidden: Bool
        let pid: pid_t

        var id: String { bundleId }
    }

    // MARK: - Browser Integration

    /// Get current browser URL (for supported browsers)
    func getBrowserURL() async throws -> URL? {
        guard let activeProfile = activeIntegration,
              activeProfile.capabilities.contains(.browserIntegration) else {
            throw IntegrationError.unsupportedCapability("browserIntegration")
        }

        // Use AppleScript to get URL from Safari or Chrome
        let script: String
        switch activeProfile.bundleId {
        case "com.apple.Safari":
            script = "tell application \"Safari\" to return URL of front document"
        case "com.google.Chrome":
            script = "tell application \"Google Chrome\" to return URL of active tab of front window"
        case "com.brave.Browser":
            script = "tell application \"Brave Browser\" to return URL of active tab of front window"
        case "com.microsoft.edgemac":
            script = "tell application \"Microsoft Edge\" to return URL of active tab of front window"
        default:
            throw IntegrationError.unsupportedBrowser(activeProfile.bundleId)
        }

        let result = try await runAppleScript(script)
        return URL(string: result)
    }

    /// Get current browser page title
    func getBrowserTitle() async throws -> String? {
        guard let activeProfile = activeIntegration,
              activeProfile.capabilities.contains(.browserIntegration) else {
            throw IntegrationError.unsupportedCapability("browserIntegration")
        }

        let script: String
        switch activeProfile.bundleId {
        case "com.apple.Safari":
            script = "tell application \"Safari\" to return name of front document"
        case "com.google.Chrome":
            script = "tell application \"Google Chrome\" to return title of active tab of front window"
        case "com.brave.Browser":
            script = "tell application \"Brave Browser\" to return title of active tab of front window"
        default:
            throw IntegrationError.unsupportedBrowser(activeProfile.bundleId)
        }

        return try await runAppleScript(script)
    }

    // MARK: - Terminal Integration

    /// Run command in terminal (for supported terminals)
    func runInTerminal(command: String) async throws {
        guard let activeProfile = activeIntegration,
              activeProfile.capabilities.contains(.terminalIntegration) else {
            // Default to Terminal.app
            let script = """
            tell application "Terminal"
                activate
                do script "\(command.replacingOccurrences(of: "\"", with: "\\\""))"
            end tell
            """
            _ = try await runAppleScript(script)
            return
        }

        let script: String
        switch activeProfile.bundleId {
        case "com.apple.Terminal":
            script = """
            tell application "Terminal"
                activate
                do script "\(command.replacingOccurrences(of: "\"", with: "\\\""))"
            end tell
            """
        case "com.googlecode.iterm2":
            script = """
            tell application "iTerm"
                activate
                tell current session of current window
                    write text "\(command.replacingOccurrences(of: "\"", with: "\\\""))"
                end tell
            end tell
            """
        default:
            throw IntegrationError.unsupportedTerminal(activeProfile.bundleId)
        }

        _ = try await runAppleScript(script)
        logger.debug("ran command in terminal")
    }

    // MARK: - Private

    private func registerBuiltInProfiles() {
        supportedApps = [
            // Browsers
            AppProfile(id: "safari", bundleId: "com.apple.Safari", name: "Safari",
                      capabilities: [.browserIntegration, .textExtraction], contextExtractors: ["url", "title"]),
            AppProfile(id: "chrome", bundleId: "com.google.Chrome", name: "Chrome",
                      capabilities: [.browserIntegration, .textExtraction], contextExtractors: ["url", "title"]),
            AppProfile(id: "brave", bundleId: "com.brave.Browser", name: "Brave",
                      capabilities: [.browserIntegration, .textExtraction], contextExtractors: ["url", "title"]),
            AppProfile(id: "edge", bundleId: "com.microsoft.edgemac", name: "Edge",
                      capabilities: [.browserIntegration, .textExtraction], contextExtractors: ["url", "title"]),

            // Terminals
            AppProfile(id: "terminal", bundleId: "com.apple.Terminal", name: "Terminal",
                      capabilities: [.terminalIntegration, .textExtraction], contextExtractors: []),
            AppProfile(id: "iterm", bundleId: "com.googlecode.iterm2", name: "iTerm",
                      capabilities: [.terminalIntegration, .textExtraction], contextExtractors: []),

            // IDEs
            AppProfile(id: "xcode", bundleId: "com.apple.dt.Xcode", name: "Xcode",
                      capabilities: [.ideIntegration, .codeNavigation, .textExtraction], contextExtractors: ["file", "selection"]),
            AppProfile(id: "vscode", bundleId: "com.microsoft.VSCode", name: "VS Code",
                      capabilities: [.ideIntegration, .codeNavigation, .textExtraction], contextExtractors: ["file", "selection"]),
            AppProfile(id: "cursor", bundleId: "com.todesktop.230313mzl4w4u92", name: "Cursor",
                      capabilities: [.ideIntegration, .codeNavigation, .textExtraction], contextExtractors: ["file", "selection"]),

            // Editors
            AppProfile(id: "textedit", bundleId: "com.apple.TextEdit", name: "TextEdit",
                      capabilities: [.textExtraction, .documentAccess], contextExtractors: ["text"]),
            AppProfile(id: "notes", bundleId: "com.apple.Notes", name: "Notes",
                      capabilities: [.textExtraction], contextExtractors: ["text"]),

            // Finder
            AppProfile(id: "finder", bundleId: "com.apple.finder", name: "Finder",
                      capabilities: [.fileSystem], contextExtractors: ["selection", "path"])
        ]
    }

    private func runAppleScript(_ script: String) async throws -> String {
        return try await withCheckedThrowingContinuation { continuation in
            DispatchQueue.global().async {
                var error: NSDictionary?
                let appleScript = NSAppleScript(source: script)
                let result = appleScript?.executeAndReturnError(&error)

                if let error {
                    let message = error[NSAppleScript.errorMessage] as? String ?? "Unknown error"
                    continuation.resume(throwing: IntegrationError.scriptFailed(message))
                } else if let stringValue = result?.stringValue {
                    continuation.resume(returning: stringValue)
                } else {
                    continuation.resume(returning: "")
                }
            }
        }
    }
}

enum IntegrationError: LocalizedError {
    case appNotFound(String)
    case unsupportedCapability(String)
    case unsupportedBrowser(String)
    case unsupportedTerminal(String)
    case scriptFailed(String)

    var errorDescription: String? {
        switch self {
        case .appNotFound(let bundleId):
            return "Application not found: \(bundleId)"
        case .unsupportedCapability(let cap):
            return "Unsupported capability: \(cap)"
        case .unsupportedBrowser(let bundleId):
            return "Unsupported browser: \(bundleId)"
        case .unsupportedTerminal(let bundleId):
            return "Unsupported terminal: \(bundleId)"
        case .scriptFailed(let message):
            return "Script failed: \(message)"
        }
    }
}
