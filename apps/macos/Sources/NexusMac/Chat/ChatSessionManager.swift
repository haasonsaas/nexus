import Foundation
import OSLog

// MARK: - Chat Session

/// Chat session metadata
struct ChatSession: Identifiable, Codable, Sendable, Hashable {
    let id: String
    var title: String?
    var createdAt: Date
    var updatedAt: Date
    var messageCount: Int
    var lastMessage: String?
    var isActive: Bool
    var model: String?
    var provider: String?

    init(
        id: String = UUID().uuidString,
        title: String? = nil,
        model: String? = nil,
        provider: String? = nil
    ) {
        self.id = id
        self.title = title
        self.createdAt = Date()
        self.updatedAt = Date()
        self.messageCount = 0
        self.lastMessage = nil
        self.isActive = true
        self.model = model
        self.provider = provider
    }

    private enum CodingKeys: String, CodingKey {
        case id
        case title
        case createdAt = "created_at"
        case updatedAt = "updated_at"
        case messageCount = "message_count"
        case lastMessage = "last_message"
        case isActive = "is_active"
        case model
        case provider
    }
}

// MARK: - Session Error

enum SessionError: Error, LocalizedError {
    case notFound
    case noActiveSession
    case createFailed(String)
    case switchFailed(String)
    case loadFailed(String)

    var errorDescription: String? {
        switch self {
        case .notFound:
            return "Session not found"
        case .noActiveSession:
            return "No active session"
        case .createFailed(let reason):
            return "Failed to create session: \(reason)"
        case .switchFailed(let reason):
            return "Failed to switch session: \(reason)"
        case .loadFailed(let reason):
            return "Failed to load sessions: \(reason)"
        }
    }
}

// MARK: - Chat Session Manager

/// Manages chat sessions.
/// Handles session lifecycle, switching, and persistence.
@MainActor
@Observable
final class ChatSessionManager {
    static let shared = ChatSessionManager()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "chat-sessions")

    // State
    private(set) var sessions: [ChatSession] = []
    private(set) var activeSessionId: String?
    private(set) var isLoading = false
    private(set) var error: String?

    // Transport
    private let transport: ChatTransportProtocol
    private var controlChannel: ControlChannel { ControlChannel.shared }

    // Persistence
    private var saveTask: Task<Void, Never>?
    private let saveDebounceMs: UInt64 = 500

    init(transport: ChatTransportProtocol? = nil) {
        self.transport = transport ?? GatewayChatTransport()
        loadFromDisk()
    }

    // MARK: - Session State

    var activeSession: ChatSession? {
        sessions.first { $0.id == activeSessionId }
    }

    var recentSessions: [ChatSession] {
        sessions.sorted { $0.updatedAt > $1.updatedAt }
    }

    var sessionCount: Int {
        sessions.count
    }

    // MARK: - Session Lifecycle

    /// Create a new session
    /// - Parameters:
    ///   - title: Optional session title
    ///   - model: Optional model identifier
    ///   - provider: Optional provider identifier
    /// - Returns: The created session
    @discardableResult
    func createSession(
        title: String? = nil,
        model: String? = nil,
        provider: String? = nil
    ) async throws -> ChatSession {
        let session = ChatSession(
            id: UUID().uuidString,
            title: title,
            model: model,
            provider: provider
        )

        sessions.insert(session, at: 0)
        activeSessionId = session.id

        // Notify gateway of new session
        do {
            _ = try await controlChannel.request(
                method: "session.create",
                params: [
                    "id": session.id,
                    "title": title ?? "",
                    "model": model ?? "",
                    "provider": provider ?? ""
                ]
            )
        } catch {
            logger.warning("Failed to notify gateway of session creation: \(error.localizedDescription)")
        }

        scheduleSave()
        logger.info("Created session: \(session.id)")

        return session
    }

    /// Switch to a different session
    /// - Parameter sessionId: The session ID to switch to
    func switchSession(_ sessionId: String) async throws {
        guard sessions.contains(where: { $0.id == sessionId }) else {
            throw SessionError.notFound
        }

        activeSessionId = sessionId

        // Load history for the session
        do {
            _ = try await ChatHistoryManager.shared.loadHistory(sessionId: sessionId)
        } catch {
            logger.warning("Failed to load history for session \(sessionId): \(error.localizedDescription)")
        }

        // Update last active timestamp
        if let index = sessions.firstIndex(where: { $0.id == sessionId }) {
            sessions[index].updatedAt = Date()
        }

        scheduleSave()
        logger.info("Switched to session: \(sessionId)")
    }

    /// Close a session
    /// - Parameter sessionId: The session ID to close
    func closeSession(_ sessionId: String) {
        sessions.removeAll { $0.id == sessionId }

        if activeSessionId == sessionId {
            activeSessionId = sessions.first?.id
        }

        ChatHistoryManager.shared.clearHistory(sessionId: sessionId)

        // Notify gateway
        Task {
            do {
                _ = try await controlChannel.request(
                    method: "session.close",
                    params: ["id": sessionId]
                )
            } catch {
                logger.warning("Failed to notify gateway of session close: \(error.localizedDescription)")
            }
        }

        scheduleSave()
        logger.info("Closed session: \(sessionId)")
    }

    /// Archive a session (mark as inactive)
    /// - Parameter sessionId: The session ID to archive
    func archiveSession(_ sessionId: String) {
        guard let index = sessions.firstIndex(where: { $0.id == sessionId }) else {
            return
        }

        sessions[index].isActive = false
        sessions[index].updatedAt = Date()

        if activeSessionId == sessionId {
            activeSessionId = sessions.first { $0.isActive }?.id
        }

        scheduleSave()
        logger.info("Archived session: \(sessionId)")
    }

    /// Update session metadata
    /// - Parameters:
    ///   - sessionId: The session ID to update
    ///   - title: Optional new title
    ///   - model: Optional new model
    ///   - provider: Optional new provider
    func updateSession(
        _ sessionId: String,
        title: String? = nil,
        model: String? = nil,
        provider: String? = nil
    ) {
        guard let index = sessions.firstIndex(where: { $0.id == sessionId }) else {
            return
        }

        if let title = title {
            sessions[index].title = title
        }
        if let model = model {
            sessions[index].model = model
        }
        if let provider = provider {
            sessions[index].provider = provider
        }
        sessions[index].updatedAt = Date()

        scheduleSave()
    }

    // MARK: - Messaging

    /// Send a message in the active session
    /// - Parameters:
    ///   - content: The message content
    ///   - attachments: Optional attachments
    func send(content: String, attachments: [ChatAttachment]? = nil) async throws {
        guard let sessionId = activeSessionId else {
            throw SessionError.noActiveSession
        }

        let messageId = try await transport.send(
            sessionId: sessionId,
            content: content,
            attachments: attachments
        )

        // Update session metadata
        if let index = sessions.firstIndex(where: { $0.id == sessionId }) {
            sessions[index].updatedAt = Date()
            sessions[index].messageCount += 1
            sessions[index].lastMessage = String(content.prefix(100))
        }

        scheduleSave()
        logger.debug("Sent message \(messageId) to session \(sessionId)")
    }

    /// Abort current generation in the active session
    func abort() async throws {
        guard let sessionId = activeSessionId else {
            throw SessionError.noActiveSession
        }

        try await transport.abort(sessionId: sessionId)
        logger.info("Aborted generation for session \(sessionId)")
    }

    // MARK: - Loading

    /// Load sessions from the server
    func loadSessions() async throws {
        isLoading = true
        error = nil
        defer { isLoading = false }

        do {
            let data = try await controlChannel.request(
                method: "sessions.list",
                params: [:]
            )

            guard let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let sessionsData = json["sessions"] as? [[String: Any]] else {
                return
            }

            let loadedSessions = sessionsData.compactMap { dict -> ChatSession? in
                parseSession(from: dict)
            }

            // Merge with local sessions, preferring remote data
            var merged: [String: ChatSession] = [:]
            for session in sessions {
                merged[session.id] = session
            }
            for session in loadedSessions {
                merged[session.id] = session
            }

            sessions = Array(merged.values).sorted { $0.updatedAt > $1.updatedAt }

            // Set active session if not set
            if activeSessionId == nil {
                activeSessionId = sessions.first?.id
            }

            scheduleSave()
            logger.info("Loaded \(self.sessions.count) sessions")
        } catch {
            self.error = error.localizedDescription
            throw SessionError.loadFailed(error.localizedDescription)
        }
    }

    /// Refresh sessions from the server
    func refresh() async {
        do {
            try await loadSessions()
        } catch {
            logger.warning("Failed to refresh sessions: \(error.localizedDescription)")
        }
    }

    private func parseSession(from dict: [String: Any]) -> ChatSession? {
        guard let id = dict["id"] as? String else {
            return nil
        }

        var session = ChatSession(id: id)
        session.title = dict["title"] as? String
        session.messageCount = dict["message_count"] as? Int ?? 0
        session.lastMessage = dict["last_message"] as? String
        session.model = dict["model"] as? String
        session.provider = dict["provider"] as? String
        session.isActive = dict["is_active"] as? Bool ?? true

        if let createdStr = dict["created_at"] as? String {
            session.createdAt = ISO8601DateFormatter().date(from: createdStr) ?? Date()
        }
        if let updatedStr = dict["updated_at"] as? String {
            session.updatedAt = ISO8601DateFormatter().date(from: updatedStr) ?? Date()
        }

        return session
    }

    // MARK: - Persistence

    private static func stateDirectory() -> URL {
        let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        return appSupport.appendingPathComponent("Nexus", isDirectory: true)
    }

    private static func fileURL() -> URL {
        stateDirectory().appendingPathComponent("chat-sessions.json")
    }

    private func loadFromDisk() {
        let url = Self.fileURL()
        guard FileManager.default.fileExists(atPath: url.path) else {
            logger.debug("No existing sessions file at \(url.path)")
            return
        }

        do {
            let data = try Data(contentsOf: url)
            let decoder = JSONDecoder()
            decoder.dateDecodingStrategy = .iso8601

            struct PersistedState: Decodable {
                let sessions: [ChatSession]
                let activeSessionId: String?
            }

            let state = try decoder.decode(PersistedState.self, from: data)
            sessions = state.sessions
            activeSessionId = state.activeSessionId

            logger.info("Loaded \(self.sessions.count) sessions from disk")
        } catch {
            logger.warning("Failed to load sessions: \(error.localizedDescription, privacy: .public)")
        }
    }

    private func scheduleSave() {
        saveTask?.cancel()
        saveTask = Task { [weak self] in
            try? await Task.sleep(nanoseconds: (self?.saveDebounceMs ?? 500) * 1_000_000)
            guard !Task.isCancelled else { return }
            self?.saveToDisk()
        }
    }

    private func saveToDisk() {
        let url = Self.fileURL()

        do {
            try FileManager.default.createDirectory(
                at: url.deletingLastPathComponent(),
                withIntermediateDirectories: true
            )

            struct PersistedState: Encodable {
                let sessions: [ChatSession]
                let activeSessionId: String?
            }

            let state = PersistedState(
                sessions: sessions,
                activeSessionId: activeSessionId
            )

            let encoder = JSONEncoder()
            encoder.dateEncodingStrategy = .iso8601
            encoder.outputFormatting = [.prettyPrinted, .sortedKeys]

            let data = try encoder.encode(state)
            try data.write(to: url, options: [.atomic])
            try? FileManager.default.setAttributes(
                [.posixPermissions: 0o600],
                ofItemAtPath: url.path
            )

            logger.debug("Saved \(self.sessions.count) sessions to disk")
        } catch {
            logger.error("Failed to save sessions: \(error.localizedDescription, privacy: .public)")
        }
    }

    // MARK: - Helpers

    /// Get a session by ID
    func session(for id: String) -> ChatSession? {
        sessions.first { $0.id == id }
    }

    /// Check if a session exists
    func hasSession(_ id: String) -> Bool {
        sessions.contains { $0.id == id }
    }

    /// Get the most recent session
    var mostRecentSession: ChatSession? {
        sessions.max { $0.updatedAt < $1.updatedAt }
    }

    /// Clear all sessions
    func clearAllSessions() {
        sessions.removeAll()
        activeSessionId = nil
        ChatHistoryManager.shared.clearAllHistory()
        scheduleSave()
        logger.info("Cleared all sessions")
    }

    // MARK: - Testing Support

    #if DEBUG
    static func _testClear() async {
        await MainActor.run {
            shared.sessions.removeAll()
            shared.activeSessionId = nil
        }
    }

    static func _testCreate(_ session: ChatSession) async {
        await MainActor.run {
            shared.sessions.append(session)
            if shared.activeSessionId == nil {
                shared.activeSessionId = session.id
            }
        }
    }
    #endif
}
