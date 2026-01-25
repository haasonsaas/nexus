import Foundation
import OSLog

// MARK: - ChatHistoryError

enum ChatHistoryError: Error, LocalizedError {
    case invalidResponse
    case sessionNotFound
    case networkError(String)

    var errorDescription: String? {
        switch self {
        case .invalidResponse:
            return "Invalid response from server"
        case .sessionNotFound:
            return "Session not found"
        case .networkError(let message):
            return "Network error: \(message)"
        }
    }
}

// MARK: - ChatHistoryManager

/// Manages chat history loading and caching
@MainActor
@Observable
final class ChatHistoryManager {
    static let shared = ChatHistoryManager()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "chat-history")

    private(set) var messages: [String: [ChatMessage]] = [:] // sessionId -> messages
    private(set) var isLoading = false
    private(set) var lastRefresh: [String: Date] = [:]

    private let deduplicationWindow: TimeInterval = 1.0 // 1 second
    private let cacheValiditySeconds: TimeInterval = 30 // 30 seconds

    private init() {}

    // MARK: - History Loading

    func loadHistory(sessionId: String, force: Bool = false) async throws -> [ChatMessage] {
        // Check cache freshness
        if !force, let last = lastRefresh[sessionId], Date().timeIntervalSince(last) < cacheValiditySeconds {
            logger.debug("Using cached history for session \(sessionId)")
            return messages[sessionId] ?? []
        }

        isLoading = true
        defer { isLoading = false }

        do {
            let params: [String: AnyHashable] = ["session_id": sessionId]
            let data = try await ControlChannel.shared.request(method: "chat.history", params: params)

            guard let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let messagesData = json["messages"] as? [[String: Any]] else {
                throw ChatHistoryError.invalidResponse
            }

            let parsedMessages = messagesData.compactMap { dict -> ChatMessage? in
                guard let id = dict["id"] as? String,
                      let roleStr = dict["role"] as? String,
                      let role = ChatMessage.MessageRole(rawValue: roleStr),
                      let content = dict["content"] as? String else {
                    return nil
                }

                let timestamp: Date
                if let ts = dict["timestamp"] as? String {
                    timestamp = ISO8601DateFormatter().date(from: ts) ?? Date()
                } else {
                    timestamp = Date()
                }

                let sessionIdValue = dict["session_id"] as? String ?? sessionId

                // Parse metadata if present
                var metadata: ChatMessageMetadata?
                if let metaDict = dict["metadata"] as? [String: Any] {
                    let model = metaDict["model"] as? String

                    var tokens: TokenUsage?
                    if let tokensDict = metaDict["tokens"] as? [String: Any],
                       let input = tokensDict["input"] as? Int,
                       let output = tokensDict["output"] as? Int {
                        tokens = TokenUsage(input: input, output: output)
                    }

                    var toolCalls: [ToolCallInfo]?
                    if let toolCallsArray = metaDict["tool_calls"] as? [[String: Any]] {
                        toolCalls = toolCallsArray.compactMap { tc -> ToolCallInfo? in
                            guard let tcId = tc["id"] as? String,
                                  let tcName = tc["name"] as? String else {
                                return nil
                            }
                            return ToolCallInfo(
                                id: tcId,
                                name: tcName,
                                arguments: tc["arguments"] as? String,
                                result: tc["result"] as? String
                            )
                        }
                    }

                    metadata = ChatMessageMetadata(model: model, tokens: tokens, toolCalls: toolCalls)
                }

                return ChatMessage(
                    id: id,
                    sessionId: sessionIdValue,
                    role: role,
                    content: content,
                    timestamp: timestamp,
                    metadata: metadata
                )
            }

            // Deduplicate
            let deduped = deduplicateMessages(parsedMessages)
            messages[sessionId] = deduped
            lastRefresh[sessionId] = Date()

            logger.info("Loaded \(deduped.count) messages for session \(sessionId)")
            return deduped

        } catch let error as ChatHistoryError {
            logger.error("Failed to load history: \(error.localizedDescription)")
            throw error
        } catch {
            logger.error("Failed to load history: \(error.localizedDescription)")
            throw ChatHistoryError.networkError(error.localizedDescription)
        }
    }

    func appendMessage(_ message: ChatMessage) {
        var sessionMessages = messages[message.sessionId] ?? []

        // Check for duplicates
        if !sessionMessages.contains(where: { isDuplicate($0, message) }) {
            sessionMessages.append(message)
            messages[message.sessionId] = sessionMessages
            logger.debug("Appended message \(message.id) to session \(message.sessionId)")
        } else {
            logger.debug("Skipped duplicate message \(message.id)")
        }
    }

    func clearHistory(sessionId: String) {
        messages.removeValue(forKey: sessionId)
        lastRefresh.removeValue(forKey: sessionId)
        logger.info("Cleared history for session \(sessionId)")
    }

    func clearAllHistory() {
        messages.removeAll()
        lastRefresh.removeAll()
        logger.info("Cleared all chat history")
    }

    // MARK: - Convenience Methods

    func messagesForSession(_ sessionId: String) -> [ChatMessage] {
        messages[sessionId] ?? []
    }

    func hasMessages(for sessionId: String) -> Bool {
        guard let sessionMessages = messages[sessionId] else { return false }
        return !sessionMessages.isEmpty
    }

    func lastMessage(for sessionId: String) -> ChatMessage? {
        messages[sessionId]?.last
    }

    func messageCount(for sessionId: String) -> Int {
        messages[sessionId]?.count ?? 0
    }

    // MARK: - Deduplication

    private func deduplicateMessages(_ messages: [ChatMessage]) -> [ChatMessage] {
        var seen: Set<String> = []
        return messages.filter { msg in
            let key = "\(msg.role.rawValue)|\(msg.content.prefix(100))|\(Int(msg.timestamp.timeIntervalSince1970))"
            if seen.contains(key) {
                return false
            }
            seen.insert(key)
            return true
        }
    }

    private func isDuplicate(_ a: ChatMessage, _ b: ChatMessage) -> Bool {
        // Check by ID first
        if a.id == b.id {
            return true
        }

        // Fallback to content-based deduplication
        guard a.role == b.role else { return false }
        guard a.content == b.content else { return false }
        return abs(a.timestamp.timeIntervalSince(b.timestamp)) < deduplicationWindow
    }
}

// MARK: - ChatEventStream Integration

extension ChatHistoryManager {
    /// Start listening to chat events and automatically append new messages
    func startListeningToEvents() {
        Task { @MainActor in
            let stream = ChatEventStream.shared.subscribe()
            for await event in stream {
                switch event {
                case .chat(let message):
                    appendMessage(message)
                default:
                    break
                }
            }
        }
    }
}
