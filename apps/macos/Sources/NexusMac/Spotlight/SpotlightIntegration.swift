import CoreSpotlight
import Foundation
import OSLog

/// Integrates with macOS Spotlight for searching conversations and prompts.
/// Makes nexus content discoverable system-wide.
@MainActor
final class SpotlightIntegration {
    static let shared = SpotlightIntegration()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "spotlight")

    private let conversationDomain = "com.nexus.conversation"
    private let promptDomain = "com.nexus.prompt"
    private let factDomain = "com.nexus.fact"

    // MARK: - Indexing

    /// Index a conversation for Spotlight search
    func indexConversation(_ conversation: ConversationMemory.ConversationRecord) {
        let attributeSet = CSSearchableItemAttributeSet(contentType: .text)
        attributeSet.title = conversation.title ?? "Conversation"
        attributeSet.contentDescription = conversation.summary ?? conversation.messages.last?.content
        attributeSet.keywords = conversation.metadata.tags
        attributeSet.kind = "AI Conversation"
        attributeSet.creator = "Nexus"

        if let model = conversation.metadata.model {
            attributeSet.addedDate = conversation.createdAt
            attributeSet.contentModificationDate = conversation.updatedAt
        }

        let item = CSSearchableItem(
            uniqueIdentifier: "\(conversationDomain).\(conversation.id)",
            domainIdentifier: conversationDomain,
            attributeSet: attributeSet
        )

        CSSearchableIndex.default().indexSearchableItems([item]) { [weak self] error in
            if let error {
                self?.logger.error("failed to index conversation: \(error.localizedDescription)")
            }
        }
    }

    /// Index a prompt template for Spotlight search
    func indexPrompt(_ prompt: PromptLibrary.PromptTemplate) {
        let attributeSet = CSSearchableItemAttributeSet(contentType: .text)
        attributeSet.title = prompt.name
        attributeSet.contentDescription = prompt.description ?? prompt.content.prefix(200).description
        attributeSet.keywords = prompt.tags
        attributeSet.kind = "Prompt Template"
        attributeSet.creator = "Nexus"

        let item = CSSearchableItem(
            uniqueIdentifier: "\(promptDomain).\(prompt.id)",
            domainIdentifier: promptDomain,
            attributeSet: attributeSet
        )

        CSSearchableIndex.default().indexSearchableItems([item]) { [weak self] error in
            if let error {
                self?.logger.error("failed to index prompt: \(error.localizedDescription)")
            }
        }
    }

    /// Index a memory fact for Spotlight search
    func indexFact(_ fact: ConversationMemory.MemoryFact) {
        let attributeSet = CSSearchableItemAttributeSet(contentType: .text)
        attributeSet.title = fact.category.rawValue.capitalized
        attributeSet.contentDescription = fact.content
        attributeSet.keywords = [fact.category.rawValue]
        attributeSet.kind = "Memory Fact"
        attributeSet.creator = "Nexus"
        attributeSet.addedDate = fact.createdAt

        let item = CSSearchableItem(
            uniqueIdentifier: "\(factDomain).\(fact.id)",
            domainIdentifier: factDomain,
            attributeSet: attributeSet
        )

        CSSearchableIndex.default().indexSearchableItems([item]) { [weak self] error in
            if let error {
                self?.logger.error("failed to index fact: \(error.localizedDescription)")
            }
        }
    }

    // MARK: - Batch Indexing

    /// Index all conversations
    func indexAllConversations() {
        let conversations = ConversationMemory.shared.conversations

        let items = conversations.map { conversation -> CSSearchableItem in
            let attributeSet = CSSearchableItemAttributeSet(contentType: .text)
            attributeSet.title = conversation.title ?? "Conversation"
            attributeSet.contentDescription = conversation.summary ?? conversation.messages.last?.content
            attributeSet.keywords = conversation.metadata.tags
            attributeSet.kind = "AI Conversation"
            attributeSet.creator = "Nexus"
            attributeSet.addedDate = conversation.createdAt
            attributeSet.contentModificationDate = conversation.updatedAt

            return CSSearchableItem(
                uniqueIdentifier: "\(conversationDomain).\(conversation.id)",
                domainIdentifier: conversationDomain,
                attributeSet: attributeSet
            )
        }

        CSSearchableIndex.default().indexSearchableItems(items) { [weak self] error in
            if let error {
                self?.logger.error("failed to index conversations: \(error.localizedDescription)")
            } else {
                self?.logger.info("indexed \(items.count) conversations")
            }
        }
    }

    /// Index all prompts
    func indexAllPrompts() {
        let prompts = PromptLibrary.shared.prompts

        let items = prompts.map { prompt -> CSSearchableItem in
            let attributeSet = CSSearchableItemAttributeSet(contentType: .text)
            attributeSet.title = prompt.name
            attributeSet.contentDescription = prompt.description ?? prompt.content.prefix(200).description
            attributeSet.keywords = prompt.tags
            attributeSet.kind = "Prompt Template"
            attributeSet.creator = "Nexus"

            return CSSearchableItem(
                uniqueIdentifier: "\(promptDomain).\(prompt.id)",
                domainIdentifier: promptDomain,
                attributeSet: attributeSet
            )
        }

        CSSearchableIndex.default().indexSearchableItems(items) { [weak self] error in
            if let error {
                self?.logger.error("failed to index prompts: \(error.localizedDescription)")
            } else {
                self?.logger.info("indexed \(items.count) prompts")
            }
        }
    }

    // MARK: - Removal

    /// Remove a conversation from Spotlight index
    func removeConversation(id: String) {
        CSSearchableIndex.default().deleteSearchableItems(
            withIdentifiers: ["\(conversationDomain).\(id)"]
        ) { [weak self] error in
            if let error {
                self?.logger.error("failed to remove conversation from index: \(error.localizedDescription)")
            }
        }
    }

    /// Remove a prompt from Spotlight index
    func removePrompt(id: String) {
        CSSearchableIndex.default().deleteSearchableItems(
            withIdentifiers: ["\(promptDomain).\(id)"]
        ) { [weak self] error in
            if let error {
                self?.logger.error("failed to remove prompt from index: \(error.localizedDescription)")
            }
        }
    }

    /// Clear all Spotlight indexes
    func clearAllIndexes() {
        CSSearchableIndex.default().deleteAllSearchableItems { [weak self] error in
            if let error {
                self?.logger.error("failed to clear indexes: \(error.localizedDescription)")
            } else {
                self?.logger.info("cleared all spotlight indexes")
            }
        }
    }

    /// Clear indexes for a specific domain
    func clearDomain(_ domain: String) {
        CSSearchableIndex.default().deleteSearchableItems(
            withDomainIdentifiers: [domain]
        ) { [weak self] error in
            if let error {
                self?.logger.error("failed to clear domain \(domain): \(error.localizedDescription)")
            }
        }
    }

    // MARK: - Search Continuation

    /// Handle Spotlight search continuation
    func handleSearchContinuation(userActivity: NSUserActivity) -> String? {
        guard userActivity.activityType == CSSearchableItemActionType,
              let identifier = userActivity.userInfo?[CSSearchableItemActivityIdentifier] as? String else {
            return nil
        }

        // Extract the ID from the identifier
        if identifier.hasPrefix(conversationDomain) {
            return identifier.replacingOccurrences(of: "\(conversationDomain).", with: "")
        } else if identifier.hasPrefix(promptDomain) {
            return identifier.replacingOccurrences(of: "\(promptDomain).", with: "")
        } else if identifier.hasPrefix(factDomain) {
            return identifier.replacingOccurrences(of: "\(factDomain).", with: "")
        }

        return nil
    }
}
