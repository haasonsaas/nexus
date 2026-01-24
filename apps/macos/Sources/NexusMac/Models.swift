import Foundation

struct SystemStatus: Codable {
    let uptime: String
    let uptimeString: String
    let goVersion: String
    let numGoroutines: Int
    let memAllocMb: Double
    let memSysMb: Double
    let numCpu: Int
    let sessionCount: Int
    let databaseStatus: String
    let channels: [ChannelStatus]

    private enum CodingKeys: String, CodingKey {
        case uptime
        case uptimeString = "uptime_string"
        case goVersion = "go_version"
        case numGoroutines = "num_goroutines"
        case memAllocMb = "mem_alloc_mb"
        case memSysMb = "mem_sys_mb"
        case numCpu = "num_cpu"
        case sessionCount = "session_count"
        case databaseStatus = "database_status"
        case channels
    }
}

struct ChannelStatus: Codable, Identifiable {
    let id = UUID()
    let name: String
    let type: String
    let status: String
    let enabled: Bool
    let connected: Bool?
    let error: String?
    let lastPing: Int64?
    let healthy: Bool?
    let healthMessage: String?
    let healthLatencyMs: Int64?
    let healthDegraded: Bool?

    private enum CodingKeys: String, CodingKey {
        case name
        case type
        case status
        case enabled
        case connected
        case error
        case lastPing = "last_ping"
        case healthy
        case healthMessage = "health_message"
        case healthLatencyMs = "health_latency_ms"
        case healthDegraded = "health_degraded"
    }
}

struct NodesResponse: Codable {
    let nodes: [NodeSummary]
}

struct NodeSummary: Codable, Identifiable, Hashable {
    let edgeId: String
    let name: String
    let status: String
    let connectedAt: Date
    let lastHeartbeat: Date
    let tools: [String]
    let channelTypes: [String]?
    let version: String?

    var id: String { edgeId }

    private enum CodingKeys: String, CodingKey {
        case edgeId = "edge_id"
        case name
        case status
        case connectedAt = "connected_at"
        case lastHeartbeat = "last_heartbeat"
        case tools
        case channelTypes = "channel_types"
        case version
    }
}

struct NodeToolsResponse: Codable {
    let tools: [NodeToolSummary]
}

struct NodeToolSummary: Codable, Identifiable, Hashable {
    let edgeId: String
    let name: String
    let description: String
    let inputSchema: String
    let requiresApproval: Bool
    let producesArtifacts: Bool
    let timeoutSeconds: Int

    var id: String { "\(edgeId):\(name)" }

    private enum CodingKeys: String, CodingKey {
        case edgeId = "edge_id"
        case name
        case description
        case inputSchema = "input_schema"
        case requiresApproval = "requires_approval"
        case producesArtifacts = "produces_artifacts"
        case timeoutSeconds = "timeout_seconds"
    }
}

struct ArtifactListResponse: Codable {
    let artifacts: [ArtifactSummary]
    let total: Int
}

struct ArtifactSummary: Codable, Identifiable, Hashable {
    let id: String
    let type: String
    let mimeType: String
    let filename: String
    let size: Int64
    let reference: String
    let ttlSeconds: Int64
    let redacted: Bool

    private enum CodingKeys: String, CodingKey {
        case id
        case type
        case mimeType = "mime_type"
        case filename
        case size
        case reference
        case ttlSeconds = "ttl_seconds"
        case redacted
    }
}

struct ToolInvocationResult: Codable {
    let content: String
    let isError: Bool
    let durationMs: Int64
    let errorDetails: String?

    private enum CodingKeys: String, CodingKey {
        case content
        case isError = "is_error"
        case durationMs = "duration_ms"
        case errorDetails = "error_details"
    }
}

struct APIError: Codable {
    let error: String
}
