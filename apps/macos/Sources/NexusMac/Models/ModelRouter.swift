import Foundation
import OSLog

/// Routes requests to appropriate AI models based on task type.
/// Supports multiple providers and intelligent model selection.
@MainActor
@Observable
final class ModelRouter {
    static let shared = ModelRouter()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "model.router")

    private(set) var providers: [ModelProvider] = []
    private(set) var defaultProvider: String?
    private(set) var taskRoutes: [TaskType: ModelRoute] = [:]

    struct ModelProvider: Identifiable, Codable, Equatable {
        let id: String
        var name: String
        var apiEndpoint: String?
        var models: [Model]
        var isEnabled: Bool
        var priority: Int

        struct Model: Identifiable, Codable, Equatable {
            let id: String
            var name: String
            var capabilities: Set<Capability>
            var contextWindow: Int
            var maxOutputTokens: Int?
            var costPerInputToken: Double?
            var costPerOutputToken: Double?

            enum Capability: String, Codable {
                case text
                case vision
                case code
                case function
                case computerUse
                case reasoning
                case fast
            }
        }
    }

    struct ModelRoute: Codable {
        let providerId: String
        let modelId: String
        let priority: Int
        let fallbackRoutes: [ModelRoute]?
    }

    enum TaskType: String, Codable, CaseIterable {
        case chat
        case code
        case vision
        case computerUse
        case research
        case creative
        case fast
    }

    init() {
        registerBuiltInProviders()
        setupDefaultRoutes()
    }

    // MARK: - Provider Management

    /// Register a model provider
    func registerProvider(_ provider: ModelProvider) {
        if let index = providers.firstIndex(where: { $0.id == provider.id }) {
            providers[index] = provider
        } else {
            providers.append(provider)
        }
        providers.sort { $0.priority < $1.priority }
        persistConfiguration()
        logger.info("provider registered id=\(provider.id)")
    }

    /// Remove a provider
    func removeProvider(id: String) {
        providers.removeAll { $0.id == id }
        persistConfiguration()
    }

    /// Set default provider
    func setDefaultProvider(id: String) {
        guard providers.contains(where: { $0.id == id }) else { return }
        defaultProvider = id
        persistConfiguration()
    }

    // MARK: - Routing

    /// Get the best model for a task
    func route(for task: TaskType) -> (provider: ModelProvider, model: ModelProvider.Model)? {
        // Check if there's a specific route for this task
        if let route = taskRoutes[task] {
            if let result = resolveRoute(route) {
                return result
            }
        }

        // Fall back to default provider
        if let defaultId = defaultProvider,
           let provider = providers.first(where: { $0.id == defaultId && $0.isEnabled }),
           let model = selectModel(for: task, from: provider) {
            return (provider, model)
        }

        // Fall back to first available provider
        for provider in providers where provider.isEnabled {
            if let model = selectModel(for: task, from: provider) {
                return (provider, model)
            }
        }

        return nil
    }

    /// Set a route for a specific task
    func setRoute(_ route: ModelRoute, for task: TaskType) {
        taskRoutes[task] = route
        persistConfiguration()
        logger.debug("route set for task=\(task.rawValue)")
    }

    /// Clear route for a task (use default)
    func clearRoute(for task: TaskType) {
        taskRoutes.removeValue(forKey: task)
        persistConfiguration()
    }

    // MARK: - Model Selection

    private func resolveRoute(_ route: ModelRoute) -> (provider: ModelProvider, model: ModelProvider.Model)? {
        if let provider = providers.first(where: { $0.id == route.providerId && $0.isEnabled }),
           let model = provider.models.first(where: { $0.id == route.modelId }) {
            return (provider, model)
        }

        // Try fallbacks
        if let fallbacks = route.fallbackRoutes {
            for fallback in fallbacks {
                if let result = resolveRoute(fallback) {
                    return result
                }
            }
        }

        return nil
    }

    private func selectModel(for task: TaskType, from provider: ModelProvider) -> ModelProvider.Model? {
        let requiredCapabilities: Set<ModelProvider.Model.Capability>

        switch task {
        case .chat:
            requiredCapabilities = [.text]
        case .code:
            requiredCapabilities = [.text, .code]
        case .vision:
            requiredCapabilities = [.vision]
        case .computerUse:
            requiredCapabilities = [.computerUse]
        case .research:
            requiredCapabilities = [.text, .reasoning]
        case .creative:
            requiredCapabilities = [.text]
        case .fast:
            requiredCapabilities = [.fast]
        }

        // Find models that have all required capabilities
        let capable = provider.models.filter { model in
            requiredCapabilities.isSubset(of: model.capabilities)
        }

        // Return the one with the largest context window
        return capable.max { $0.contextWindow < $1.contextWindow }
    }

    // MARK: - Built-in Configuration

    private func registerBuiltInProviders() {
        providers = [
            ModelProvider(
                id: "anthropic",
                name: "Anthropic",
                apiEndpoint: "https://api.anthropic.com",
                models: [
                    ModelProvider.Model(
                        id: "claude-opus-4-5-20251101",
                        name: "Claude Opus 4.5",
                        capabilities: [.text, .vision, .code, .function, .computerUse, .reasoning],
                        contextWindow: 200000,
                        maxOutputTokens: 8192,
                        costPerInputToken: 0.015,
                        costPerOutputToken: 0.075
                    ),
                    ModelProvider.Model(
                        id: "claude-sonnet-4-20250514",
                        name: "Claude Sonnet 4",
                        capabilities: [.text, .vision, .code, .function, .computerUse],
                        contextWindow: 200000,
                        maxOutputTokens: 8192,
                        costPerInputToken: 0.003,
                        costPerOutputToken: 0.015
                    ),
                    ModelProvider.Model(
                        id: "claude-3-5-haiku-20241022",
                        name: "Claude 3.5 Haiku",
                        capabilities: [.text, .code, .function, .fast],
                        contextWindow: 200000,
                        maxOutputTokens: 8192,
                        costPerInputToken: 0.0008,
                        costPerOutputToken: 0.004
                    )
                ],
                isEnabled: true,
                priority: 1
            ),
            ModelProvider(
                id: "openai",
                name: "OpenAI",
                apiEndpoint: "https://api.openai.com/v1",
                models: [
                    ModelProvider.Model(
                        id: "gpt-4o",
                        name: "GPT-4o",
                        capabilities: [.text, .vision, .code, .function],
                        contextWindow: 128000,
                        maxOutputTokens: 4096,
                        costPerInputToken: 0.005,
                        costPerOutputToken: 0.015
                    ),
                    ModelProvider.Model(
                        id: "gpt-4o-mini",
                        name: "GPT-4o Mini",
                        capabilities: [.text, .code, .function, .fast],
                        contextWindow: 128000,
                        maxOutputTokens: 4096,
                        costPerInputToken: 0.00015,
                        costPerOutputToken: 0.0006
                    )
                ],
                isEnabled: true,
                priority: 2
            ),
            ModelProvider(
                id: "ollama",
                name: "Ollama (Local)",
                apiEndpoint: "http://localhost:11434",
                models: [
                    ModelProvider.Model(
                        id: "llama3.2",
                        name: "Llama 3.2",
                        capabilities: [.text, .code, .fast],
                        contextWindow: 8192,
                        maxOutputTokens: nil,
                        costPerInputToken: 0,
                        costPerOutputToken: 0
                    )
                ],
                isEnabled: false,
                priority: 10
            )
        ]
    }

    private func setupDefaultRoutes() {
        defaultProvider = "anthropic"

        taskRoutes = [
            .computerUse: ModelRoute(providerId: "anthropic", modelId: "claude-sonnet-4-20250514", priority: 1, fallbackRoutes: nil),
            .code: ModelRoute(providerId: "anthropic", modelId: "claude-opus-4-5-20251101", priority: 1, fallbackRoutes: nil),
            .fast: ModelRoute(providerId: "anthropic", modelId: "claude-3-5-haiku-20241022", priority: 1, fallbackRoutes: nil)
        ]
    }

    // MARK: - Persistence

    private func persistConfiguration() {
        let url = configFileURL()
        let config = RouterConfig(
            providers: providers,
            defaultProvider: defaultProvider,
            taskRoutes: taskRoutes
        )
        do {
            let data = try JSONEncoder().encode(config)
            try data.write(to: url)
        } catch {
            logger.error("failed to persist router config: \(error.localizedDescription)")
        }
    }

    func loadConfiguration() {
        let url = configFileURL()
        guard FileManager.default.fileExists(atPath: url.path),
              let data = try? Data(contentsOf: url),
              let config = try? JSONDecoder().decode(RouterConfig.self, from: data) else {
            return
        }
        providers = config.providers
        defaultProvider = config.defaultProvider
        taskRoutes = config.taskRoutes
        logger.debug("router config loaded")
    }

    private func configFileURL() -> URL {
        let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        let nexusDir = appSupport.appendingPathComponent("Nexus")
        try? FileManager.default.createDirectory(at: nexusDir, withIntermediateDirectories: true)
        return nexusDir.appendingPathComponent("model_router.json")
    }

    struct RouterConfig: Codable {
        let providers: [ModelProvider]
        let defaultProvider: String?
        let taskRoutes: [TaskType: ModelRoute]
    }
}
