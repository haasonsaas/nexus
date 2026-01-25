import Foundation
import OSLog

/// Represents a tool call in progress or completed
struct TrackedToolCall: Identifiable, Sendable {
    let id: String
    let name: String
    let arguments: String
    let startedAt: Date
    var phase: ToolCallPhase
    var result: String?
    var completedAt: Date?
    var error: String?

    enum ToolCallPhase: String, Sendable {
        case pending
        case executing
        case completed
        case failed
    }

    var duration: TimeInterval? {
        guard let completed = completedAt else { return nil }
        return completed.timeIntervalSince(startedAt)
    }

    var isActive: Bool {
        phase == .pending || phase == .executing
    }
}

/// Tool metadata for display
struct ToolMetadata {
    let name: String
    let displayName: String
    let icon: String
    let summaryExtractor: @Sendable (String) -> String?

    static let registry: [String: ToolMetadata] = [
        "bash": ToolMetadata(
            name: "bash",
            displayName: "Terminal",
            icon: "terminal",
            summaryExtractor: { args in
                // Extract command from arguments JSON
                guard let data = args.data(using: .utf8),
                      let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                      let command = json["command"] as? String else {
                    return nil
                }
                return String(command.prefix(50))
            }
        ),
        "read": ToolMetadata(
            name: "read",
            displayName: "Read File",
            icon: "doc.text",
            summaryExtractor: { args in
                guard let data = args.data(using: .utf8),
                      let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                      let path = json["file_path"] as? String else {
                    return nil
                }
                return URL(fileURLWithPath: path).lastPathComponent
            }
        ),
        "edit": ToolMetadata(
            name: "edit",
            displayName: "Edit File",
            icon: "pencil",
            summaryExtractor: { args in
                guard let data = args.data(using: .utf8),
                      let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                      let path = json["file_path"] as? String else {
                    return nil
                }
                return URL(fileURLWithPath: path).lastPathComponent
            }
        ),
        "write": ToolMetadata(
            name: "write",
            displayName: "Write File",
            icon: "doc.badge.plus",
            summaryExtractor: { args in
                guard let data = args.data(using: .utf8),
                      let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                      let path = json["file_path"] as? String else {
                    return nil
                }
                return URL(fileURLWithPath: path).lastPathComponent
            }
        ),
        "glob": ToolMetadata(
            name: "glob",
            displayName: "Find Files",
            icon: "magnifyingglass",
            summaryExtractor: { args in
                guard let data = args.data(using: .utf8),
                      let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                      let pattern = json["pattern"] as? String else {
                    return nil
                }
                return pattern
            }
        ),
        "grep": ToolMetadata(
            name: "grep",
            displayName: "Search",
            icon: "text.magnifyingglass",
            summaryExtractor: { args in
                guard let data = args.data(using: .utf8),
                      let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                      let pattern = json["pattern"] as? String else {
                    return nil
                }
                return pattern
            }
        ),
        "task": ToolMetadata(
            name: "task",
            displayName: "Agent Task",
            icon: "person.crop.circle.badge.plus",
            summaryExtractor: { args in
                guard let data = args.data(using: .utf8),
                      let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                      let desc = json["description"] as? String else {
                    return nil
                }
                return desc
            }
        ),
        "web_fetch": ToolMetadata(
            name: "web_fetch",
            displayName: "Fetch URL",
            icon: "globe",
            summaryExtractor: { args in
                guard let data = args.data(using: .utf8),
                      let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                      let url = json["url"] as? String else {
                    return nil
                }
                return URL(string: url)?.host ?? String(url.prefix(30))
            }
        )
    ]

    static func metadata(for name: String) -> ToolMetadata? {
        registry[name.lowercased()]
    }
}

/// Tracks active and completed tool calls
@MainActor
@Observable
final class ToolCallTracker {
    static let shared = ToolCallTracker()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "tool-tracker")

    // Tool calls indexed by ID
    private(set) var toolCallsById: [String: TrackedToolCall] = [:]

    // Sorted active tools
    var pendingToolCalls: [TrackedToolCall] {
        toolCallsById.values
            .filter { $0.isActive }
            .sorted { $0.startedAt < $1.startedAt }
    }

    // Recent completed tools
    var recentCompletedCalls: [TrackedToolCall] {
        Array(
            toolCallsById.values
                .filter { !$0.isActive }
                .sorted { ($0.completedAt ?? .distantPast) > ($1.completedAt ?? .distantPast) }
                .prefix(10)
        )
    }

    // Statistics
    var activeCount: Int { pendingToolCalls.count }
    var totalCount: Int { toolCallsById.count }

    private init() {}

    // MARK: - Lifecycle

    func startToolCall(id: String, name: String, arguments: String) {
        let call = TrackedToolCall(
            id: id,
            name: name,
            arguments: arguments,
            startedAt: Date(),
            phase: .pending
        )
        toolCallsById[id] = call
        logger.info("Tool call started: \(name) (\(id))")
    }

    func updatePhase(id: String, phase: TrackedToolCall.ToolCallPhase) {
        guard var call = toolCallsById[id] else {
            logger.warning("Unknown tool call: \(id)")
            return
        }
        call.phase = phase
        toolCallsById[id] = call
        logger.debug("Tool call phase: \(phase.rawValue) (\(id))")
    }

    func completeToolCall(id: String, result: String) {
        guard var call = toolCallsById[id] else {
            logger.warning("Unknown tool call: \(id)")
            return
        }
        call.phase = .completed
        call.result = result
        call.completedAt = Date()
        toolCallsById[id] = call

        let duration = call.duration ?? 0
        logger.info("Tool call completed: \(call.name) in \(String(format: "%.2f", duration))s")
    }

    func failToolCall(id: String, error: String) {
        guard var call = toolCallsById[id] else {
            logger.warning("Unknown tool call: \(id)")
            return
        }
        call.phase = .failed
        call.error = error
        call.completedAt = Date()
        toolCallsById[id] = call
        logger.error("Tool call failed: \(call.name) - \(error)")
    }

    // MARK: - Control Agent Event Integration

    /// Process a ControlAgentEvent from the control channel.
    /// Expects tool_use stream events with "start" or "result" data.
    func processControlAgentEvent(_ event: ControlAgentEvent) {
        // Tool use events have stream == "tool_use" or "tool_result"
        guard event.stream == "tool_use" || event.stream == "tool_result" else { return }

        let toolCallId = event.id

        if event.stream == "tool_use" {
            // Tool use start - extract name and arguments from data
            let name = event.data["name"]?.value as? String ?? "unknown"
            let args: String
            if let argsDict = event.data["arguments"]?.value as? [String: Any],
               let argsData = try? JSONSerialization.data(withJSONObject: argsDict),
               let argsStr = String(data: argsData, encoding: .utf8) {
                args = argsStr
            } else if let argsStr = event.data["arguments"]?.value as? String {
                args = argsStr
            } else {
                args = "{}"
            }

            startToolCall(id: toolCallId, name: name, arguments: args)
            updatePhase(id: toolCallId, phase: .executing)

        } else if event.stream == "tool_result" {
            // Tool result - check for error or success
            if let errorMsg = event.data["error"]?.value as? String {
                failToolCall(id: toolCallId, error: errorMsg)
            } else {
                let result: String
                if let content = event.data["content"]?.value as? String {
                    result = content
                } else if let resultData = event.data["result"]?.value as? String {
                    result = resultData
                } else {
                    result = ""
                }
                completeToolCall(id: toolCallId, result: result)
            }
        }
    }

    // MARK: - Cleanup

    func clearAll() {
        toolCallsById.removeAll()
        logger.info("Cleared all tool calls")
    }

    func clearCompleted() {
        toolCallsById = toolCallsById.filter { $0.value.isActive }
        logger.debug("Cleared completed tool calls")
    }

    func pruneOld(olderThan: TimeInterval = 3600) {
        let cutoff = Date().addingTimeInterval(-olderThan)
        toolCallsById = toolCallsById.filter { call in
            call.value.isActive || (call.value.completedAt ?? .distantPast) > cutoff
        }
    }
}
