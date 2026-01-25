import Foundation
import OSLog

/// Registry for MCP (Model Context Protocol) servers.
/// Enables integration with the MCP ecosystem for tool extensions.
@MainActor
@Observable
final class MCPServerRegistry {
    static let shared = MCPServerRegistry()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "mcp")

    private(set) var servers: [MCPServerInfo] = []
    private(set) var isLoading = false

    struct MCPServerInfo: Identifiable, Codable, Equatable {
        let id: String
        let name: String
        let description: String?
        let command: String
        let args: [String]?
        let env: [String: String]?
        var enabled: Bool
        var status: ServerStatus

        enum ServerStatus: String, Codable {
            case stopped
            case starting
            case running
            case error
        }
    }

    /// Load MCP server configurations from config
    func loadFromConfig() async {
        isLoading = true
        defer { isLoading = false }

        let config = await ConfigStore.load()
        guard let mcpConfig = config["mcp"] as? [String: Any],
              let serversConfig = mcpConfig["servers"] as? [[String: Any]] else {
            logger.debug("mcp no servers configured")
            return
        }

        var loadedServers: [MCPServerInfo] = []
        for serverConfig in serversConfig {
            guard let id = serverConfig["id"] as? String,
                  let name = serverConfig["name"] as? String,
                  let command = serverConfig["command"] as? String else {
                continue
            }

            let server = MCPServerInfo(
                id: id,
                name: name,
                description: serverConfig["description"] as? String,
                command: command,
                args: serverConfig["args"] as? [String],
                env: serverConfig["env"] as? [String: String],
                enabled: serverConfig["enabled"] as? Bool ?? true,
                status: .stopped
            )
            loadedServers.append(server)
        }

        servers = loadedServers
        logger.info("mcp loaded \(loadedServers.count) servers")
    }

    /// Start an MCP server
    func startServer(id: String) async throws {
        guard let index = servers.firstIndex(where: { $0.id == id }) else {
            throw MCPError.serverNotFound(id)
        }

        servers[index].status = .starting
        logger.info("mcp starting server id=\(id)")

        // Send start request to gateway
        do {
            _ = try await ControlChannel.shared.request(
                method: "mcp.start",
                params: ["serverId": id]
            )
            servers[index].status = .running
            logger.info("mcp server started id=\(id)")
        } catch {
            servers[index].status = .error
            logger.error("mcp server start failed id=\(id) error=\(error.localizedDescription)")
            throw error
        }
    }

    /// Stop an MCP server
    func stopServer(id: String) async throws {
        guard let index = servers.firstIndex(where: { $0.id == id }) else {
            throw MCPError.serverNotFound(id)
        }

        logger.info("mcp stopping server id=\(id)")

        do {
            _ = try await ControlChannel.shared.request(
                method: "mcp.stop",
                params: ["serverId": id]
            )
            servers[index].status = .stopped
            logger.info("mcp server stopped id=\(id)")
        } catch {
            logger.error("mcp server stop failed id=\(id) error=\(error.localizedDescription)")
            throw error
        }
    }

    /// List available tools from running MCP servers
    func listTools() async throws -> [MCPTool] {
        let data = try await ControlChannel.shared.request(method: "mcp.tools")
        return try JSONDecoder().decode([MCPTool].self, from: data)
    }

    /// Call an MCP tool
    func callTool(serverId: String, toolName: String, arguments: [String: Any]) async throws -> Data {
        var params: [String: AnyHashable] = [
            "serverId": serverId,
            "tool": toolName
        ]
        // Convert arguments to AnyHashable
        for (key, value) in arguments {
            if let hashable = value as? AnyHashable {
                params[key] = hashable
            }
        }

        return try await ControlChannel.shared.request(
            method: "mcp.call",
            params: params
        )
    }
}

struct MCPTool: Identifiable, Codable {
    let id: String
    let serverId: String
    let name: String
    let description: String?
    let inputSchema: [String: Any]?

    enum CodingKeys: String, CodingKey {
        case id, serverId, name, description, inputSchema
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        serverId = try container.decode(String.self, forKey: .serverId)
        name = try container.decode(String.self, forKey: .name)
        description = try container.decodeIfPresent(String.self, forKey: .description)
        inputSchema = nil // JSON schema parsing would go here
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(id, forKey: .id)
        try container.encode(serverId, forKey: .serverId)
        try container.encode(name, forKey: .name)
        try container.encodeIfPresent(description, forKey: .description)
    }
}

enum MCPError: LocalizedError {
    case serverNotFound(String)
    case toolNotFound(String)
    case executionFailed(String)

    var errorDescription: String? {
        switch self {
        case .serverNotFound(let id):
            return "MCP server not found: \(id)"
        case .toolNotFound(let name):
            return "MCP tool not found: \(name)"
        case .executionFailed(let reason):
            return "MCP execution failed: \(reason)"
        }
    }
}
