import Foundation
import OSLog

/// Pre-warms and provides session previews for the menu bar.
/// Acts as a data layer between SessionBridge/ConversationMemory and MenuBarView.
@MainActor
@Observable
final class MenuSessionsInjector {
    static let shared = MenuSessionsInjector()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "menu-sessions")

    // MARK: - Session Preview Model

    struct SessionPreview: Identifiable, Equatable {
        let id: String
        var title: String
        var lastMessageTimestamp: Date
        var agentType: AgentType
        var status: Status
        var lastUserMessage: String?
        var lastAssistantResponse: String?

        enum AgentType: String, CaseIterable {
            case chat
            case voice
            case agent
            case computerUse
            case mcp

            var icon: String {
                switch self {
                case .chat: return "message"
                case .voice: return "mic"
                case .agent: return "cpu"
                case .computerUse: return "desktopcomputer"
                case .mcp: return "puzzlepiece"
                }
            }

            var displayName: String {
                switch self {
                case .chat: return "Chat"
                case .voice: return "Voice"
                case .agent: return "Agent"
                case .computerUse: return "Computer Use"
                case .mcp: return "MCP"
                }
            }
        }

        enum Status: String, CaseIterable {
            case active
            case paused
            case completed

            var color: String {
                switch self {
                case .active: return "green"
                case .paused: return "orange"
                case .completed: return "secondary"
                }
            }
        }

        /// Truncated preview of the last user message
        var userMessagePreview: String? {
            guard let message = lastUserMessage else { return nil }
            return truncate(message, maxLength: 80)
        }

        /// Truncated preview of the last assistant response
        var assistantResponsePreview: String? {
            guard let response = lastAssistantResponse else { return nil }
            return truncate(response, maxLength: 100)
        }

        /// Human-readable time since last activity
        var timeSinceActivity: String {
            let formatter = RelativeDateTimeFormatter()
            formatter.unitsStyle = .abbreviated
            return formatter.localizedString(for: lastMessageTimestamp, relativeTo: Date())
        }

        private func truncate(_ text: String, maxLength: Int) -> String {
            let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
            if trimmed.count <= maxLength {
                return trimmed
            }
            let index = trimmed.index(trimmed.startIndex, offsetBy: maxLength - 1)
            return String(trimmed[..<index]) + "…"
        }
    }

    // MARK: - State

    private(set) var cachedPreviews: [String: SessionPreview] = [:]
    private(set) var isLoading: Bool = false
    private(set) var lastRefreshed: Date?

    private var refreshTask: Task<Void, Never>?
    private var autoRefreshTask: Task<Void, Never>?
    private var isMenuOpen: Bool = false

    private let maxCompletedSessions = 5
    private let autoRefreshInterval: TimeInterval = 30

    private init() {}

    // MARK: - Computed Properties

    /// Sorted list of session previews for menu display.
    /// Active sessions first (sorted by most recent activity), then recent completed sessions.
    var menuItems: [SessionPreview] {
        let previews = Array(cachedPreviews.values)

        // Separate active and completed
        let activeSessions = previews
            .filter { $0.status == .active || $0.status == .paused }
            .sorted { $0.lastMessageTimestamp > $1.lastMessageTimestamp }

        let completedSessions = previews
            .filter { $0.status == .completed }
            .sorted { $0.lastMessageTimestamp > $1.lastMessageTimestamp }
            .prefix(maxCompletedSessions)

        return activeSessions + completedSessions
    }

    /// Count of active sessions
    var activeSessionCount: Int {
        cachedPreviews.values.filter { $0.status == .active }.count
    }

    /// Count of paused sessions
    var pausedSessionCount: Int {
        cachedPreviews.values.filter { $0.status == .paused }.count
    }

    // MARK: - Public Methods

    /// Load and cache session previews from SessionBridge and ConversationMemory.
    func inject() {
        guard !isLoading else {
            logger.debug("inject skipped - already loading")
            return
        }

        isLoading = true
        logger.debug("injecting session previews")

        // Get sessions from SessionBridge
        let sessions = SessionBridge.shared.activeSessions

        // Build previews
        var newPreviews: [String: SessionPreview] = [:]

        for session in sessions {
            let preview = buildPreview(from: session)
            newPreviews[session.id] = preview
        }

        // Merge with conversation memory for completed sessions
        let conversations = ConversationMemory.shared.recentConversations(limit: maxCompletedSessions)
        for conversation in conversations {
            // Skip if already tracked as active
            if newPreviews[conversation.id] != nil { continue }

            let preview = buildPreview(from: conversation)
            newPreviews[conversation.id] = preview
        }

        cachedPreviews = newPreviews
        lastRefreshed = Date()
        isLoading = false

        logger.info("injected \(newPreviews.count) session previews")
    }

    /// Update cached previews from SessionBridge without full reload.
    func refresh() {
        refreshTask?.cancel()
        refreshTask = Task { @MainActor in
            logger.debug("refreshing session previews")

            // Update existing active sessions
            let sessions = SessionBridge.shared.activeSessions

            for session in sessions {
                if var existing = cachedPreviews[session.id] {
                    // Update mutable fields
                    existing.status = mapStatus(session.status)
                    existing.lastMessageTimestamp = session.lastActiveAt
                    if let title = session.metadata.title {
                        existing.title = title
                    }
                    cachedPreviews[session.id] = existing
                } else {
                    // New session, add it
                    let preview = buildPreview(from: session)
                    cachedPreviews[session.id] = preview
                }
            }

            // Remove stale active sessions
            let activeIds = Set(sessions.map(\.id))
            for (id, preview) in cachedPreviews {
                if preview.status != .completed && !activeIds.contains(id) {
                    // Mark as completed instead of removing
                    var updated = preview
                    updated.status = .completed
                    cachedPreviews[id] = updated
                }
            }

            // Prune old completed sessions
            pruneCompletedSessions()

            lastRefreshed = Date()
            logger.debug("refresh complete - \(self.cachedPreviews.count) sessions")
        }
    }

    /// Get preview data for a specific session.
    func previewFor(sessionId: String) -> SessionPreview? {
        if let cached = cachedPreviews[sessionId] {
            return cached
        }

        // Try to build on-demand from SessionBridge
        if let session = SessionBridge.shared.activeSessions.first(where: { $0.id == sessionId }) {
            let preview = buildPreview(from: session)
            cachedPreviews[sessionId] = preview
            return preview
        }

        // Try conversation memory
        if let conversation = ConversationMemory.shared.conversations.first(where: { $0.id == sessionId }) {
            let preview = buildPreview(from: conversation)
            cachedPreviews[sessionId] = preview
            return preview
        }

        return nil
    }

    // MARK: - Actions

    /// Resume a paused or completed session.
    func resumeSession(_ sessionId: String) {
        logger.info("resuming session: \(sessionId)")

        // Update local cache
        if var preview = cachedPreviews[sessionId] {
            preview.status = .active
            preview.lastMessageTimestamp = Date()
            cachedPreviews[sessionId] = preview
        }

        // Notify SessionBridge
        SessionBridge.shared.setPrimarySession(id: sessionId)

        // Open chat
        Task {
            WebChatManager.shared.show(sessionKey: sessionId)
        }
    }

    /// Delete a session.
    func deleteSession(_ sessionId: String) {
        logger.info("deleting session: \(sessionId)")

        // Remove from cache
        cachedPreviews.removeValue(forKey: sessionId)

        // End session in bridge
        SessionBridge.shared.endSession(id: sessionId, status: .completed)

        // Delete from conversation memory
        ConversationMemory.shared.deleteConversation(id: sessionId)
    }

    /// Duplicate a session (creates new session with same context).
    func duplicateSession(_ sessionId: String) {
        logger.info("duplicating session: \(sessionId)")

        guard let original = cachedPreviews[sessionId] else {
            logger.warning("cannot duplicate - session not found: \(sessionId)")
            return
        }

        // Create new session via bridge
        var metadata = SessionBridge.Session.SessionMetadata()
        metadata.title = "Copy of \(original.title)"

        let sessionType = mapAgentTypeToSessionType(original.agentType)
        let newSession = SessionBridge.shared.createSession(type: sessionType, metadata: metadata)

        // Add to cache
        let preview = SessionPreview(
            id: newSession.id,
            title: metadata.title ?? "New Session",
            lastMessageTimestamp: Date(),
            agentType: original.agentType,
            status: .active,
            lastUserMessage: nil,
            lastAssistantResponse: nil
        )
        cachedPreviews[newSession.id] = preview

        // Open the new session
        Task {
            WebChatManager.shared.show(sessionKey: newSession.id)
        }
    }

    // MARK: - Menu Lifecycle

    /// Called when menu opens - starts auto-refresh.
    func menuDidOpen() {
        isMenuOpen = true
        startAutoRefresh()
        refresh()
    }

    /// Called when menu closes - stops auto-refresh.
    func menuDidClose() {
        isMenuOpen = false
        stopAutoRefresh()
    }

    // MARK: - Private Helpers

    private func buildPreview(from session: SessionBridge.Session) -> SessionPreview {
        // Try to get message previews from conversation memory
        let conversation = ConversationMemory.shared.conversations.first { $0.id == session.id }

        let lastUserMessage = conversation?.messages
            .last { $0.role == .user }?
            .content

        let lastAssistantResponse = conversation?.messages
            .last { $0.role == .assistant }?
            .content

        return SessionPreview(
            id: session.id,
            title: session.metadata.title ?? generateTitle(from: session),
            lastMessageTimestamp: session.lastActiveAt,
            agentType: mapAgentType(session.type),
            status: mapStatus(session.status),
            lastUserMessage: lastUserMessage,
            lastAssistantResponse: lastAssistantResponse
        )
    }

    private func buildPreview(from conversation: ConversationMemory.ConversationRecord) -> SessionPreview {
        let lastUserMessage = conversation.messages
            .last { $0.role == .user }?
            .content

        let lastAssistantResponse = conversation.messages
            .last { $0.role == .assistant }?
            .content

        return SessionPreview(
            id: conversation.id,
            title: conversation.title ?? "Conversation",
            lastMessageTimestamp: conversation.updatedAt,
            agentType: .chat, // Default for memory-only conversations
            status: .completed,
            lastUserMessage: lastUserMessage,
            lastAssistantResponse: lastAssistantResponse
        )
    }

    private func generateTitle(from session: SessionBridge.Session) -> String {
        let formatter = DateFormatter()
        formatter.dateStyle = .short
        formatter.timeStyle = .short
        return "\(session.type.rawValue.capitalized) - \(formatter.string(from: session.createdAt))"
    }

    private func mapAgentType(_ type: SessionBridge.Session.SessionType) -> SessionPreview.AgentType {
        switch type {
        case .chat: return .chat
        case .voice: return .voice
        case .agent: return .agent
        case .computerUse: return .computerUse
        case .mcp: return .mcp
        }
    }

    private func mapAgentTypeToSessionType(_ agentType: SessionPreview.AgentType) -> SessionBridge.Session.SessionType {
        switch agentType {
        case .chat: return .chat
        case .voice: return .voice
        case .agent: return .agent
        case .computerUse: return .computerUse
        case .mcp: return .mcp
        }
    }

    private func mapStatus(_ status: SessionBridge.Session.SessionStatus) -> SessionPreview.Status {
        switch status {
        case .active: return .active
        case .paused: return .paused
        case .completed: return .completed
        case .error: return .paused // Treat error as paused for menu purposes
        }
    }

    private func pruneCompletedSessions() {
        let completedSessions = cachedPreviews.values
            .filter { $0.status == .completed }
            .sorted { $0.lastMessageTimestamp > $1.lastMessageTimestamp }

        if completedSessions.count > maxCompletedSessions {
            let toRemove = completedSessions.dropFirst(maxCompletedSessions)
            for session in toRemove {
                cachedPreviews.removeValue(forKey: session.id)
            }
        }
    }

    private func startAutoRefresh() {
        stopAutoRefresh()

        autoRefreshTask = Task { @MainActor [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(self?.autoRefreshInterval ?? 30))

                guard let self, self.isMenuOpen else { break }

                self.refresh()
                self.logger.debug("auto-refresh triggered")
            }
        }
    }

    private func stopAutoRefresh() {
        autoRefreshTask?.cancel()
        autoRefreshTask = nil
    }
}
