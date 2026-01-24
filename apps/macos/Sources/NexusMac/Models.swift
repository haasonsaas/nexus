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
    let metadata: [String: String]?

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
        case metadata
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
    let artifacts: [ToolInvocationArtifact]?

    private enum CodingKeys: String, CodingKey {
        case content
        case isError = "is_error"
        case durationMs = "duration_ms"
        case errorDetails = "error_details"
        case artifacts
    }
}

struct ToolInvocationArtifact: Codable, Hashable {
    let id: String
    let type: String
    let mimeType: String
    let filename: String?
    let size: Int64
    let reference: String?
    let data: Data?
    let ttlSeconds: Int?

    private enum CodingKeys: String, CodingKey {
        case id
        case type
        case mimeType = "mime_type"
        case filename
        case size
        case reference
        case data
        case ttlSeconds = "ttl_seconds"
    }
}

struct APIError: Codable {
    let error: String
}

struct SessionListResponse: Codable {
    let sessions: [SessionSummary]
    let total: Int
    let page: Int
    let pageSize: Int
    let hasMore: Bool

    private enum CodingKeys: String, CodingKey {
        case sessions
        case total
        case page
        case pageSize = "page_size"
        case hasMore = "has_more"
    }
}

struct SessionSummary: Codable, Identifiable, Hashable {
    let id: String
    let title: String
    let channel: String
    let channelId: String
    let agentId: String
    let createdAt: Date
    let updatedAt: Date

    private enum CodingKeys: String, CodingKey {
        case id
        case title
        case channel
        case channelId = "channel_id"
        case agentId = "agent_id"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }
}

struct SessionMessagesResponse: Codable {
    let messages: [SessionMessage]
    let total: Int
    let page: Int
    let pageSize: Int
    let hasMore: Bool

    private enum CodingKeys: String, CodingKey {
        case messages
        case total
        case page
        case pageSize = "page_size"
        case hasMore = "has_more"
    }
}

struct SessionMessage: Codable, Identifiable, Hashable {
    let id: String
    let sessionId: String
    let channel: String
    let channelId: String
    let direction: String
    let role: String
    let content: String
    let createdAt: Date

    private enum CodingKeys: String, CodingKey {
        case id
        case sessionId = "session_id"
        case channel
        case channelId = "channel_id"
        case direction
        case role
        case content
        case createdAt = "created_at"
    }
}

struct ProvidersResponse: Codable {
    let providers: [ProviderStatus]
}

struct ProviderStatus: Codable, Identifiable, Hashable {
    let name: String
    let enabled: Bool
    let connected: Bool
    let error: String?
    let lastPing: Int64?
    let healthy: Bool?
    let healthMessage: String?
    let healthLatencyMs: Int64?
    let healthDegraded: Bool?
    let qrAvailable: Bool?
    let qrUpdatedAt: String?

    var id: String { name }

    private enum CodingKeys: String, CodingKey {
        case name
        case enabled
        case connected
        case error
        case lastPing = "last_ping"
        case healthy
        case healthMessage = "health_message"
        case healthLatencyMs = "health_latency_ms"
        case healthDegraded = "health_degraded"
        case qrAvailable = "qr_available"
        case qrUpdatedAt = "qr_updated_at"
    }
}

struct SkillsResponse: Codable {
    let skills: [SkillSummary]
}

struct SkillSummary: Codable, Identifiable, Hashable {
    let name: String
    let description: String
    let source: String
    let path: String
    let emoji: String?
    let execution: String?
    let eligible: Bool
    let reason: String?

    var id: String { name }
}

struct CronResponse: Codable {
    let enabled: Bool
    let jobs: [CronJobSummary]
}

struct CronJobSummary: Codable, Identifiable, Hashable {
    let id: String
    let name: String
    let type: String
    let enabled: Bool
    let schedule: String
    let nextRun: Date
    let lastRun: Date
    let lastError: String?

    private enum CodingKeys: String, CodingKey {
        case id
        case name
        case type
        case enabled
        case schedule
        case nextRun = "next_run"
        case lastRun = "last_run"
        case lastError = "last_error"
    }
}
