import Foundation
import OSLog

/// Dependency injection container for services.
/// Provides centralized access to all singletons and services.
@MainActor
final class ServiceContainer {
    static let shared = ServiceContainer()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "services")

    // MARK: - Core Services

    var coordinator: ApplicationCoordinator { ApplicationCoordinator.shared }
    var config: ConfigStore.Type { ConfigStore.self }

    // MARK: - Gateway Services

    var gateway: GatewayProcessManager { GatewayProcessManager.shared }
    var controlChannel: ControlChannel { ControlChannel.shared }
    var tunnel: RemoteTunnelManager { RemoteTunnelManager.shared }
    var health: HealthStore { HealthStore.shared }

    // MARK: - Session Services

    var sessionBridge: SessionBridge { SessionBridge.shared }
    var webChat: WebChatManager { WebChatManager.shared }

    // MARK: - Agent Services

    var orchestrator: AgentOrchestrator { AgentOrchestrator.shared }
    var toolService: ToolExecutionService { ToolExecutionService.shared }
    var contextManager: ContextManager { ContextManager.shared }

    // MARK: - Computer Use Services

    var screenCapture: ScreenCaptureService { ScreenCaptureService.shared }
    var mouse: MouseController { MouseController.shared }
    var keyboard: KeyboardController { KeyboardController.shared }

    // MARK: - Integration Services

    var appIntegration: AppIntegration { AppIntegration.shared }
    var accessibility: AccessibilityBridge { AccessibilityBridge.shared }
    var notifications: NotificationBridge { NotificationBridge.shared }
    var fileWatcher: FileSystemWatcher { FileSystemWatcher.shared }
    var clipboard: ClipboardHistory { ClipboardHistory.shared }

    // MARK: - MCP Services

    var mcpRegistry: MCPServerRegistry { MCPServerRegistry.shared }

    // MARK: - Model Services

    var modelRouter: ModelRouter { ModelRouter.shared }
    var prompts: PromptLibrary { PromptLibrary.shared }

    // MARK: - Automation Services

    var workflows: WorkflowEngine { WorkflowEngine.shared }
    var quickActions: QuickActionManager { QuickActionManager.shared }

    // MARK: - Memory Services

    var memory: ConversationMemory { ConversationMemory.shared }

    // MARK: - System Services

    var system: SystemIntegration { SystemIntegration.shared }
    var analytics: UsageAnalytics { UsageAnalytics.shared }
    var updates: UpdateChecker { UpdateChecker.shared }

    // MARK: - Search Services

    var spotlight: SpotlightIntegration { SpotlightIntegration.shared }

    // MARK: - Handoff Services

    var handoff: HandoffManager { HandoffManager.shared }

    // MARK: - Voice Services

    var voiceWake: VoiceWakeRuntime { VoiceWakeRuntime.shared }
    var talkMode: TalkModeRuntime { TalkModeRuntime.shared }

    // MARK: - Audio Services

    var micMonitor: MicLevelMonitor { MicLevelMonitor.shared }
    var audioDevices: AudioInputObserver { AudioInputObserver.shared }

    // MARK: - UI Services

    var permissions: PermissionManager { PermissionManager.shared }
    var hotkeys: GlobalHotkeyManager { GlobalHotkeyManager.shared }

    // MARK: - Security Services

    var execApprovals: ExecApprovalsService { ExecApprovalsService.shared }

    // MARK: - Presence Services

    var presence: PresenceReporter { PresenceReporter.shared }

    // MARK: - Canvas Services

    var canvas: CanvasManager { CanvasManager.shared }

    // MARK: - Service Initialization

    /// Initialize all services (called during app startup)
    func initializeAll() async {
        logger.info("initializing all services")

        // Services are singletons accessed lazily
        // Accessing them here ensures they're created

        _ = config
        _ = modelRouter
        _ = prompts
        _ = memory
        _ = analytics

        logger.info("services initialized")
    }

    /// Shutdown all services (called during app termination)
    func shutdownAll() async {
        logger.info("shutting down all services")

        // Save state
        analytics.saveAnalytics()

        // Stop monitoring
        clipboard.stopTracking()
        fileWatcher.unwatchAll()
        system.stopMonitoring()

        // Stop security services
        execApprovals.stop()
        presence.stop()

        logger.info("services shut down")
    }
}

// MARK: - Convenience Accessors

extension ServiceContainer {
    /// Quick access to services from any context
    static var services: ServiceContainer { shared }

    /// Get the control channel for gateway communication
    static var control: ControlChannel { shared.controlChannel }

    /// Get the tool execution service
    static var tools: ToolExecutionService { shared.toolService }

    /// Get the context manager
    static var context: ContextManager { shared.contextManager }

    /// Get the model router
    static var models: ModelRouter { shared.modelRouter }
}
