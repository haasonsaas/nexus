import AppKit
import Foundation
import OSLog

/// Manages Handoff between macOS and iOS devices.
/// Enables continuing conversations across devices.
@MainActor
@Observable
final class HandoffManager {
    static let shared = HandoffManager()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "handoff")

    private(set) var currentActivity: NSUserActivity?
    private(set) var isAdvertising = false

    let conversationActivityType = "com.nexus.conversation"
    let promptActivityType = "com.nexus.prompt"
    let contextActivityType = "com.nexus.context"

    // MARK: - Activity Creation

    /// Start advertising a conversation activity
    func advertiseConversation(id: String, title: String?, preview: String?) {
        let activity = NSUserActivity(activityType: conversationActivityType)
        activity.title = title ?? "Continue Conversation"
        activity.isEligibleForHandoff = true
        activity.isEligibleForSearch = true
        activity.isEligibleForPrediction = true

        activity.userInfo = [
            "conversationId": id,
            "title": title ?? "",
            "preview": preview ?? ""
        ]

        if let preview {
            activity.contentAttributeSet = createAttributeSet(
                title: title ?? "Conversation",
                description: preview
            )
        }

        activity.becomeCurrent()
        currentActivity = activity
        isAdvertising = true

        logger.info("advertising conversation id=\(id)")
    }

    /// Start advertising a prompt activity
    func advertisePrompt(id: String, name: String, content: String) {
        let activity = NSUserActivity(activityType: promptActivityType)
        activity.title = "Use Prompt: \(name)"
        activity.isEligibleForHandoff = true
        activity.isEligibleForSearch = true

        activity.userInfo = [
            "promptId": id,
            "name": name,
            "content": content
        ]

        activity.contentAttributeSet = createAttributeSet(
            title: name,
            description: String(content.prefix(200))
        )

        activity.becomeCurrent()
        currentActivity = activity
        isAdvertising = true

        logger.info("advertising prompt id=\(id)")
    }

    /// Start advertising current context
    func advertiseContext() {
        let context = ContextManager.shared.currentContext

        let activity = NSUserActivity(activityType: contextActivityType)
        activity.title = "Continue with Context"
        activity.isEligibleForHandoff = true

        var userInfo: [String: Any] = [:]
        if let app = context?.activeApp {
            userInfo["activeApp"] = app.name
            userInfo["bundleId"] = app.bundleId
        }
        if let window = context?.frontmostWindow {
            userInfo["windowTitle"] = window.title
        }
        if let clipboard = context?.clipboard, let preview = clipboard.textPreview {
            userInfo["clipboardPreview"] = preview
        }

        activity.userInfo = userInfo
        activity.becomeCurrent()
        currentActivity = activity
        isAdvertising = true

        logger.info("advertising context")
    }

    /// Stop advertising current activity
    func stopAdvertising() {
        currentActivity?.invalidate()
        currentActivity = nil
        isAdvertising = false
        logger.info("stopped advertising")
    }

    // MARK: - Activity Reception

    /// Handle incoming Handoff activity
    func handleActivity(_ activity: NSUserActivity) -> Bool {
        logger.info("received handoff activity type=\(activity.activityType)")

        switch activity.activityType {
        case conversationActivityType:
            return handleConversationActivity(activity)
        case promptActivityType:
            return handlePromptActivity(activity)
        case contextActivityType:
            return handleContextActivity(activity)
        default:
            return false
        }
    }

    private func handleConversationActivity(_ activity: NSUserActivity) -> Bool {
        guard let conversationId = activity.userInfo?["conversationId"] as? String else {
            return false
        }

        // Open the conversation
        Task {
            // Try to find existing session or create new one
            if let existingSession = SessionBridge.shared.activeSessions.first(where: { $0.id == conversationId }) {
                WebChatManager.shared.openChat(for: existingSession.id)
            } else {
                // Create new session and load conversation history from memory
                let session = SessionBridge.shared.createSession(type: .chat)

                // Load any saved memory context for this conversation
                if let memory = ConversationMemory.shared.searchMemories(query: conversationId).first {
                    WebChatManager.shared.openChat(for: session.id, withMessage: "Continue our previous conversation about: \(memory.content)")
                } else {
                    WebChatManager.shared.openChat(for: session.id)
                }
            }

            logger.info("handoff: opened conversation id=\(conversationId)")
        }

        return true
    }

    private func handlePromptActivity(_ activity: NSUserActivity) -> Bool {
        guard let promptId = activity.userInfo?["promptId"] as? String,
              let prompt = PromptLibrary.shared.prompts.first(where: { $0.id == promptId }) else {
            return false
        }

        // Open chat with prompt pre-filled
        Task {
            let session = SessionBridge.shared.createSession(type: .chat)

            // Expand prompt with no variables
            let content = PromptLibrary.shared.expand(prompt, variables: [:])

            // Open chat with prompt content as initial message
            WebChatManager.shared.openChat(for: session.id, withMessage: content)

            logger.info("handoff: opened prompt id=\(promptId) name=\(prompt.name)")
        }

        return true
    }

    private func handleContextActivity(_ activity: NSUserActivity) -> Bool {
        // Restore context if available
        if let clipboardPreview = activity.userInfo?["clipboardPreview"] as? String {
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(clipboardPreview, forType: .string)
        }

        // Open new session with context
        Task {
            let session = SessionBridge.shared.createSession(type: .chat)
            WebChatManager.shared.openChat(for: session.id)
        }

        return true
    }

    // MARK: - Private

    private func createAttributeSet(title: String, description: String) -> CSSearchableItemAttributeSet {
        let attributeSet = CSSearchableItemAttributeSet(contentType: .text)
        attributeSet.title = title
        attributeSet.contentDescription = description
        return attributeSet
    }
}

import CoreSpotlight

extension HandoffManager {
    /// Register activity types with the system
    func registerActivityTypes() {
        // Activity types are registered via Info.plist NSUserActivityTypes
        logger.debug("activity types should be registered in Info.plist")
    }
}
