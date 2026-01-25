import Foundation
import OSLog

/// Persistent memory for conversations and context.
/// Enables continuity across sessions and personalization.
@MainActor
@Observable
final class ConversationMemory {
    static let shared = ConversationMemory()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "memory")

    private(set) var conversations: [ConversationRecord] = []
    private(set) var facts: [MemoryFact] = []
    private(set) var preferences: [String: String] = [:]

    struct ConversationRecord: Identifiable, Codable {
        let id: String
        var title: String?
        var summary: String?
        var messages: [Message]
        var metadata: ConversationMetadata
        let createdAt: Date
        var updatedAt: Date

        struct Message: Codable {
            let id: String
            let role: Role
            let content: String
            let timestamp: Date

            enum Role: String, Codable {
                case user
                case assistant
                case system
            }
        }

        struct ConversationMetadata: Codable {
            var model: String?
            var provider: String?
            var totalTokens: Int?
            var tags: [String]
            var starred: Bool
        }
    }

    struct MemoryFact: Identifiable, Codable {
        let id: String
        let category: Category
        var content: String
        var confidence: Double
        let learnedFrom: String?
        let createdAt: Date
        var lastUsedAt: Date
        var useCount: Int

        enum Category: String, Codable, CaseIterable {
            case userPreference
            case projectInfo
            case codeStyle
            case personalInfo
            case workContext
            case technicalFact
        }
    }

    // MARK: - Conversation Management

    /// Create a new conversation
    func createConversation(title: String? = nil) -> ConversationRecord {
        let conversation = ConversationRecord(
            id: UUID().uuidString,
            title: title,
            summary: nil,
            messages: [],
            metadata: ConversationRecord.ConversationMetadata(tags: [], starred: false),
            createdAt: Date(),
            updatedAt: Date()
        )

        conversations.insert(conversation, at: 0)
        persistConversations()
        logger.info("conversation created id=\(conversation.id)")

        return conversation
    }

    /// Add message to conversation
    func addMessage(to conversationId: String, role: ConversationRecord.Message.Role, content: String) {
        guard let index = conversations.firstIndex(where: { $0.id == conversationId }) else { return }

        let message = ConversationRecord.Message(
            id: UUID().uuidString,
            role: role,
            content: content,
            timestamp: Date()
        )

        conversations[index].messages.append(message)
        conversations[index].updatedAt = Date()

        // Auto-generate title from first user message
        if conversations[index].title == nil && role == .user {
            conversations[index].title = String(content.prefix(50))
        }
    }

    /// Update conversation summary
    func updateSummary(for conversationId: String, summary: String) {
        guard let index = conversations.firstIndex(where: { $0.id == conversationId }) else { return }
        conversations[index].summary = summary
        conversations[index].updatedAt = Date()
        persistConversations()
    }

    /// Star/unstar conversation
    func toggleStar(conversationId: String) {
        guard let index = conversations.firstIndex(where: { $0.id == conversationId }) else { return }
        conversations[index].metadata.starred.toggle()
        persistConversations()
    }

    /// Delete conversation
    func deleteConversation(id: String) {
        conversations.removeAll { $0.id == id }
        persistConversations()
        logger.info("conversation deleted id=\(id)")
    }

    /// Search conversations
    func searchConversations(query: String) -> [ConversationRecord] {
        let lowercased = query.lowercased()
        return conversations.filter { conv in
            conv.title?.lowercased().contains(lowercased) == true ||
            conv.summary?.lowercased().contains(lowercased) == true ||
            conv.messages.contains { $0.content.lowercased().contains(lowercased) }
        }
    }

    /// Get recent conversations
    func recentConversations(limit: Int = 20) -> [ConversationRecord] {
        Array(conversations.prefix(limit))
    }

    /// Get starred conversations
    func starredConversations() -> [ConversationRecord] {
        conversations.filter { $0.metadata.starred }
    }

    // MARK: - Memory Facts

    /// Learn a new fact
    func learn(fact content: String, category: MemoryFact.Category, source: String? = nil, confidence: Double = 0.8) {
        // Check for existing similar fact
        if let existing = facts.first(where: { $0.content.lowercased() == content.lowercased() }) {
            updateFactUsage(existing.id)
            return
        }

        let fact = MemoryFact(
            id: UUID().uuidString,
            category: category,
            content: content,
            confidence: confidence,
            learnedFrom: source,
            createdAt: Date(),
            lastUsedAt: Date(),
            useCount: 1
        )

        facts.append(fact)
        persistFacts()
        logger.info("fact learned category=\(category.rawValue)")
    }

    /// Recall facts by category
    func recall(category: MemoryFact.Category) -> [MemoryFact] {
        facts.filter { $0.category == category }
            .sorted { $0.useCount > $1.useCount }
    }

    /// Recall all relevant facts
    func recallAll() -> [MemoryFact] {
        facts.sorted { $0.confidence > $1.confidence }
    }

    /// Search facts
    func searchFacts(query: String) -> [MemoryFact] {
        let lowercased = query.lowercased()
        return facts.filter { $0.content.lowercased().contains(lowercased) }
    }

    /// Update fact usage
    func updateFactUsage(_ factId: String) {
        guard let index = facts.firstIndex(where: { $0.id == factId }) else { return }
        facts[index].lastUsedAt = Date()
        facts[index].useCount += 1
    }

    /// Forget a fact
    func forget(factId: String) {
        facts.removeAll { $0.id == factId }
        persistFacts()
    }

    // MARK: - Preferences

    /// Set a preference
    func setPreference(_ key: String, value: String) {
        preferences[key] = value
        persistPreferences()
    }

    /// Get a preference
    func getPreference(_ key: String) -> String? {
        preferences[key]
    }

    /// Remove a preference
    func removePreference(_ key: String) {
        preferences.removeValue(forKey: key)
        persistPreferences()
    }

    // MARK: - Context Generation

    /// Generate context summary for AI
    func generateContext() -> String {
        var context = "# Memory Context\n\n"

        // Add relevant facts
        let relevantFacts = facts.sorted { $0.useCount > $1.useCount }.prefix(20)
        if !relevantFacts.isEmpty {
            context += "## Known Facts\n"
            for fact in relevantFacts {
                context += "- [\(fact.category.rawValue)] \(fact.content)\n"
            }
            context += "\n"
        }

        // Add preferences
        if !preferences.isEmpty {
            context += "## User Preferences\n"
            for (key, value) in preferences {
                context += "- \(key): \(value)\n"
            }
            context += "\n"
        }

        // Add recent conversation context
        if let recent = conversations.first, let summary = recent.summary {
            context += "## Recent Conversation\n"
            context += summary
            context += "\n"
        }

        return context
    }

    // MARK: - Persistence

    private func persistConversations() {
        let url = conversationsURL()
        // Only save metadata and summaries, not full messages
        let summaries = conversations.map { conv in
            ConversationRecord(
                id: conv.id,
                title: conv.title,
                summary: conv.summary,
                messages: conv.messages.suffix(5).map { $0 }, // Last 5 messages only
                metadata: conv.metadata,
                createdAt: conv.createdAt,
                updatedAt: conv.updatedAt
            )
        }

        do {
            let data = try JSONEncoder().encode(summaries)
            try data.write(to: url)
        } catch {
            logger.error("failed to persist conversations: \(error.localizedDescription)")
        }
    }

    private func persistFacts() {
        let url = factsURL()
        do {
            let data = try JSONEncoder().encode(facts)
            try data.write(to: url)
        } catch {
            logger.error("failed to persist facts: \(error.localizedDescription)")
        }
    }

    private func persistPreferences() {
        let url = preferencesURL()
        do {
            let data = try JSONEncoder().encode(preferences)
            try data.write(to: url)
        } catch {
            logger.error("failed to persist preferences: \(error.localizedDescription)")
        }
    }

    func loadMemory() {
        // Load conversations
        if let data = try? Data(contentsOf: conversationsURL()),
           let loaded = try? JSONDecoder().decode([ConversationRecord].self, from: data) {
            conversations = loaded
        }

        // Load facts
        if let data = try? Data(contentsOf: factsURL()),
           let loaded = try? JSONDecoder().decode([MemoryFact].self, from: data) {
            facts = loaded
        }

        // Load preferences
        if let data = try? Data(contentsOf: preferencesURL()),
           let loaded = try? JSONDecoder().decode([String: String].self, from: data) {
            preferences = loaded
        }

        logger.debug("memory loaded conversations=\(self.conversations.count) facts=\(self.facts.count)")
    }

    private func memoryDir() -> URL {
        let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        let dir = appSupport.appendingPathComponent("Nexus/Memory")
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir
    }

    private func conversationsURL() -> URL { memoryDir().appendingPathComponent("conversations.json") }
    private func factsURL() -> URL { memoryDir().appendingPathComponent("facts.json") }
    private func preferencesURL() -> URL { memoryDir().appendingPathComponent("preferences.json") }
}
