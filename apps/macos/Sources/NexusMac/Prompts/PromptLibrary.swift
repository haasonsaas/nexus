import Foundation
import OSLog

/// Library of prompt templates for various AI tasks.
/// Supports variables, categories, and user customization.
@MainActor
@Observable
final class PromptLibrary {
    static let shared = PromptLibrary()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "prompts")

    private(set) var prompts: [PromptTemplate] = []
    private(set) var categories: [Category] = []

    struct PromptTemplate: Identifiable, Codable {
        let id: String
        var name: String
        var description: String?
        var content: String
        var variables: [Variable]
        var categoryId: String
        var tags: [String]
        var isBuiltIn: Bool
        var usageCount: Int

        struct Variable: Codable {
            let name: String
            let placeholder: String
            let defaultValue: String?
            let type: VariableType

            enum VariableType: String, Codable {
                case text
                case multiline
                case selection
                case context
                case clipboard
            }
        }
    }

    struct Category: Identifiable, Codable {
        let id: String
        var name: String
        var icon: String
        var order: Int
    }

    init() {
        registerBuiltInCategories()
        registerBuiltInPrompts()
    }

    // MARK: - Prompt Management

    /// Add or update a prompt
    func save(_ prompt: PromptTemplate) {
        if let index = prompts.firstIndex(where: { $0.id == prompt.id }) {
            prompts[index] = prompt
        } else {
            prompts.append(prompt)
        }
        persistPrompts()
        logger.info("prompt saved id=\(prompt.id)")
    }

    /// Remove a prompt
    func remove(promptId: String) {
        prompts.removeAll { $0.id == promptId }
        persistPrompts()
    }

    /// Get prompts by category
    func prompts(in categoryId: String) -> [PromptTemplate] {
        prompts.filter { $0.categoryId == categoryId }
    }

    /// Search prompts
    func search(query: String) -> [PromptTemplate] {
        let lowercased = query.lowercased()
        return prompts.filter { prompt in
            prompt.name.lowercased().contains(lowercased) ||
            prompt.content.lowercased().contains(lowercased) ||
            prompt.tags.contains { $0.lowercased().contains(lowercased) }
        }
    }

    /// Get most used prompts
    func mostUsed(limit: Int = 10) -> [PromptTemplate] {
        prompts.sorted { $0.usageCount > $1.usageCount }.prefix(limit).map { $0 }
    }

    // MARK: - Prompt Execution

    /// Expand a prompt with variable values
    func expand(_ prompt: PromptTemplate, variables: [String: String]) -> String {
        var result = prompt.content

        for variable in prompt.variables {
            let value = variables[variable.name] ?? variable.defaultValue ?? ""

            // Handle special variable types
            let expandedValue: String
            switch variable.type {
            case .context:
                expandedValue = ContextManager.shared.exportMarkdown()
            case .clipboard:
                expandedValue = NSPasteboard.general.string(forType: .string) ?? ""
            case .selection:
                expandedValue = AccessibilityBridge.shared.getSelectedText() ?? value
            default:
                expandedValue = value
            }

            result = result.replacingOccurrences(
                of: "{{\(variable.name)}}",
                with: expandedValue
            )
        }

        // Track usage
        incrementUsage(promptId: prompt.id)

        return result
    }

    /// Create a prompt from a template string
    func parseTemplate(_ content: String) -> (content: String, variables: [PromptTemplate.Variable]) {
        var variables: [PromptTemplate.Variable] = []
        let pattern = "\\{\\{([^}]+)\\}\\}"

        if let regex = try? NSRegularExpression(pattern: pattern) {
            let matches = regex.matches(in: content, range: NSRange(content.startIndex..., in: content))

            for match in matches {
                if let range = Range(match.range(at: 1), in: content) {
                    let varName = String(content[range])
                    if !variables.contains(where: { $0.name == varName }) {
                        variables.append(PromptTemplate.Variable(
                            name: varName,
                            placeholder: "Enter \(varName)",
                            defaultValue: nil,
                            type: .text
                        ))
                    }
                }
            }
        }

        return (content, variables)
    }

    // MARK: - Category Management

    /// Add or update a category
    func saveCategory(_ category: Category) {
        if let index = categories.firstIndex(where: { $0.id == category.id }) {
            categories[index] = category
        } else {
            categories.append(category)
        }
        categories.sort { $0.order < $1.order }
        persistPrompts()
    }

    /// Remove a category
    func removeCategory(id: String) {
        categories.removeAll { $0.id == id }
        // Move prompts to uncategorized
        for i in prompts.indices where prompts[i].categoryId == id {
            prompts[i].categoryId = "uncategorized"
        }
        persistPrompts()
    }

    // MARK: - Private

    private func incrementUsage(promptId: String) {
        if let index = prompts.firstIndex(where: { $0.id == promptId }) {
            prompts[index].usageCount += 1
        }
    }

    private func registerBuiltInCategories() {
        categories = [
            Category(id: "writing", name: "Writing", icon: "pencil", order: 1),
            Category(id: "code", name: "Code", icon: "chevron.left.forwardslash.chevron.right", order: 2),
            Category(id: "analysis", name: "Analysis", icon: "magnifyingglass", order: 3),
            Category(id: "creative", name: "Creative", icon: "paintbrush", order: 4),
            Category(id: "productivity", name: "Productivity", icon: "checklist", order: 5),
            Category(id: "uncategorized", name: "Other", icon: "folder", order: 99)
        ]
    }

    private func registerBuiltInPrompts() {
        prompts = [
            // Writing
            PromptTemplate(
                id: "builtin_summarize",
                name: "Summarize",
                description: "Summarize the given text",
                content: "Please summarize the following text concisely:\n\n{{text}}",
                variables: [.init(name: "text", placeholder: "Text to summarize", defaultValue: nil, type: .multiline)],
                categoryId: "writing",
                tags: ["summary", "writing"],
                isBuiltIn: true,
                usageCount: 0
            ),
            PromptTemplate(
                id: "builtin_rewrite",
                name: "Rewrite",
                description: "Rewrite text in a different style",
                content: "Please rewrite the following text to be more {{style}}:\n\n{{text}}",
                variables: [
                    .init(name: "style", placeholder: "formal, casual, concise, etc.", defaultValue: "professional", type: .text),
                    .init(name: "text", placeholder: "Text to rewrite", defaultValue: nil, type: .multiline)
                ],
                categoryId: "writing",
                tags: ["rewrite", "style"],
                isBuiltIn: true,
                usageCount: 0
            ),

            // Code
            PromptTemplate(
                id: "builtin_code_explain",
                name: "Explain Code",
                description: "Explain what code does",
                content: "Please explain what this code does:\n\n```{{language}}\n{{code}}\n```",
                variables: [
                    .init(name: "language", placeholder: "Language", defaultValue: "", type: .text),
                    .init(name: "code", placeholder: "Code to explain", defaultValue: nil, type: .multiline)
                ],
                categoryId: "code",
                tags: ["code", "explain"],
                isBuiltIn: true,
                usageCount: 0
            ),
            PromptTemplate(
                id: "builtin_code_review",
                name: "Code Review",
                description: "Review code for issues",
                content: "Please review this code for potential issues, bugs, and improvements:\n\n```{{language}}\n{{code}}\n```",
                variables: [
                    .init(name: "language", placeholder: "Language", defaultValue: "", type: .text),
                    .init(name: "code", placeholder: "Code to review", defaultValue: nil, type: .multiline)
                ],
                categoryId: "code",
                tags: ["code", "review"],
                isBuiltIn: true,
                usageCount: 0
            ),

            // Analysis
            PromptTemplate(
                id: "builtin_analyze_context",
                name: "Analyze Current Context",
                description: "Analyze the current screen and context",
                content: "Here is my current context:\n\n{{context}}\n\nPlease analyze what I'm working on and suggest how you can help.",
                variables: [
                    .init(name: "context", placeholder: "", defaultValue: nil, type: .context)
                ],
                categoryId: "analysis",
                tags: ["context", "analysis"],
                isBuiltIn: true,
                usageCount: 0
            ),

            // Productivity
            PromptTemplate(
                id: "builtin_task_breakdown",
                name: "Break Down Task",
                description: "Break down a task into steps",
                content: "Please break down this task into actionable steps:\n\n{{task}}",
                variables: [
                    .init(name: "task", placeholder: "Task to break down", defaultValue: nil, type: .multiline)
                ],
                categoryId: "productivity",
                tags: ["task", "planning"],
                isBuiltIn: true,
                usageCount: 0
            )
        ]
    }

    // MARK: - Persistence

    private func persistPrompts() {
        let url = promptsFileURL()
        let customPrompts = prompts.filter { !$0.isBuiltIn }
        do {
            let data = try JSONEncoder().encode(customPrompts)
            try data.write(to: url)
        } catch {
            logger.error("failed to persist prompts: \(error.localizedDescription)")
        }
    }

    func loadPrompts() {
        let url = promptsFileURL()
        guard FileManager.default.fileExists(atPath: url.path),
              let data = try? Data(contentsOf: url),
              let loaded = try? JSONDecoder().decode([PromptTemplate].self, from: data) else {
            return
        }
        for prompt in loaded {
            if !prompts.contains(where: { $0.id == prompt.id }) {
                prompts.append(prompt)
            }
        }
        logger.debug("loaded \(loaded.count) custom prompts")
    }

    private func promptsFileURL() -> URL {
        let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        let nexusDir = appSupport.appendingPathComponent("Nexus")
        try? FileManager.default.createDirectory(at: nexusDir, withIntermediateDirectories: true)
        return nexusDir.appendingPathComponent("prompts.json")
    }
}
