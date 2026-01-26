import XCTest
@testable import NexusMac
import Foundation

final class ModelsTests: XCTestCase {

    private var decoder: JSONDecoder!

    override func setUp() {
        super.setUp()
        decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
    }

    override func tearDown() {
        decoder = nil
        super.tearDown()
    }

    // MARK: - SystemStatus Tests

    func testSystemStatusDecoding() throws {
        let json = """
        {
            "uptime": "3h15m",
            "uptime_string": "3 hours 15 minutes",
            "go_version": "go1.24.12",
            "num_goroutines": 50,
            "mem_alloc_mb": 64.25,
            "mem_sys_mb": 128.5,
            "num_cpu": 4,
            "session_count": 25,
            "database_status": "healthy",
            "channels": []
        }
        """

        let status = try decoder.decode(SystemStatus.self, from: json.data(using: .utf8)!)

        XCTAssertEqual(status.uptime, "3h15m")
        XCTAssertEqual(status.uptimeString, "3 hours 15 minutes")
        XCTAssertEqual(status.goVersion, "go1.24.12")
        XCTAssertEqual(status.numGoroutines, 50)
        XCTAssertEqual(status.memAllocMb, 64.25, accuracy: 0.001)
        XCTAssertEqual(status.memSysMb, 128.5, accuracy: 0.001)
        XCTAssertEqual(status.numCpu, 4)
        XCTAssertEqual(status.sessionCount, 25)
        XCTAssertEqual(status.databaseStatus, "healthy")
        XCTAssertTrue(status.channels.isEmpty)
    }

    func testSystemStatusWithChannels() throws {
        let json = """
        {
            "uptime": "1h",
            "uptime_string": "1 hour",
            "go_version": "go1.21",
            "num_goroutines": 10,
            "mem_alloc_mb": 50.0,
            "mem_sys_mb": 100.0,
            "num_cpu": 8,
            "session_count": 5,
            "database_status": "connected",
            "channels": [
                {
                    "name": "slack-main",
                    "type": "slack",
                    "status": "connected",
                    "enabled": true,
                    "connected": true,
                    "healthy": true,
                    "health_message": "OK",
                    "health_latency_ms": 50
                },
                {
                    "name": "discord-dev",
                    "type": "discord",
                    "status": "disconnected",
                    "enabled": false,
                    "connected": false,
                    "error": "Token expired"
                }
            ]
        }
        """

        let status = try decoder.decode(SystemStatus.self, from: json.data(using: .utf8)!)

        XCTAssertEqual(status.channels.count, 2)

        let slack = status.channels[0]
        XCTAssertEqual(slack.name, "slack-main")
        XCTAssertEqual(slack.type, "slack")
        XCTAssertTrue(slack.enabled)
        XCTAssertEqual(slack.connected, true)
        XCTAssertEqual(slack.healthy, true)
        XCTAssertEqual(slack.healthMessage, "OK")
        XCTAssertEqual(slack.healthLatencyMs, 50)

        let discord = status.channels[1]
        XCTAssertEqual(discord.name, "discord-dev")
        XCTAssertFalse(discord.enabled)
        XCTAssertEqual(discord.error, "Token expired")
    }

    // MARK: - ChannelStatus Tests

    func testChannelStatusMinimalFields() throws {
        let json = """
        {
            "name": "test-channel",
            "type": "slack",
            "status": "pending",
            "enabled": true
        }
        """

        let channel = try decoder.decode(ChannelStatus.self, from: json.data(using: .utf8)!)

        XCTAssertEqual(channel.name, "test-channel")
        XCTAssertEqual(channel.type, "slack")
        XCTAssertEqual(channel.status, "pending")
        XCTAssertTrue(channel.enabled)
        XCTAssertNil(channel.connected)
        XCTAssertNil(channel.error)
        XCTAssertNil(channel.lastPing)
        XCTAssertNil(channel.healthy)
    }

    func testChannelStatusAllFields() throws {
        let json = """
        {
            "name": "full-channel",
            "type": "discord",
            "status": "connected",
            "enabled": true,
            "connected": true,
            "error": null,
            "last_ping": 1705320000,
            "healthy": true,
            "health_message": "All systems operational",
            "health_latency_ms": 25,
            "health_degraded": false
        }
        """

        let channel = try decoder.decode(ChannelStatus.self, from: json.data(using: .utf8)!)

        XCTAssertEqual(channel.name, "full-channel")
        XCTAssertEqual(channel.connected, true)
        XCTAssertNil(channel.error)
        XCTAssertEqual(channel.lastPing, 1705320000)
        XCTAssertEqual(channel.healthy, true)
        XCTAssertEqual(channel.healthMessage, "All systems operational")
        XCTAssertEqual(channel.healthLatencyMs, 25)
        XCTAssertEqual(channel.healthDegraded, false)
    }

    // MARK: - NodeSummary Tests

    func testNodeSummaryDecoding() throws {
        let json = """
        {
            "edge_id": "node-abc-123",
            "name": "Production Node",
            "status": "online",
            "connected_at": "2024-01-15T08:00:00Z",
            "last_heartbeat": "2024-01-15T12:30:00Z",
            "tools": ["bash", "read_file", "write_file"],
            "channel_types": ["slack", "discord", "telegram"],
            "version": "2.1.0",
            "metadata": {
                "region": "us-west-2",
                "environment": "production"
            }
        }
        """

        let node = try decoder.decode(NodeSummary.self, from: json.data(using: .utf8)!)

        XCTAssertEqual(node.edgeId, "node-abc-123")
        XCTAssertEqual(node.id, "node-abc-123")
        XCTAssertEqual(node.name, "Production Node")
        XCTAssertEqual(node.status, "online")
        XCTAssertEqual(node.tools, ["bash", "read_file", "write_file"])
        XCTAssertEqual(node.channelTypes, ["slack", "discord", "telegram"])
        XCTAssertEqual(node.version, "2.1.0")
        XCTAssertEqual(node.metadata?["region"], "us-west-2")
        XCTAssertEqual(node.metadata?["environment"], "production")
    }

    func testNodeSummaryMinimalFields() throws {
        let json = """
        {
            "edge_id": "minimal-node",
            "name": "Minimal",
            "status": "offline",
            "connected_at": "2024-01-01T00:00:00Z",
            "last_heartbeat": "2024-01-01T00:00:00Z",
            "tools": []
        }
        """

        let node = try decoder.decode(NodeSummary.self, from: json.data(using: .utf8)!)

        XCTAssertEqual(node.edgeId, "minimal-node")
        XCTAssertTrue(node.tools.isEmpty)
        XCTAssertNil(node.channelTypes)
        XCTAssertNil(node.version)
        XCTAssertNil(node.metadata)
    }

    func testNodeSummaryHashable() throws {
        let json1 = """
        {
            "edge_id": "node-1",
            "name": "Node 1",
            "status": "online",
            "connected_at": "2024-01-01T00:00:00Z",
            "last_heartbeat": "2024-01-01T00:00:00Z",
            "tools": []
        }
        """

        let json2 = """
        {
            "edge_id": "node-2",
            "name": "Node 2",
            "status": "online",
            "connected_at": "2024-01-01T00:00:00Z",
            "last_heartbeat": "2024-01-01T00:00:00Z",
            "tools": []
        }
        """

        let node1 = try decoder.decode(NodeSummary.self, from: json1.data(using: .utf8)!)
        let node2 = try decoder.decode(NodeSummary.self, from: json2.data(using: .utf8)!)

        var nodeSet: Set<NodeSummary> = []
        nodeSet.insert(node1)
        nodeSet.insert(node2)

        XCTAssertEqual(nodeSet.count, 2)
    }

    // MARK: - SessionSummary Tests

    func testSessionSummaryDecoding() throws {
        let json = """
        {
            "id": "sess-xyz-789",
            "title": "Bug Investigation",
            "channel": "slack",
            "channel_id": "C0123456789",
            "agent_id": "agent-prod-01",
            "created_at": "2024-01-15T09:00:00Z",
            "updated_at": "2024-01-15T11:30:00Z"
        }
        """

        let session = try decoder.decode(SessionSummary.self, from: json.data(using: .utf8)!)

        XCTAssertEqual(session.id, "sess-xyz-789")
        XCTAssertEqual(session.title, "Bug Investigation")
        XCTAssertEqual(session.channel, "slack")
        XCTAssertEqual(session.channelId, "C0123456789")
        XCTAssertEqual(session.agentId, "agent-prod-01")
    }

    // MARK: - SessionMessage Tests

    func testSessionMessageDecoding() throws {
        let json = """
        {
            "id": "msg-001",
            "session_id": "sess-001",
            "channel": "slack",
            "channel_id": "C123",
            "direction": "inbound",
            "role": "user",
            "content": "Hello, can you help me?",
            "created_at": "2024-01-15T10:00:00Z"
        }
        """

        let message = try decoder.decode(SessionMessage.self, from: json.data(using: .utf8)!)

        XCTAssertEqual(message.id, "msg-001")
        XCTAssertEqual(message.sessionId, "sess-001")
        XCTAssertEqual(message.channel, "slack")
        XCTAssertEqual(message.channelId, "C123")
        XCTAssertEqual(message.direction, "inbound")
        XCTAssertEqual(message.role, "user")
        XCTAssertEqual(message.content, "Hello, can you help me?")
    }

    func testSessionMessageHashable() throws {
        let json = """
        {
            "id": "msg-unique",
            "session_id": "sess-001",
            "channel": "slack",
            "channel_id": "C123",
            "direction": "outbound",
            "role": "assistant",
            "content": "Sure!",
            "created_at": "2024-01-15T10:01:00Z"
        }
        """

        let message = try decoder.decode(SessionMessage.self, from: json.data(using: .utf8)!)
        var messageSet: Set<SessionMessage> = []
        messageSet.insert(message)

        XCTAssertEqual(messageSet.count, 1)
    }

    // MARK: - SessionMessagesResponse Tests

    func testSessionMessagesResponseDecoding() throws {
        let json = """
        {
            "messages": [
                {
                    "id": "msg-1",
                    "session_id": "sess-1",
                    "channel": "discord",
                    "channel_id": "D1",
                    "direction": "inbound",
                    "role": "user",
                    "content": "Test",
                    "created_at": "2024-01-15T10:00:00Z"
                }
            ],
            "total": 100,
            "page": 2,
            "page_size": 25,
            "has_more": true
        }
        """

        let response = try decoder.decode(SessionMessagesResponse.self, from: json.data(using: .utf8)!)

        XCTAssertEqual(response.messages.count, 1)
        XCTAssertEqual(response.total, 100)
        XCTAssertEqual(response.page, 2)
        XCTAssertEqual(response.pageSize, 25)
        XCTAssertTrue(response.hasMore)
    }

    // MARK: - NodeToolSummary Tests

    func testNodeToolSummaryDecoding() throws {
        let json = """
        {
            "edge_id": "edge-001",
            "name": "execute_bash",
            "description": "Execute a bash command on the server",
            "input_schema": "{\\"command\\": \\"string\\"}",
            "requires_approval": true,
            "produces_artifacts": false,
            "timeout_seconds": 60
        }
        """

        let tool = try decoder.decode(NodeToolSummary.self, from: json.data(using: .utf8)!)

        XCTAssertEqual(tool.edgeId, "edge-001")
        XCTAssertEqual(tool.name, "execute_bash")
        XCTAssertEqual(tool.description, "Execute a bash command on the server")
        XCTAssertTrue(tool.requiresApproval)
        XCTAssertFalse(tool.producesArtifacts)
        XCTAssertEqual(tool.timeoutSeconds, 60)
        XCTAssertEqual(tool.id, "edge-001:execute_bash")
    }

    // MARK: - ArtifactSummary Tests

    func testArtifactSummaryDecoding() throws {
        let json = """
        {
            "id": "artifact-abc",
            "type": "file",
            "mime_type": "image/png",
            "filename": "screenshot.png",
            "size": 1024000,
            "reference": "s3://bucket/screenshot.png",
            "ttl_seconds": 3600,
            "redacted": false
        }
        """

        let artifact = try decoder.decode(ArtifactSummary.self, from: json.data(using: .utf8)!)

        XCTAssertEqual(artifact.id, "artifact-abc")
        XCTAssertEqual(artifact.type, "file")
        XCTAssertEqual(artifact.mimeType, "image/png")
        XCTAssertEqual(artifact.filename, "screenshot.png")
        XCTAssertEqual(artifact.size, 1024000)
        XCTAssertEqual(artifact.reference, "s3://bucket/screenshot.png")
        XCTAssertEqual(artifact.ttlSeconds, 3600)
        XCTAssertFalse(artifact.redacted)
    }

    // MARK: - ProviderStatus Tests

    func testProviderStatusDecoding() throws {
        let json = """
        {
            "name": "whatsapp",
            "enabled": true,
            "connected": true,
            "healthy": true,
            "health_message": "Connected",
            "health_latency_ms": 100,
            "health_degraded": false,
            "qr_available": false
        }
        """

        let provider = try decoder.decode(ProviderStatus.self, from: json.data(using: .utf8)!)

        XCTAssertEqual(provider.name, "whatsapp")
        XCTAssertEqual(provider.id, "whatsapp")
        XCTAssertTrue(provider.enabled)
        XCTAssertTrue(provider.connected)
        XCTAssertEqual(provider.healthy, true)
        XCTAssertEqual(provider.healthMessage, "Connected")
        XCTAssertEqual(provider.healthLatencyMs, 100)
        XCTAssertEqual(provider.healthDegraded, false)
        XCTAssertEqual(provider.qrAvailable, false)
    }

    func testProviderStatusWithQR() throws {
        let json = """
        {
            "name": "whatsapp",
            "enabled": true,
            "connected": false,
            "qr_available": true,
            "qr_updated_at": "2024-01-15T12:00:00Z"
        }
        """

        let provider = try decoder.decode(ProviderStatus.self, from: json.data(using: .utf8)!)

        XCTAssertFalse(provider.connected)
        XCTAssertEqual(provider.qrAvailable, true)
        XCTAssertEqual(provider.qrUpdatedAt, "2024-01-15T12:00:00Z")
    }

    // MARK: - SkillSummary Tests

    func testSkillSummaryDecoding() throws {
        let json = """
        {
            "name": "code-review",
            "description": "Performs automated code review",
            "source": "builtin",
            "path": "/skills/code-review",
            "emoji": "🔍",
            "execution": "sync",
            "eligible": true,
            "reason": null
        }
        """

        let skill = try decoder.decode(SkillSummary.self, from: json.data(using: .utf8)!)

        XCTAssertEqual(skill.name, "code-review")
        XCTAssertEqual(skill.id, "code-review")
        XCTAssertEqual(skill.description, "Performs automated code review")
        XCTAssertEqual(skill.source, "builtin")
        XCTAssertEqual(skill.path, "/skills/code-review")
        XCTAssertEqual(skill.emoji, "🔍")
        XCTAssertEqual(skill.execution, "sync")
        XCTAssertTrue(skill.eligible)
        XCTAssertNil(skill.reason)
    }

    func testSkillSummaryIneligible() throws {
        let json = """
        {
            "name": "premium-feature",
            "description": "Premium feature",
            "source": "marketplace",
            "path": "/skills/premium",
            "eligible": false,
            "reason": "Requires premium subscription"
        }
        """

        let skill = try decoder.decode(SkillSummary.self, from: json.data(using: .utf8)!)

        XCTAssertFalse(skill.eligible)
        XCTAssertEqual(skill.reason, "Requires premium subscription")
        XCTAssertNil(skill.emoji)
        XCTAssertNil(skill.execution)
    }

    // MARK: - CronJobSummary Tests

    func testCronJobSummaryDecoding() throws {
        let json = """
        {
            "id": "cron-001",
            "name": "Daily Backup",
            "type": "backup",
            "enabled": true,
            "schedule": "0 2 * * *",
            "next_run": "2024-01-16T02:00:00Z",
            "last_run": "2024-01-15T02:00:00Z"
        }
        """

        let job = try decoder.decode(CronJobSummary.self, from: json.data(using: .utf8)!)

        XCTAssertEqual(job.id, "cron-001")
        XCTAssertEqual(job.name, "Daily Backup")
        XCTAssertEqual(job.type, "backup")
        XCTAssertTrue(job.enabled)
        XCTAssertEqual(job.schedule, "0 2 * * *")
        XCTAssertNil(job.lastError)
    }

    func testCronJobSummaryWithError() throws {
        let json = """
        {
            "id": "cron-002",
            "name": "Failed Job",
            "type": "sync",
            "enabled": true,
            "schedule": "*/5 * * * *",
            "next_run": "2024-01-15T12:05:00Z",
            "last_run": "2024-01-15T12:00:00Z",
            "last_error": "Connection timeout"
        }
        """

        let job = try decoder.decode(CronJobSummary.self, from: json.data(using: .utf8)!)

        XCTAssertEqual(job.lastError, "Connection timeout")
    }

    // MARK: - CronResponse Tests

    func testCronResponseDecoding() throws {
        let json = """
        {
            "enabled": true,
            "jobs": [
                {
                    "id": "job-1",
                    "name": "Test Job",
                    "type": "test",
                    "enabled": true,
                    "schedule": "* * * * *",
                    "next_run": "2024-01-15T12:01:00Z",
                    "last_run": "2024-01-15T12:00:00Z"
                }
            ]
        }
        """

        let response = try decoder.decode(CronResponse.self, from: json.data(using: .utf8)!)

        XCTAssertTrue(response.enabled)
        XCTAssertEqual(response.jobs.count, 1)
        XCTAssertEqual(response.jobs.first?.name, "Test Job")
    }

    // MARK: - ToolInvocationArtifact Tests

    func testToolInvocationArtifactDecoding() throws {
        let json = """
        {
            "id": "art-001",
            "type": "screenshot",
            "mime_type": "image/jpeg",
            "filename": "capture.jpg",
            "size": 512000,
            "reference": "local://tmp/capture.jpg",
            "ttl_seconds": 1800
        }
        """

        let artifact = try decoder.decode(ToolInvocationArtifact.self, from: json.data(using: .utf8)!)

        XCTAssertEqual(artifact.id, "art-001")
        XCTAssertEqual(artifact.type, "screenshot")
        XCTAssertEqual(artifact.mimeType, "image/jpeg")
        XCTAssertEqual(artifact.filename, "capture.jpg")
        XCTAssertEqual(artifact.size, 512000)
        XCTAssertEqual(artifact.reference, "local://tmp/capture.jpg")
        XCTAssertEqual(artifact.ttlSeconds, 1800)
        XCTAssertNil(artifact.data)
    }

    // MARK: - Edge Cases

    func testDecodingWithNullOptionalFields() throws {
        let json = """
        {
            "name": "test-channel",
            "type": "slack",
            "status": "pending",
            "enabled": true,
            "connected": null,
            "error": null,
            "last_ping": null,
            "healthy": null,
            "health_message": null,
            "health_latency_ms": null,
            "health_degraded": null
        }
        """

        let channel = try decoder.decode(ChannelStatus.self, from: json.data(using: .utf8)!)

        XCTAssertNil(channel.connected)
        XCTAssertNil(channel.error)
        XCTAssertNil(channel.lastPing)
        XCTAssertNil(channel.healthy)
        XCTAssertNil(channel.healthMessage)
        XCTAssertNil(channel.healthLatencyMs)
        XCTAssertNil(channel.healthDegraded)
    }

    func testDecodingEmptyArrays() throws {
        let json = """
        {
            "sessions": [],
            "total": 0,
            "page": 1,
            "page_size": 50,
            "has_more": false
        }
        """

        let response = try decoder.decode(SessionListResponse.self, from: json.data(using: .utf8)!)

        XCTAssertTrue(response.sessions.isEmpty)
        XCTAssertEqual(response.total, 0)
        XCTAssertFalse(response.hasMore)
    }

    func testDecodingLargeNumbers() throws {
        let json = """
        {
            "id": "artifact-big",
            "type": "video",
            "mime_type": "video/mp4",
            "filename": "recording.mp4",
            "size": 9999999999,
            "reference": "s3://bucket/recording.mp4",
            "ttl_seconds": 86400,
            "redacted": false
        }
        """

        let artifact = try decoder.decode(ArtifactSummary.self, from: json.data(using: .utf8)!)

        XCTAssertEqual(artifact.size, 9999999999)
    }

    func testDecodingSpecialCharactersInStrings() throws {
        let json = """
        {
            "id": "msg-special",
            "session_id": "sess-special",
            "channel": "slack",
            "channel_id": "C123",
            "direction": "inbound",
            "role": "user",
            "content": "Hello! 🎉 Here's some <html> & 'quotes' and \\"escaped\\"",
            "created_at": "2024-01-15T10:00:00Z"
        }
        """

        let message = try decoder.decode(SessionMessage.self, from: json.data(using: .utf8)!)

        XCTAssertTrue(message.content.contains("🎉"))
        XCTAssertTrue(message.content.contains("<html>"))
        XCTAssertTrue(message.content.contains("&"))
    }

    func testDecodingUnicodeContent() throws {
        let json = """
        {
            "id": "msg-unicode",
            "session_id": "sess-1",
            "channel": "telegram",
            "channel_id": "T1",
            "direction": "outbound",
            "role": "assistant",
            "content": "你好世界 مرحبا 🌍",
            "created_at": "2024-01-15T10:00:00Z"
        }
        """

        let message = try decoder.decode(SessionMessage.self, from: json.data(using: .utf8)!)

        XCTAssertTrue(message.content.contains("你好世界"))
        XCTAssertTrue(message.content.contains("مرحبا"))
        XCTAssertTrue(message.content.contains("🌍"))
    }
}
