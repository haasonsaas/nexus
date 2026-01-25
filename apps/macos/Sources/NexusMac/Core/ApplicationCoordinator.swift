import AppKit
import Foundation
import OSLog

/// Central coordinator for application lifecycle and services.
/// Initializes and coordinates all subsystems.
@MainActor
@Observable
final class ApplicationCoordinator {
    static let shared = ApplicationCoordinator()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "coordinator")

    private(set) var isInitialized = false
    private(set) var initializationError: Error?

    enum InitializationPhase: String {
        case pending
        case config
        case permissions
        case services
        case gateway
        case ui
        case complete
        case failed
    }

    private(set) var phase: InitializationPhase = .pending

    // MARK: - Initialization

    /// Initialize all application services
    func initialize() async {
        guard !isInitialized else { return }

        logger.info("application initialization starting")
        let startTime = Date()

        do {
            // Phase 1: Load configuration
            phase = .config
            await loadConfiguration()

            // Phase 2: Check permissions
            phase = .permissions
            await checkPermissions()

            // Phase 3: Initialize services
            phase = .services
            await initializeServices()

            // Phase 4: Connect to gateway
            phase = .gateway
            await connectGateway()

            // Phase 5: Initialize UI components
            phase = .ui
            await initializeUI()

            // Complete
            phase = .complete
            isInitialized = true

            let duration = Date().timeIntervalSince(startTime)
            logger.info("application initialized in \(String(format: "%.2f", duration))s")

        } catch {
            phase = .failed
            initializationError = error
            logger.error("initialization failed: \(error.localizedDescription)")
        }
    }

    /// Shutdown all services
    func shutdown() async {
        logger.info("application shutdown starting")

        // Save state
        UsageAnalytics.shared.saveAnalytics()
        ConversationMemory.shared.loadMemory() // Persist any unsaved changes

        // Stop services
        ClipboardHistory.shared.stopTracking()
        FileSystemWatcher.shared.unwatchAll()
        SystemIntegration.shared.stopMonitoring()
        SystemIntegration.shared.allowSleep()

        // Disconnect tunnel
        await RemoteTunnelManager.shared.disconnect()

        // Stop gateway
        GatewayProcessManager.shared.stopGateway()

        logger.info("application shutdown complete")
    }

    // MARK: - Initialization Phases

    private func loadConfiguration() async {
        // Load stored configuration
        let config = await ConfigStore.load()
        logger.debug("configuration loaded")

        // Load saved data
        ModelRouter.shared.loadConfiguration()
        PromptLibrary.shared.loadPrompts()
        WorkflowEngine.shared.loadWorkflows()
        QuickActionManager.shared.loadActions()
        ConversationMemory.shared.loadMemory()
        UsageAnalytics.shared.loadAnalytics()
        GatewayEndpointStore.shared // Initialize (loads on init)
    }

    private func checkPermissions() async {
        // Check accessibility permission
        if !AccessibilityBridge.shared.checkPermission() {
            logger.warning("accessibility permission not granted")
        }

        // Request notification permission if needed
        do {
            let granted = try await NotificationBridge.shared.requestPermission()
            if !granted {
                logger.warning("notification permission not granted")
            }
        } catch {
            logger.warning("notification permission request failed: \(error.localizedDescription)")
        }
    }

    private func initializeServices() async {
        // Start monitoring services
        ClipboardHistory.shared.startTracking()
        SystemIntegration.shared.startMonitoring()
        NotificationBridge.shared.startCapturing()

        // Register agent handlers
        AgentOrchestrator.shared.registerHandlers()

        // Index existing data for Spotlight
        SpotlightIntegration.shared.indexAllConversations()
        SpotlightIntegration.shared.indexAllPrompts()

        // Track session start
        UsageAnalytics.shared.trackSessionStart()

        logger.debug("services initialized")
    }

    private func connectGateway() async {
        // Check if we should use local or remote gateway
        let endpoint = GatewayEndpointStore.shared.currentEndpoint

        if endpoint?.id == "local" {
            // Start local gateway
            await GatewayProcessManager.shared.ensureRunning()
        }

        // Connect control channel
        do {
            try await ControlChannel.shared.connect()
            logger.info("gateway connected")
        } catch {
            logger.error("gateway connection failed: \(error.localizedDescription)")
            // Don't throw - app can work partially without gateway
        }
    }

    private func initializeUI() async {
        // Detect active app for integration
        AppIntegration.shared.detectActiveApp()

        // Check for updates
        await UpdateChecker.shared.checkIfNeeded()

        logger.debug("UI initialized")
    }

    // MARK: - Runtime Services

    /// Handle app becoming active
    func handleBecameActive() {
        AppIntegration.shared.detectActiveApp()
        UsageAnalytics.shared.trackActiveMinutes(1)
    }

    /// Handle app resigning active
    func handleResignActive() {
        UsageAnalytics.shared.saveAnalytics()
    }

    /// Handle incoming URL
    func handleURL(_ url: URL) -> Bool {
        logger.info("handling URL: \(url.absoluteString)")

        // nexus://chat?id=xxx
        // nexus://prompt?id=xxx
        // nexus://action?name=xxx

        guard let components = URLComponents(url: url, resolvingAgainstBaseURL: false),
              let host = components.host else {
            return false
        }

        let params = Dictionary(
            uniqueKeysWithValues: (components.queryItems ?? []).map { ($0.name, $0.value ?? "") }
        )

        switch host {
        case "chat":
            let session = SessionBridge.shared.createSession(type: .chat)
            WebChatManager.shared.openChat(for: session.id)
            return true

        case "prompt":
            if let id = params["id"],
               let prompt = PromptLibrary.shared.prompts.first(where: { $0.id == id }) {
                // Open chat with prompt
                let session = SessionBridge.shared.createSession(type: .chat)
                WebChatManager.shared.openChat(for: session.id)
                return true
            }

        case "action":
            if let name = params["name"] {
                Task {
                    if let action = QuickActionManager.shared.actions.first(where: { $0.name.lowercased() == name.lowercased() }) {
                        await QuickActionManager.shared.execute(action)
                    }
                }
                return true
            }

        default:
            break
        }

        return false
    }

    /// Handle Handoff activity
    func handleHandoff(_ activity: NSUserActivity) -> Bool {
        return HandoffManager.shared.handleActivity(activity)
    }
}
