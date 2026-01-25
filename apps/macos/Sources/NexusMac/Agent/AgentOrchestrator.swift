import Foundation
import OSLog

/// Orchestrates multiple agents and their interactions.
/// Manages agent lifecycle, communication, and tool execution.
@MainActor
@Observable
final class AgentOrchestrator {
    static let shared = AgentOrchestrator()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "orchestrator")

    private(set) var activeAgents: [AgentInstance] = []
    private(set) var isProcessing = false

    private let toolService = ToolExecutionService.shared
    private let controlChannel = ControlChannel.shared

    struct AgentInstance: Identifiable {
        let id: String
        let type: AgentType
        var status: AgentStatus
        var currentTask: String?
        var startedAt: Date
        var lastActivityAt: Date

        enum AgentType: String, Codable {
            case computerUse = "computer_use"
            case coder = "coder"
            case researcher = "researcher"
            case assistant = "assistant"
            case custom = "custom"
        }

        enum AgentStatus: String, Codable {
            case idle
            case thinking
            case executing
            case waiting
            case completed
            case error
        }
    }

    struct AgentRequest: Codable {
        let agentId: String
        let type: String
        let action: String
        let parameters: [String: AnyCodable]?
    }

    struct AgentResponse {
        let agentId: String
        let success: Bool
        let result: Any?
        let error: String?
        let nextAction: AgentRequest?
    }

    // MARK: - Agent Lifecycle

    /// Spawn a new agent instance
    func spawn(type: AgentInstance.AgentType, task: String? = nil) -> AgentInstance {
        let agent = AgentInstance(
            id: UUID().uuidString,
            type: type,
            status: .idle,
            currentTask: task,
            startedAt: Date(),
            lastActivityAt: Date()
        )

        activeAgents.append(agent)
        logger.info("agent spawned id=\(agent.id) type=\(type.rawValue)")

        return agent
    }

    /// Terminate an agent instance
    func terminate(agentId: String) {
        if let index = activeAgents.firstIndex(where: { $0.id == agentId }) {
            activeAgents.remove(at: index)
            logger.info("agent terminated id=\(agentId)")
        }
    }

    /// Update agent status
    func updateStatus(agentId: String, status: AgentInstance.AgentStatus, task: String? = nil) {
        guard let index = activeAgents.firstIndex(where: { $0.id == agentId }) else { return }

        activeAgents[index].status = status
        activeAgents[index].lastActivityAt = Date()
        if let task {
            activeAgents[index].currentTask = task
        }
    }

    // MARK: - Request Processing

    /// Process an agent request from the gateway
    func processRequest(_ request: AgentRequest) async throws -> AgentResponse {
        isProcessing = true
        defer { isProcessing = false }

        logger.info("processing agent request id=\(request.agentId) action=\(request.action)")

        // Ensure agent exists
        if activeAgents.first(where: { $0.id == request.agentId }) == nil {
            let type = AgentInstance.AgentType(rawValue: request.type) ?? .assistant
            _ = spawn(type: type)
        }

        updateStatus(agentId: request.agentId, status: .executing, task: request.action)

        do {
            let result = try await executeAction(request)
            updateStatus(agentId: request.agentId, status: .completed)

            return AgentResponse(
                agentId: request.agentId,
                success: true,
                result: result,
                error: nil,
                nextAction: nil
            )
        } catch {
            updateStatus(agentId: request.agentId, status: .error)
            logger.error("agent request failed: \(error.localizedDescription)")

            return AgentResponse(
                agentId: request.agentId,
                success: false,
                result: nil,
                error: error.localizedDescription,
                nextAction: nil
            )
        }
    }

    private func executeAction(_ request: AgentRequest) async throws -> Any? {
        switch request.action {
        // Computer use actions
        case "screenshot":
            let action = ComputerUseAction.screenshot(.init())
            let result = try await toolService.executeComputerUse(action)
            return result.output

        case "click":
            let x = request.parameters?["x"]?.value as? Int ?? 0
            let y = request.parameters?["y"]?.value as? Int ?? 0
            let button = request.parameters?["button"]?.value as? String
            let count = request.parameters?["count"]?.value as? Int
            let action = ComputerUseAction.click(x: x, y: y, button: button, count: count)
            _ = try await toolService.executeComputerUse(action)
            return nil

        case "type":
            guard let text = request.parameters?["text"]?.value as? String else {
                throw OrchestratorError.missingParameter("text")
            }
            let delay = request.parameters?["delay"]?.value as? Double
            let action = ComputerUseAction.type(text: text, delay: delay)
            _ = try await toolService.executeComputerUse(action)
            return nil

        case "key":
            guard let name = request.parameters?["key"]?.value as? String else {
                throw OrchestratorError.missingParameter("key")
            }
            let modifiers = request.parameters?["modifiers"]?.value as? [String]
            let action = ComputerUseAction.key(name: name, modifiers: modifiers)
            _ = try await toolService.executeComputerUse(action)
            return nil

        case "scroll":
            let deltaY = request.parameters?["deltaY"]?.value as? Int ?? 0
            let deltaX = request.parameters?["deltaX"]?.value as? Int
            let action = ComputerUseAction.scroll(deltaX: deltaX, deltaY: deltaY)
            _ = try await toolService.executeComputerUse(action)
            return nil

        case "drag":
            let fromX = request.parameters?["fromX"]?.value as? Int ?? 0
            let fromY = request.parameters?["fromY"]?.value as? Int ?? 0
            let toX = request.parameters?["toX"]?.value as? Int ?? 0
            let toY = request.parameters?["toY"]?.value as? Int ?? 0
            let duration = request.parameters?["duration"]?.value as? Double
            let action = ComputerUseAction.drag(fromX: fromX, fromY: fromY, toX: toX, toY: toY, duration: duration)
            _ = try await toolService.executeComputerUse(action)
            return nil

        case "list_windows":
            let action = ComputerUseAction.listWindows
            let result = try await toolService.executeComputerUse(action)
            return result.output

        case "list_displays":
            let action = ComputerUseAction.listDisplays
            let result = try await toolService.executeComputerUse(action)
            return result.output

        case "clipboard_get":
            let action = ComputerUseAction.clipboard(operation: "get", content: nil)
            let result = try await toolService.executeComputerUse(action)
            return result.output

        case "clipboard_set":
            let content = request.parameters?["content"]?.value as? String
            let action = ComputerUseAction.clipboard(operation: "set", content: content)
            _ = try await toolService.executeComputerUse(action)
            return nil

        // MCP tool execution
        case "mcp_call":
            guard let serverId = request.parameters?["serverId"]?.value as? String,
                  let toolName = request.parameters?["tool"]?.value as? String else {
                throw OrchestratorError.missingParameter("serverId or tool")
            }
            var args: [String: Any] = [:]
            if let argsDict = request.parameters?["arguments"]?.value as? [String: Any] {
                args = argsDict
            }
            let result = try await toolService.executeMCPTool(serverId: serverId, toolName: toolName, arguments: args)
            return result.output

        default:
            throw OrchestratorError.unknownAction(request.action)
        }
    }

    // MARK: - Gateway Communication

    /// Register orchestrator handlers with control channel
    func registerHandlers() {
        controlChannel.onRequest = { [weak self] method, params in
            guard method == "agent.request",
                  let self,
                  let data = try? JSONSerialization.data(withJSONObject: params),
                  let request = try? JSONDecoder().decode(AgentRequest.self, from: data) else {
                return nil
            }

            let response = try await self.processRequest(request)
            return try? JSONSerialization.data(withJSONObject: [
                "agentId": response.agentId,
                "success": response.success,
                "result": response.result ?? NSNull(),
                "error": response.error ?? NSNull()
            ])
        }

        logger.info("orchestrator handlers registered")
    }
}

enum OrchestratorError: LocalizedError {
    case missingParameter(String)
    case unknownAction(String)
    case agentNotFound(String)

    var errorDescription: String? {
        switch self {
        case .missingParameter(let name):
            return "Missing required parameter: \(name)"
        case .unknownAction(let action):
            return "Unknown action: \(action)"
        case .agentNotFound(let id):
            return "Agent not found: \(id)"
        }
    }
}

/// Type-erased codable wrapper for heterogeneous data
struct AnyCodable: Codable {
    let value: Any

    init(_ value: Any) {
        self.value = value
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()

        if let intValue = try? container.decode(Int.self) {
            value = intValue
        } else if let doubleValue = try? container.decode(Double.self) {
            value = doubleValue
        } else if let boolValue = try? container.decode(Bool.self) {
            value = boolValue
        } else if let stringValue = try? container.decode(String.self) {
            value = stringValue
        } else if let arrayValue = try? container.decode([AnyCodable].self) {
            value = arrayValue.map { $0.value }
        } else if let dictValue = try? container.decode([String: AnyCodable].self) {
            value = dictValue.mapValues { $0.value }
        } else {
            value = NSNull()
        }
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()

        switch value {
        case let intValue as Int:
            try container.encode(intValue)
        case let doubleValue as Double:
            try container.encode(doubleValue)
        case let boolValue as Bool:
            try container.encode(boolValue)
        case let stringValue as String:
            try container.encode(stringValue)
        case let arrayValue as [Any]:
            try container.encode(arrayValue.map { AnyCodable($0) })
        case let dictValue as [String: Any]:
            try container.encode(dictValue.mapValues { AnyCodable($0) })
        default:
            try container.encodeNil()
        }
    }
}
