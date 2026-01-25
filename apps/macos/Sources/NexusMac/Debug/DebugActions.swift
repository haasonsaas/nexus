import AppKit
import Foundation

/// Centralized debug operations for the Nexus macOS app.
enum DebugActions {
    private static let verboseDefaultsKey = "nexus.debug.verboseMain"
    private static let onboardingSeenKey = "nexus.onboardingSeen"

    // MARK: - File/Folder Operations

    /// Opens the gateway log file in Finder.
    @MainActor
    static func openLog() {
        let path = LaunchAgentManager.gatewayLogPath()
        let url = URL(fileURLWithPath: path)
        guard FileManager.default.fileExists(atPath: path) else {
            showAlert(title: "Log file not found", message: path)
            return
        }
        NSWorkspace.shared.activateFileViewerSelecting([url])
    }

    /// Opens the ~/.nexus config folder in Finder.
    @MainActor
    static func openConfigFolder() {
        let url = FileManager.default
            .homeDirectoryForCurrentUser
            .appendingPathComponent(".nexus", isDirectory: true)
        if FileManager.default.fileExists(atPath: url.path) {
            NSWorkspace.shared.activateFileViewerSelecting([url])
        } else {
            NSWorkspace.shared.open(url.deletingLastPathComponent())
        }
    }

    /// Opens the session store directory in Finder.
    @MainActor
    static func openSessionStore() {
        let path = resolveSessionStorePath()
        let url = URL(fileURLWithPath: path)
        if FileManager.default.fileExists(atPath: path) {
            NSWorkspace.shared.activateFileViewerSelecting([url])
        } else {
            NSWorkspace.shared.open(url.deletingLastPathComponent())
        }
    }

    // MARK: - Gateway Operations

    /// Restarts the gateway process.
    static func restartGateway() {
        Task { @MainActor in
            GatewayProcessManager.shared.stop()
            await GatewayConnection.shared.shutdown()
            try? await Task.sleep(nanoseconds: 300_000_000)
            GatewayProcessManager.shared.setActive(true)
            Task { await ControlChannel.shared.configure() }
            Task { await HealthStore.shared.refresh() }
        }
    }

    /// Triggers an immediate health check.
    @MainActor
    static func runHealthCheckNow() async {
        await HealthStore.shared.refresh()
    }

    // MARK: - Debug Operations

    /// Sends a test notification to verify notification permissions.
    static func sendTestNotification() async {
        await MainActor.run {
            NotificationService.shared.sendNotification(
                title: "Nexus",
                body: "Test notification",
                category: .statusChange
            )
        }
    }

    /// Returns whether verbose logging is enabled for the main app.
    static var verboseLoggingEnabledMain: Bool {
        UserDefaults.standard.bool(forKey: verboseDefaultsKey)
    }

    /// Toggles verbose logging for the main app.
    /// - Returns: The new verbose logging state.
    static func toggleVerboseLoggingMain() async -> Bool {
        let newValue = !verboseLoggingEnabledMain
        UserDefaults.standard.set(newValue, forKey: verboseDefaultsKey)
        _ = try? await ControlChannel.shared.request(
            method: "system-event",
            params: ["text": AnyHashable("verbose-main:\(newValue ? "on" : "off")")]
        )
        return newValue
    }

    // MARK: - App Operations

    /// Relaunches the application.
    @MainActor
    static func restartApp() {
        let url = Bundle.main.bundleURL
        let task = Process()
        task.launchPath = "/bin/sh"
        task.arguments = ["-c", "sleep 0.2; open -n \"$1\"", "_", url.path]
        try? task.run()
        NSApp.terminate(nil)
    }

    /// Resets the onboarding state so the onboarding flow is shown again.
    @MainActor
    static func restartOnboarding() {
        UserDefaults.standard.set(false, forKey: onboardingSeenKey)
    }

    // MARK: - Private Helpers

    @MainActor
    private static func showAlert(title: String, message: String) {
        let alert = NSAlert()
        alert.messageText = title
        alert.informativeText = message
        alert.runModal()
    }

    @MainActor
    private static func resolveSessionStorePath() -> String {
        let defaultPath = FileManager.default
            .homeDirectoryForCurrentUser
            .appendingPathComponent(".nexus/sessions")
            .path

        let configURL = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".nexus/nexus.json")
        guard
            let data = try? Data(contentsOf: configURL),
            let parsed = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
            let session = parsed["session"] as? [String: Any],
            let path = session["store"] as? String,
            !path.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        else {
            return defaultPath
        }
        return path
    }
}

// MARK: - DebugActionError

enum DebugActionError: LocalizedError {
    case message(String)

    var errorDescription: String? {
        switch self {
        case let .message(text):
            text
        }
    }
}
