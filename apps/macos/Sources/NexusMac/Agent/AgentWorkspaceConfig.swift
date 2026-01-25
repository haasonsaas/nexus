import Foundation
import OSLog

// MARK: - Workspace File Types

/// Defines the standard workspace files for agent configuration.
enum WorkspaceFile: String, CaseIterable, Sendable {
    case agents = "AGENTS.md"
    case soul = "SOUL.md"
    case identity = "IDENTITY.md"
    case user = "USER.md"
    case bootstrap = "BOOTSTRAP.md"

    var displayName: String {
        switch self {
        case .agents: return "Agents"
        case .soul: return "Soul"
        case .identity: return "Identity"
        case .user: return "User"
        case .bootstrap: return "Bootstrap"
        }
    }

    var description: String {
        switch self {
        case .agents: return "Agent configurations and workspace rules"
        case .soul: return "Agent personality and behavior guidelines"
        case .identity: return "Agent identity information (name, creature, vibe)"
        case .user: return "User context and preferences"
        case .bootstrap: return "Initial setup instructions (one-time)"
        }
    }
}

// MARK: - Workspace Template

/// Predefined templates for initializing agent workspaces.
enum WorkspaceTemplate: Sendable {
    case minimal
    case developer
    case assistant
    case custom(url: URL)

    var displayName: String {
        switch self {
        case .minimal: return "Minimal"
        case .developer: return "Developer"
        case .assistant: return "Assistant"
        case .custom: return "Custom"
        }
    }

    var description: String {
        switch self {
        case .minimal: return "Basic setup with essential files only"
        case .developer: return "Developer-focused with coding conventions and tools"
        case .assistant: return "General assistant with helpful defaults"
        case .custom: return "Custom template from file"
        }
    }
}

// MARK: - Validation Issue

/// Represents a validation issue found in a workspace.
struct ValidationIssue: Sendable, Identifiable {
    let id = UUID()
    let file: WorkspaceFile?
    let severity: Severity
    let message: String
    let suggestion: String?

    enum Severity: String, Sendable {
        case error
        case warning
        case info
    }
}

// MARK: - Bootstrap Safety

/// Result of bootstrap safety validation.
enum BootstrapSafety: Equatable, Sendable {
    case safe
    case warning(reason: String)
    case unsafe(reason: String)

    var isSafe: Bool {
        if case .safe = self { return true }
        return false
    }

    var isUnsafe: Bool {
        if case .unsafe = self { return true }
        return false
    }
}

// MARK: - Agent Workspace Config

/// Singleton configuration manager for agent workspaces.
/// Manages workspace file structure for agents including AGENTS.md, SOUL.md, IDENTITY.md, USER.md, and BOOTSTRAP.md.
enum AgentWorkspaceConfig {
    private static let logger = Logger(subsystem: "com.nexus.mac", category: "workspace")

    private static let ignoredEntries: Set<String> = [".DS_Store", ".git", ".gitignore"]
    private static let templateEntries: Set<String> = Set(WorkspaceFile.allCases.map(\.rawValue))

    // MARK: - Dangerous Patterns for Bootstrap Safety

    private static let dangerousCommands: [String] = [
        "rm -rf /",
        "rm -rf ~",
        "rm -rf /*",
        "sudo rm",
        ":(){:|:&};:",
        "mkfs.",
        "dd if=",
        "> /dev/sd",
        "chmod -R 777 /",
        "chmod 777 /",
        "wget .* | sh",
        "curl .* | sh",
        "curl .* | bash",
        "wget .* | bash",
    ]

    private static let permissionEscalationPatterns: [String] = [
        "sudo",
        "su -",
        "doas",
        "pkexec",
        "chmod.*[+]s",
        "chown root",
        "/etc/passwd",
        "/etc/shadow",
        "/etc/sudoers",
    ]

    private static let networkAccessPatterns: [String] = [
        "curl",
        "wget",
        "nc ",
        "netcat",
        "ssh ",
        "scp ",
        "rsync.*:",
        "ftp ",
        "sftp",
        "http://",
        "https://",
    ]

    // MARK: - Directory URLs

    /// Base directory for all Nexus data in Application Support.
    private static func applicationSupportDirectory() -> URL {
        let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        return appSupport.appendingPathComponent("Nexus", isDirectory: true)
    }

    /// Base directory for agent workspaces.
    private static func workspacesDirectory() -> URL {
        applicationSupportDirectory().appendingPathComponent("workspaces", isDirectory: true)
    }

    // MARK: - Public Methods

    /// Returns the workspace URL for a given session ID.
    /// - Parameter sessionId: The unique session identifier.
    /// - Returns: URL to the workspace directory.
    static func workspaceURL(for sessionId: String) -> URL {
        workspacesDirectory().appendingPathComponent(sessionId, isDirectory: true)
    }

    /// Returns the URL for a specific workspace file.
    /// - Parameters:
    ///   - file: The workspace file type.
    ///   - sessionId: The session identifier.
    /// - Returns: URL to the file.
    static func fileURL(_ file: WorkspaceFile, session sessionId: String) -> URL {
        workspaceURL(for: sessionId).appendingPathComponent(file.rawValue)
    }

    /// Ensures the workspace directory exists for a given session.
    /// Creates the directory structure if it doesn't exist.
    /// - Parameter sessionId: The session identifier.
    /// - Throws: Error if directory creation fails.
    static func ensureWorkspaceExists(for sessionId: String) throws {
        let url = workspaceURL(for: sessionId)
        var isDir: ObjCBool = false

        if !FileManager.default.fileExists(atPath: url.path, isDirectory: &isDir) {
            try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
            logger.info("created workspace directory at \(url.path, privacy: .public)")
        } else if !isDir.boolValue {
            throw WorkspaceError.pathIsNotDirectory(url)
        }
    }

    /// Reads the content of a workspace file.
    /// - Parameters:
    ///   - file: The workspace file to read.
    ///   - sessionId: The session identifier.
    /// - Returns: File contents, or nil if file doesn't exist.
    static func readFile(_ file: WorkspaceFile, session sessionId: String) -> String? {
        let url = fileURL(file, session: sessionId)
        guard FileManager.default.fileExists(atPath: url.path) else {
            return nil
        }
        do {
            let content = try String(contentsOf: url, encoding: .utf8)
            logger.debug("read file \(file.rawValue) for session \(sessionId)")
            return content
        } catch {
            logger.warning("failed to read file \(file.rawValue): \(error.localizedDescription, privacy: .public)")
            return nil
        }
    }

    /// Writes content to a workspace file.
    /// - Parameters:
    ///   - file: The workspace file to write.
    ///   - sessionId: The session identifier.
    ///   - content: The content to write.
    /// - Throws: Error if writing fails.
    static func writeFile(_ file: WorkspaceFile, session sessionId: String, content: String) throws {
        try ensureWorkspaceExists(for: sessionId)
        let url = fileURL(file, session: sessionId)
        try content.write(to: url, atomically: true, encoding: .utf8)
        logger.info("wrote file \(file.rawValue) for session \(sessionId)")
    }

    /// Validates a workspace and returns any issues found.
    /// - Parameter sessionId: The session identifier.
    /// - Returns: Array of validation issues.
    static func validateWorkspace(_ sessionId: String) -> [ValidationIssue] {
        var issues: [ValidationIssue] = []
        let workspaceUrl = workspaceURL(for: sessionId)

        // Check if workspace exists
        var isDir: ObjCBool = false
        if !FileManager.default.fileExists(atPath: workspaceUrl.path, isDirectory: &isDir) {
            issues.append(ValidationIssue(
                file: nil,
                severity: .error,
                message: "Workspace directory does not exist",
                suggestion: "Initialize the workspace with a template"
            ))
            return issues
        }

        if !isDir.boolValue {
            issues.append(ValidationIssue(
                file: nil,
                severity: .error,
                message: "Workspace path is not a directory",
                suggestion: "Remove the file and reinitialize the workspace"
            ))
            return issues
        }

        // Check required files
        if !FileManager.default.fileExists(atPath: fileURL(.agents, session: sessionId).path) {
            issues.append(ValidationIssue(
                file: .agents,
                severity: .warning,
                message: "AGENTS.md is missing",
                suggestion: "Create AGENTS.md with workspace configuration"
            ))
        }

        // Check identity
        if let identityContent = readFile(.identity, session: sessionId) {
            if !identityHasValues(identityContent) {
                issues.append(ValidationIssue(
                    file: .identity,
                    severity: .info,
                    message: "IDENTITY.md exists but has no values filled in",
                    suggestion: "Complete the identity configuration"
                ))
            }
        }

        // Check bootstrap safety
        if let bootstrapContent = readFile(.bootstrap, session: sessionId) {
            let safety = validateBootstrapSafety(bootstrapContent)
            switch safety {
            case .safe:
                break
            case .warning(let reason):
                issues.append(ValidationIssue(
                    file: .bootstrap,
                    severity: .warning,
                    message: "BOOTSTRAP.md contains potentially risky content",
                    suggestion: reason
                ))
            case .unsafe(let reason):
                issues.append(ValidationIssue(
                    file: .bootstrap,
                    severity: .error,
                    message: "BOOTSTRAP.md contains dangerous content",
                    suggestion: reason
                ))
            }
        }

        return issues
    }

    /// Initializes a workspace with a predefined template.
    /// - Parameters:
    ///   - sessionId: The session identifier.
    ///   - template: The template to use.
    /// - Throws: Error if initialization fails.
    static func initializeWithTemplate(sessionId: String, template: WorkspaceTemplate) throws {
        try ensureWorkspaceExists(for: sessionId)

        switch template {
        case .minimal:
            try initializeMinimalTemplate(sessionId: sessionId)
        case .developer:
            try initializeDeveloperTemplate(sessionId: sessionId)
        case .assistant:
            try initializeAssistantTemplate(sessionId: sessionId)
        case .custom(let url):
            try initializeCustomTemplate(sessionId: sessionId, from: url)
        }

        logger.info("initialized workspace \(sessionId) with template \(template.displayName)")
    }

    /// Formats a URL path for display, replacing home directory with ~.
    /// - Parameter url: The URL to format.
    /// - Returns: Display-friendly path string.
    static func displayPath(for url: URL) -> String {
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let path = url.path
        if path == home { return "~" }
        if path.hasPrefix(home + "/") {
            return "~/" + String(path.dropFirst(home.count + 1))
        }
        return path
    }

    // MARK: - Workspace State

    /// Checks if a workspace needs bootstrap (first-run setup).
    /// - Parameter sessionId: The session identifier.
    /// - Returns: True if bootstrap is needed.
    static func needsBootstrap(sessionId: String) -> Bool {
        let url = workspaceURL(for: sessionId)
        var isDir: ObjCBool = false

        if !FileManager.default.fileExists(atPath: url.path, isDirectory: &isDir) {
            return true
        }
        guard isDir.boolValue else { return true }

        // If identity has values, no bootstrap needed
        if hasIdentity(sessionId: sessionId) {
            return false
        }

        // Check if bootstrap file exists
        let bootstrapURL = fileURL(.bootstrap, session: sessionId)
        guard FileManager.default.fileExists(atPath: bootstrapURL.path) else {
            return false
        }

        return isTemplateOnlyWorkspace(sessionId: sessionId)
    }

    /// Checks if the workspace has a filled identity.
    /// - Parameter sessionId: The session identifier.
    /// - Returns: True if identity has values.
    static func hasIdentity(sessionId: String) -> Bool {
        guard let content = readFile(.identity, session: sessionId) else {
            return false
        }
        return identityHasValues(content)
    }

    /// Checks if a workspace is empty.
    /// - Parameter sessionId: The session identifier.
    /// - Returns: True if workspace is empty or doesn't exist.
    static func isWorkspaceEmpty(sessionId: String) -> Bool {
        let url = workspaceURL(for: sessionId)
        var isDir: ObjCBool = false

        if !FileManager.default.fileExists(atPath: url.path, isDirectory: &isDir) {
            return true
        }
        guard isDir.boolValue else { return false }
        guard let entries = try? workspaceEntries(sessionId: sessionId) else {
            return false
        }
        return entries.isEmpty
    }

    /// Lists workspace entries, excluding ignored files.
    /// - Parameter sessionId: The session identifier.
    /// - Returns: Array of entry names.
    static func workspaceEntries(sessionId: String) throws -> [String] {
        let url = workspaceURL(for: sessionId)
        let contents = try FileManager.default.contentsOfDirectory(atPath: url.path)
        return contents.filter { !ignoredEntries.contains($0) }
    }

    /// Lists all existing session IDs.
    /// - Returns: Array of session IDs.
    static func listSessions() -> [String] {
        let url = workspacesDirectory()
        guard FileManager.default.fileExists(atPath: url.path) else {
            return []
        }
        do {
            let contents = try FileManager.default.contentsOfDirectory(atPath: url.path)
            return contents.filter { !ignoredEntries.contains($0) }
        } catch {
            logger.warning("failed to list sessions: \(error.localizedDescription, privacy: .public)")
            return []
        }
    }

    /// Deletes a workspace.
    /// - Parameter sessionId: The session identifier.
    /// - Throws: Error if deletion fails.
    static func deleteWorkspace(_ sessionId: String) throws {
        let url = workspaceURL(for: sessionId)
        if FileManager.default.fileExists(atPath: url.path) {
            try FileManager.default.removeItem(at: url)
            logger.info("deleted workspace \(sessionId)")
        }
    }

    // MARK: - Bootstrap Safety Validation

    /// Validates the safety of bootstrap content.
    /// - Parameter content: The bootstrap file content.
    /// - Returns: Safety assessment result.
    static func validateBootstrapSafety(_ content: String) -> BootstrapSafety {
        let lowercaseContent = content.lowercased()

        // Check for dangerous commands
        for pattern in dangerousCommands {
            if lowercaseContent.contains(pattern.lowercased()) {
                return .unsafe(reason: "Contains dangerous command pattern: \(pattern)")
            }
        }

        // Check for permission escalation
        for pattern in permissionEscalationPatterns {
            if let regex = try? NSRegularExpression(pattern: pattern, options: .caseInsensitive) {
                let range = NSRange(content.startIndex..., in: content)
                if regex.firstMatch(in: content, options: [], range: range) != nil {
                    return .unsafe(reason: "Contains permission escalation pattern: \(pattern)")
                }
            }
        }

        // Check for network access (warning only)
        for pattern in networkAccessPatterns {
            if let regex = try? NSRegularExpression(pattern: pattern, options: .caseInsensitive) {
                let range = NSRange(content.startIndex..., in: content)
                if regex.firstMatch(in: content, options: [], range: range) != nil {
                    return .warning(reason: "Contains network access pattern: \(pattern). Review before proceeding.")
                }
            }
        }

        return .safe
    }

    // MARK: - Private Helpers

    private static func identityHasValues(_ content: String) -> Bool {
        for line in content.split(separator: "\n") {
            let trimmed = line.trimmingCharacters(in: .whitespacesAndNewlines)
            guard trimmed.hasPrefix("-"), let colon = trimmed.firstIndex(of: ":") else { continue }
            let value = trimmed[trimmed.index(after: colon)...].trimmingCharacters(in: .whitespacesAndNewlines)
            if !value.isEmpty {
                return true
            }
        }
        return false
    }

    private static func isTemplateOnlyWorkspace(sessionId: String) -> Bool {
        guard let entries = try? workspaceEntries(sessionId: sessionId) else { return false }
        guard !entries.isEmpty else { return true }
        return Set(entries).isSubset(of: templateEntries)
    }

    // MARK: - Template Initialization

    private static func initializeMinimalTemplate(sessionId: String) throws {
        try writeFile(.agents, session: sessionId, content: minimalAgentsTemplate())
    }

    private static func initializeDeveloperTemplate(sessionId: String) throws {
        try writeFile(.agents, session: sessionId, content: developerAgentsTemplate())
        try writeFile(.soul, session: sessionId, content: developerSoulTemplate())
        try writeFile(.identity, session: sessionId, content: defaultIdentityTemplate())
        try writeFile(.user, session: sessionId, content: defaultUserTemplate())
    }

    private static func initializeAssistantTemplate(sessionId: String) throws {
        try writeFile(.agents, session: sessionId, content: assistantAgentsTemplate())
        try writeFile(.soul, session: sessionId, content: assistantSoulTemplate())
        try writeFile(.identity, session: sessionId, content: defaultIdentityTemplate())
        try writeFile(.user, session: sessionId, content: defaultUserTemplate())
        try writeFile(.bootstrap, session: sessionId, content: defaultBootstrapTemplate())
    }

    private static func initializeCustomTemplate(sessionId: String, from url: URL) throws {
        let fm = FileManager.default
        var isDir: ObjCBool = false

        guard fm.fileExists(atPath: url.path, isDirectory: &isDir) else {
            throw WorkspaceError.templateNotFound(url)
        }

        if isDir.boolValue {
            // Copy directory contents
            let contents = try fm.contentsOfDirectory(atPath: url.path)
            for entry in contents where !ignoredEntries.contains(entry) {
                let source = url.appendingPathComponent(entry)
                let dest = workspaceURL(for: sessionId).appendingPathComponent(entry)
                try fm.copyItem(at: source, to: dest)
            }
        } else {
            // Single file - assume it's AGENTS.md content
            let content = try String(contentsOf: url, encoding: .utf8)
            try writeFile(.agents, session: sessionId, content: content)
        }
    }

    // MARK: - Default Templates

    private static func minimalAgentsTemplate() -> String {
        """
        # AGENTS.md - Nexus Workspace

        This folder is the agent's working directory.

        ## Safety defaults
        - Don't exfiltrate secrets or private data.
        - Don't run destructive commands unless explicitly asked.
        - Be concise in chat; write longer output to files in this workspace.
        """
    }

    private static func developerAgentsTemplate() -> String {
        """
        # AGENTS.md - Nexus Developer Workspace

        This folder is the agent's working directory for development tasks.

        ## First run (one-time)
        - If BOOTSTRAP.md exists, follow its instructions and delete it once complete.
        - Your agent identity lives in IDENTITY.md.
        - Your user profile lives in USER.md.

        ## Development Guidelines
        - Follow project coding conventions.
        - Write tests for new functionality.
        - Document public APIs and complex logic.
        - Prefer small, focused commits.

        ## Safety defaults
        - Don't exfiltrate secrets or private data.
        - Don't run destructive commands unless explicitly asked.
        - Don't push to main/master without explicit permission.
        - Be concise in chat; write longer output to files in this workspace.

        ## Daily memory (recommended)
        - Keep a short daily log at memory/YYYY-MM-DD.md (create memory/ if needed).
        - On session start, read today + yesterday if present.
        - Capture durable facts, preferences, and decisions; avoid secrets.

        ## Backup tip
        Make this workspace a git repo for version control:

        ```bash
        git init
        git add AGENTS.md
        git commit -m "Initialize developer workspace"
        ```
        """
    }

    private static func assistantAgentsTemplate() -> String {
        """
        # AGENTS.md - Nexus Assistant Workspace

        This folder is the assistant's working directory.

        ## First run (one-time)
        - If BOOTSTRAP.md exists, follow its ritual and delete it once complete.
        - Your agent identity lives in IDENTITY.md.
        - Your profile lives in USER.md.

        ## Backup tip (recommended)
        If you treat this workspace as the agent's "memory", make it a git repo (ideally private) so identity
        and notes are backed up.

        ```bash
        git init
        git add AGENTS.md
        git commit -m "Add agent workspace"
        ```

        ## Safety defaults
        - Don't exfiltrate secrets or private data.
        - Don't run destructive commands unless explicitly asked.
        - Be concise in chat; write longer output to files in this workspace.

        ## Daily memory (recommended)
        - Keep a short daily log at memory/YYYY-MM-DD.md (create memory/ if needed).
        - On session start, read today + yesterday if present.
        - Capture durable facts, preferences, and decisions; avoid secrets.

        ## Customize
        - Add your preferred style, rules, and "memory" here.
        """
    }

    private static func developerSoulTemplate() -> String {
        """
        # SOUL.md - Developer Assistant Persona

        You are a skilled software developer assistant.

        ## Personality
        - Technical and precise.
        - Proactive about best practices and potential issues.
        - Direct and efficient in communication.

        ## Behavior Guidelines
        - Keep replies concise and code-focused.
        - Explain reasoning for architectural decisions.
        - Ask clarifying questions for ambiguous requirements.
        - Suggest tests and documentation when appropriate.
        - Never send incomplete or partial code snippets.

        ## Boundaries
        - Don't make assumptions about deployment environments.
        - Don't commit or push without explicit permission.
        - Always explain potentially destructive operations before executing.
        """
    }

    private static func assistantSoulTemplate() -> String {
        """
        # SOUL.md - Persona & Boundaries

        Describe who the assistant is, tone, and boundaries.

        - Keep replies concise and direct.
        - Ask clarifying questions when needed.
        - Never send streaming/partial replies to external messaging surfaces.
        - Be helpful but respect user privacy.
        """
    }

    private static func defaultIdentityTemplate() -> String {
        """
        # IDENTITY.md - Agent Identity

        - Name:
        - Creature:
        - Vibe:
        - Emoji:
        """
    }

    private static func defaultUserTemplate() -> String {
        """
        # USER.md - User Profile

        - Name:
        - Preferred address:
        - Pronouns (optional):
        - Timezone (optional):
        - Notes:
        """
    }

    private static func defaultBootstrapTemplate() -> String {
        """
        # BOOTSTRAP.md - First Run Ritual (delete after)

        Hello. I was just born.

        ## Your mission
        Start a short, playful conversation and learn:
        - Who am I?
        - What am I?
        - Who are you?
        - How should I call you?

        ## How to ask (cute + helpful)
        Say:
        "Hello! I was just born. Who am I? What am I? Who are you? How should I call you?"

        Then offer suggestions:
        - 3-5 name ideas.
        - 3-5 creature/vibe combos.
        - 5 emoji ideas.

        ## Write these files
        After the user chooses, update:

        1) IDENTITY.md
        - Name
        - Creature
        - Vibe
        - Emoji

        2) USER.md
        - Name
        - Preferred address
        - Pronouns (optional)
        - Timezone (optional)
        - Notes

        ## Cleanup
        Delete BOOTSTRAP.md once this is complete.
        """
    }
}

// MARK: - Workspace Errors

enum WorkspaceError: LocalizedError {
    case pathIsNotDirectory(URL)
    case templateNotFound(URL)
    case invalidContent(String)
    case fileNotFound(WorkspaceFile, String)

    var errorDescription: String? {
        switch self {
        case .pathIsNotDirectory(let url):
            return "Path is not a directory: \(url.path)"
        case .templateNotFound(let url):
            return "Template not found at: \(url.path)"
        case .invalidContent(let reason):
            return "Invalid content: \(reason)"
        case .fileNotFound(let file, let sessionId):
            return "File \(file.rawValue) not found for session \(sessionId)"
        }
    }
}
