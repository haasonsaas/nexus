import Foundation
import OSLog

// MARK: - Chat View Model

/// View model for chat UI.
/// Manages chat state, messaging, and event handling.
@MainActor
@Observable
final class ChatViewModel {
    private let logger = Logger(subsystem: "com.nexus.mac", category: "chat-viewmodel")

    // Managers
    private let sessionManager = ChatSessionManager.shared
    private let historyManager = ChatHistoryManager.shared
    private let eventStream = ChatEventStream.shared
    private let toolTracker = ToolCallTracker.shared

    // State
    private(set) var messages: [ChatMessage] = []
    private(set) var isLoading = false
    private(set) var isSending = false
    private(set) var isConnected = false
    private(set) var error: String?
    private(set) var lastError: Date?

    // Input
    var inputText = ""
    var attachments: [ChatAttachment] = []

    // Streaming state (forwarded from event stream)
    var streamingText: String { eventStream.streamingText }
    var isStreaming: Bool { eventStream.isStreamingAssistant }

    // Session state (forwarded from session manager)
    var activeSessionId: String? { sessionManager.activeSessionId }
    var sessions: [ChatSession] { sessionManager.sessions }
    var activeSession: ChatSession? { sessionManager.activeSession }

    // Tool tracking
    var activeToolCalls: [ToolCallTracker.ToolCall] { toolTracker.activeToolCalls }
    var hasActiveToolCalls: Bool { !toolTracker.activeToolCalls.isEmpty }

    // Event subscription
    private var eventTask: Task<Void, Never>?

    // MARK: - Initialization

    init() {
        startEventSubscription()
        loadInitialState()
    }

    deinit {
        Task { @MainActor [weak self] in
            self?.eventTask?.cancel()
        }
    }

    private func loadInitialState() {
        Task {
            await loadSessionsIfNeeded()
            await loadHistoryIfNeeded()
        }
    }

    private func loadSessionsIfNeeded() async {
        if sessions.isEmpty {
            await sessionManager.refresh()
        }
    }

    private func loadHistoryIfNeeded() async {
        if let sessionId = activeSessionId {
            messages = historyManager.messagesForSession(sessionId)
        }
    }

    // MARK: - Event Subscription

    private func startEventSubscription() {
        eventTask?.cancel()
        eventTask = Task { [weak self] in
            guard let self else { return }

            for await event in self.eventStream.subscribe() {
                guard !Task.isCancelled else { break }
                await self.handleEvent(event)
            }
        }
    }

    private func handleEvent(_ event: ChatTransportEvent) async {
        switch event {
        case .chat(let message):
            handleChatMessage(message)

        case .agent(let agentEvent):
            handleAgentEvent(agentEvent)

        case .seqGap(let expected, let received):
            handleSeqGap(expected: expected, received: received)

        case .health(let health):
            handleHealthState(health)

        case .error(let err):
            handleError(err)

        case .tick:
            // Periodic tick, can be used for UI updates
            break
        }
    }

    private func handleChatMessage(_ message: ChatMessage) {
        guard message.sessionId == activeSessionId else { return }

        historyManager.appendMessage(message)
        messages = historyManager.messagesForSession(message.sessionId)

        logger.debug("Received message id=\(message.id) role=\(message.role.rawValue)")
    }

    private func handleAgentEvent(_ event: AgentEvent) {
        toolTracker.processAgentEvent(event)

        // If this is a message event, check if we need to update
        if event.stream == "message" || event.stream == "output" {
            if let content = event.data?["content"]?.value as? String,
               let sessionId = event.data?["session_id"]?.value as? String,
               sessionId == activeSessionId {
                // This might be a partial update, handled by streaming
                logger.debug("Agent message event: \(event.stream ?? "unknown")")
            }
        }
    }

    private func handleSeqGap(expected: Int64, received: Int64) {
        logger.warning("Sequence gap detected: expected=\(expected) received=\(received)")

        // Reload history to recover from sequence gap
        Task {
            await loadHistory()
        }
    }

    private func handleHealthState(_ health: ChatHealthState) {
        switch health.state {
        case .ok:
            isConnected = true
            if error != nil && lastError != nil && Date().timeIntervalSince(lastError!) > 5 {
                clearError()
            }

        case .degraded:
            isConnected = false
            if let summary = health.summary {
                setError(summary)
            }

        case .linkingNeeded:
            isConnected = false
            setError("Linking required")

        case .unknown:
            isConnected = false
        }
    }

    private func handleError(_ err: Error) {
        setError(err.localizedDescription)
    }

    // MARK: - Actions

    /// Send the current input as a message
    func send() async {
        let content = inputText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !content.isEmpty else { return }

        // Create session if needed
        if activeSessionId == nil {
            do {
                _ = try await sessionManager.createSession()
            } catch {
                setError(error.localizedDescription)
                return
            }
        }

        isSending = true
        clearError()

        let currentAttachments = attachments.isEmpty ? nil : attachments
        inputText = ""
        attachments = []

        do {
            try await sessionManager.send(content: content, attachments: currentAttachments)
            logger.debug("Message sent successfully")
        } catch {
            setError(error.localizedDescription)
            // Restore input on failure
            inputText = content
            if let atts = currentAttachments {
                attachments = atts
            }
        }

        isSending = false
    }

    /// Abort current generation
    func abort() async {
        do {
            try await sessionManager.abort()
            eventStream.clearStreamingText()
            toolTracker.clearAll()
            logger.info("Generation aborted")
        } catch {
            setError(error.localizedDescription)
        }
    }

    /// Load chat history for the active session
    func loadHistory() async {
        guard let sessionId = activeSessionId else { return }

        isLoading = true
        defer { isLoading = false }

        do {
            messages = try await historyManager.loadHistory(sessionId: sessionId)
            logger.debug("Loaded \(self.messages.count) messages")
        } catch {
            setError(error.localizedDescription)
        }
    }

    /// Refresh history from the server
    func refreshHistory() async {
        await loadHistory()
    }

    // MARK: - Session Management

    /// Switch to a different session
    func switchSession(_ sessionId: String) async {
        do {
            try await sessionManager.switchSession(sessionId)
            messages = historyManager.messagesForSession(sessionId)
            eventStream.clearStreamingText()
            toolTracker.clearAll()
            clearError()
            logger.info("Switched to session \(sessionId)")
        } catch {
            setError(error.localizedDescription)
        }
    }

    /// Create a new session
    func newSession(title: String? = nil) async {
        do {
            _ = try await sessionManager.createSession(title: title)
            messages = []
            eventStream.clearStreamingText()
            toolTracker.clearAll()
            clearError()
            logger.info("Created new session")
        } catch {
            setError(error.localizedDescription)
        }
    }

    /// Close the active session
    func closeActiveSession() {
        guard let sessionId = activeSessionId else { return }
        sessionManager.closeSession(sessionId)
        messages = []
        eventStream.clearStreamingText()
        toolTracker.clearAll()
    }

    /// Close a specific session
    func closeSession(_ sessionId: String) {
        sessionManager.closeSession(sessionId)
        if sessionId == activeSessionId {
            messages = []
            eventStream.clearStreamingText()
            toolTracker.clearAll()
        }
    }

    /// Rename a session
    func renameSession(_ sessionId: String, title: String) {
        sessionManager.updateSession(sessionId, title: title)
    }

    // MARK: - Attachments

    /// Add an attachment
    func addAttachment(_ attachment: ChatAttachment) {
        attachments.append(attachment)
        logger.debug("Added attachment: \(attachment.name)")
    }

    /// Add an image attachment from data
    func addImageAttachment(data: Data, name: String, mimeType: String = "image/png") {
        let attachment = ChatAttachment(
            type: .image,
            name: name,
            data: data,
            mimeType: mimeType
        )
        addAttachment(attachment)
    }

    /// Add a file attachment from data
    func addFileAttachment(data: Data, name: String, mimeType: String? = nil) {
        let attachment = ChatAttachment(
            type: .file,
            name: name,
            data: data,
            mimeType: mimeType
        )
        addAttachment(attachment)
    }

    /// Remove an attachment by ID
    func removeAttachment(_ id: String) {
        attachments.removeAll { $0.id == id }
        logger.debug("Removed attachment: \(id)")
    }

    /// Clear all attachments
    func clearAttachments() {
        attachments.removeAll()
    }

    // MARK: - Error Handling

    /// Set an error message
    private func setError(_ message: String) {
        error = message
        lastError = Date()
        logger.error("Error: \(message)")
    }

    /// Clear the current error
    func clearError() {
        error = nil
        lastError = nil
    }

    /// Retry the last failed action
    func retry() async {
        clearError()
        if !inputText.isEmpty {
            await send()
        }
    }

    // MARK: - Computed Properties

    /// Whether a message can be sent
    var canSend: Bool {
        !inputText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty &&
        !isSending &&
        !isStreaming
    }

    /// Whether there are any messages or streaming content
    var hasContent: Bool {
        !messages.isEmpty || isStreaming
    }

    /// Get messages including the current streaming message (if any)
    var displayMessages: [ChatMessage] {
        var result = messages

        // Add streaming message if active
        if isStreaming, let sessionId = activeSessionId {
            let streamingMessage = ChatMessage(
                id: "streaming-\(UUID().uuidString)",
                sessionId: sessionId,
                role: .assistant,
                content: streamingText,
                timestamp: Date(),
                metadata: nil
            )
            result.append(streamingMessage)
        }

        return result
    }

    /// Get the session title for display
    var sessionTitle: String {
        activeSession?.title ?? "New Chat"
    }

    /// Get recent sessions for the session picker
    var recentSessions: [ChatSession] {
        sessionManager.recentSessions
    }
}

// MARK: - Keyboard Shortcuts

extension ChatViewModel {
    /// Handle keyboard shortcut for sending
    func handleSendShortcut() {
        Task {
            await send()
        }
    }

    /// Handle keyboard shortcut for aborting
    func handleAbortShortcut() {
        if isStreaming || isSending {
            Task {
                await abort()
            }
        }
    }

    /// Handle keyboard shortcut for new session
    func handleNewSessionShortcut() {
        Task {
            await newSession()
        }
    }
}

// MARK: - State Restoration

extension ChatViewModel {
    /// Save current state for restoration
    func saveState() -> ChatViewState {
        ChatViewState(
            activeSessionId: activeSessionId,
            inputText: inputText
        )
    }

    /// Restore state from saved state
    func restoreState(_ state: ChatViewState) {
        inputText = state.inputText

        if let sessionId = state.activeSessionId {
            Task {
                await switchSession(sessionId)
            }
        }
    }

    struct ChatViewState: Codable {
        let activeSessionId: String?
        let inputText: String
    }
}

// MARK: - Debug Support

#if DEBUG
extension ChatViewModel {
    /// Add a test message (debug only)
    func _addTestMessage(role: ChatMessage.MessageRole, content: String) {
        guard let sessionId = activeSessionId else { return }

        let message = ChatMessage(
            id: UUID().uuidString,
            sessionId: sessionId,
            role: role,
            content: content,
            timestamp: Date(),
            metadata: nil
        )

        historyManager.appendMessage(message)
        messages = historyManager.messagesForSession(sessionId)
    }

    /// Simulate streaming (debug only)
    func _simulateStreaming(_ text: String, delay: TimeInterval = 0.05) {
        Task {
            for char in text {
                eventStream.clearStreamingText()
                // Simulate streaming would require direct access to event stream internals
                // This is just a placeholder for debug purposes
                try? await Task.sleep(nanoseconds: UInt64(delay * 1_000_000_000))
                _ = char
            }
        }
    }
}
#endif
