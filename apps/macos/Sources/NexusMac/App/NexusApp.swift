import AppKit
import OSLog
import SwiftUI

/// Main application entry point for Nexus macOS client.
/// Configures the menu bar app with comprehensive AI agent capabilities.
@main
struct NexusApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate

    @State private var appState = AppStateStore.shared
    @State private var isMenuPresented = false

    private let logger = Logger(subsystem: "com.nexus.mac", category: "app")

    init() {
        // Bootstrap logging
        NexusLogging.bootstrapIfNeeded()
        logger.info("nexus app initializing")
    }

    var body: some Scene {
        // Menu bar extra (status item)
        MenuBarExtra {
            MenuContentView(
                appState: appState,
                onOpenChat: { openChat() },
                onOpenSettings: { openSettings() },
                onTogglePause: { togglePause() }
            )
        } label: {
            AnimatedStatusIcon(appState: appState)
        }
        .menuBarExtraStyle(.menu)

        // Settings window
        Settings {
            SettingsRootView(appState: appState)
        }
    }

    // MARK: - Actions

    private func openChat() {
        let session = SessionBridge.shared.createSession(type: .chat)
        WebChatManager.shared.openChat(for: session.id)
    }

    private func openSettings() {
        NSApp.sendAction(Selector(("showSettingsWindow:")), to: nil, from: nil)
    }

    private func togglePause() {
        appState.isPaused.toggle()
    }
}

/// Application delegate for lifecycle management
final class AppDelegate: NSObject, NSApplicationDelegate {
    private let logger = Logger(subsystem: "com.nexus.mac", category: "delegate")

    func applicationDidFinishLaunching(_ notification: Notification) {
        logger.info("application did finish launching")

        // Check for duplicate instance
        if isDuplicateInstance() {
            logger.warning("duplicate instance detected, terminating")
            NSApp.terminate(nil)
            return
        }

        // Apply activation policy (menu bar only by default)
        NSApp.setActivationPolicy(.accessory)

        // Initialize all services
        Task {
            await initializeServices()
        }
    }

    func applicationWillTerminate(_ notification: Notification) {
        logger.info("application will terminate")

        Task {
            await shutdownServices()
        }
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        // Open chat when dock icon is clicked (if visible)
        if !flag {
            let session = SessionBridge.shared.createSession(type: .chat)
            WebChatManager.shared.openChat(for: session.id)
        }
        return true
    }

    func application(_ application: NSApplication, open urls: [URL]) {
        for url in urls {
            _ = ApplicationCoordinator.shared.handleURL(url)
        }
    }

    func application(_ application: NSApplication, continue userActivity: NSUserActivity, restorationHandler: @escaping ([any NSUserActivityRestoring]) -> Void) -> Bool {
        return ApplicationCoordinator.shared.handleHandoff(userActivity)
    }

    // MARK: - Private

    private func isDuplicateInstance() -> Bool {
        let runningApps = NSWorkspace.shared.runningApplications
        let bundleId = Bundle.main.bundleIdentifier ?? "com.nexus.mac"
        let instances = runningApps.filter { $0.bundleIdentifier == bundleId }
        return instances.count > 1
    }

    @MainActor
    private func initializeServices() async {
        // Core services
        await ApplicationCoordinator.shared.initialize()

        // Start presence reporter
        PresenceReporter.shared.start()

        // Start node pairing if enabled
        if AppStateStore.shared.nodeModeEnabled {
            NodeModeCoordinator.shared.start()
        }

        // Load channels
        await ChannelsStore.shared.loadChannels()

        // Load cron jobs
        await CronJobsStore.shared.loadJobs()

        // Load MCP servers
        await MCPServerRegistry.shared.loadFromConfig()

        // Start voice wake if enabled
        if AppStateStore.shared.voiceWakeEnabled {
            VoiceWakeOverlayRuntime.shared.startListening()
        }

        // Wire up global hotkey manager
        GlobalHotkeyManager.shared.start()

        // Start mic level monitor for voice features
        if AppStateStore.shared.voiceWakeEnabled {
            try? await MicLevelMonitor.shared.start { _ in }
        }

        // Check for updates in background
        Task {
            await UpdateChecker.shared.checkForUpdates()
        }

        logger.info("services initialized")
    }

    @MainActor
    private func shutdownServices() async {
        // Stop voice wake
        VoiceWakeOverlayRuntime.shared.stopListening()

        // Stop global hotkeys
        GlobalHotkeyManager.shared.stop()

        // Stop mic monitor
        await MicLevelMonitor.shared.stop()

        // Stop node mode
        NodeModeCoordinator.shared.stop()

        // Stop presence reporter
        PresenceReporter.shared.stop()

        // Shutdown coordinator
        await ApplicationCoordinator.shared.shutdown()

        logger.info("services shutdown complete")
    }
}
