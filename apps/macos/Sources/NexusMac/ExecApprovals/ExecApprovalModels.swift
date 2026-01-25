import Foundation

/// Security level for exec approvals.
enum ExecSecurity: String, CaseIterable, Codable, Identifiable {
    case deny
    case allowlist
    case full

    var id: String { rawValue }

    var title: String {
        switch self {
        case .deny: "Deny All"
        case .allowlist: "Allowlist Only"
        case .full: "Always Allow"
        }
    }

    var description: String {
        switch self {
        case .deny: "Block all command execution"
        case .allowlist: "Only allow commands matching allowlist patterns"
        case .full: "Allow all commands without prompting"
        }
    }
}

/// Ask mode for exec approval prompts.
enum ExecAsk: String, CaseIterable, Codable, Identifiable {
    case off
    case onMiss = "on-miss"
    case always

    var id: String { rawValue }

    var title: String {
        switch self {
        case .off: "Never Ask"
        case .onMiss: "Ask on Miss"
        case .always: "Always Ask"
        }
    }

    var description: String {
        switch self {
        case .off: "Never prompt for approval"
        case .onMiss: "Prompt when command doesn't match allowlist"
        case .always: "Prompt for every command execution"
        }
    }
}

/// Quick mode combining security and ask settings.
enum ExecApprovalQuickMode: String, CaseIterable, Identifiable {
    case deny
    case ask
    case allow

    var id: String { rawValue }

    var title: String {
        switch self {
        case .deny: "Deny"
        case .ask: "Ask"
        case .allow: "Allow"
        }
    }

    var security: ExecSecurity {
        switch self {
        case .deny: .deny
        case .ask: .allowlist
        case .allow: .full
        }
    }

    var ask: ExecAsk {
        switch self {
        case .deny: .off
        case .ask: .onMiss
        case .allow: .off
        }
    }

    static func from(security: ExecSecurity, ask: ExecAsk) -> ExecApprovalQuickMode {
        switch security {
        case .deny: .deny
        case .full: .allow
        case .allowlist: .ask
        }
    }
}

/// User's decision when prompted for command approval.
enum ExecApprovalDecision: String, Codable, Sendable {
    case allowOnce = "allow-once"
    case allowAlways = "allow-always"
    case deny
}

/// An entry in the command allowlist.
struct ExecAllowlistEntry: Codable, Hashable, Identifiable {
    var id: UUID
    var pattern: String
    var lastUsedAt: Double?
    var lastUsedCommand: String?
    var lastResolvedPath: String?

    init(
        id: UUID = UUID(),
        pattern: String,
        lastUsedAt: Double? = nil,
        lastUsedCommand: String? = nil,
        lastResolvedPath: String? = nil
    ) {
        self.id = id
        self.pattern = pattern
        self.lastUsedAt = lastUsedAt
        self.lastUsedCommand = lastUsedCommand
        self.lastResolvedPath = lastResolvedPath
    }

    var lastUsedDate: Date? {
        lastUsedAt.map { Date(timeIntervalSince1970: $0 / 1000) }
    }
}

/// Default settings for exec approvals.
struct ExecApprovalsDefaults: Codable {
    var security: ExecSecurity?
    var ask: ExecAsk?
    var askFallback: ExecSecurity?
    var autoAllowSkills: Bool?
}

/// Agent-specific exec approval settings.
struct ExecApprovalsAgent: Codable {
    var security: ExecSecurity?
    var ask: ExecAsk?
    var askFallback: ExecSecurity?
    var autoAllowSkills: Bool?
    var allowlist: [ExecAllowlistEntry]?

    var isEmpty: Bool {
        security == nil && ask == nil && askFallback == nil &&
            autoAllowSkills == nil && (allowlist?.isEmpty ?? true)
    }
}

/// Socket configuration for exec approvals IPC.
struct ExecApprovalsSocketConfig: Codable {
    var path: String?
    var token: String?
}

/// Persisted exec approvals file structure.
struct ExecApprovalsFile: Codable {
    var version: Int
    var socket: ExecApprovalsSocketConfig?
    var defaults: ExecApprovalsDefaults?
    var agents: [String: ExecApprovalsAgent]?
}

/// Snapshot of the exec approvals file with hash for change detection.
struct ExecApprovalsSnapshot: Codable {
    var path: String
    var exists: Bool
    var hash: String
    var file: ExecApprovalsFile
}

/// Resolved exec approval settings with all defaults applied.
struct ExecApprovalsResolved {
    let url: URL
    let socketPath: String
    let token: String
    let defaults: ExecApprovalsResolvedDefaults
    let agent: ExecApprovalsResolvedDefaults
    let allowlist: [ExecAllowlistEntry]
    var file: ExecApprovalsFile
}

/// Resolved default settings with all values filled in.
struct ExecApprovalsResolvedDefaults {
    var security: ExecSecurity
    var ask: ExecAsk
    var askFallback: ExecSecurity
    var autoAllowSkills: Bool
}

/// Request for command approval prompt.
struct ExecApprovalPromptRequest: Codable, Sendable {
    var command: String
    var cwd: String?
    var host: String?
    var security: String?
    var ask: String?
    var agentId: String?
    var resolvedPath: String?
    var sessionKey: String?
}

/// Resolution of a command to its executable path.
struct ExecCommandResolution: Sendable {
    let rawExecutable: String
    let resolvedPath: String?
    let executableName: String
    let cwd: String?

    static func resolve(
        command: [String],
        rawCommand: String?,
        cwd: String?,
        env: [String: String]?
    ) -> ExecCommandResolution? {
        let trimmedRaw = rawCommand?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if !trimmedRaw.isEmpty, let token = parseFirstToken(trimmedRaw) {
            return resolveExecutable(rawExecutable: token, cwd: cwd, env: env)
        }
        return resolve(command: command, cwd: cwd, env: env)
    }

    static func resolve(
        command: [String],
        cwd: String?,
        env: [String: String]?
    ) -> ExecCommandResolution? {
        guard let raw = command.first?.trimmingCharacters(in: .whitespacesAndNewlines),
              !raw.isEmpty
        else {
            return nil
        }
        return resolveExecutable(rawExecutable: raw, cwd: cwd, env: env)
    }

    private static func resolveExecutable(
        rawExecutable: String,
        cwd: String?,
        env: [String: String]?
    ) -> ExecCommandResolution? {
        let expanded = rawExecutable.hasPrefix("~")
            ? (rawExecutable as NSString).expandingTildeInPath
            : rawExecutable
        let hasPathSeparator = expanded.contains("/") || expanded.contains("\\")

        let resolvedPath: String? = {
            if hasPathSeparator {
                if expanded.hasPrefix("/") {
                    return expanded
                }
                let base = cwd?.trimmingCharacters(in: .whitespacesAndNewlines)
                let root = (base?.isEmpty == false) ? base! : FileManager.default.currentDirectoryPath
                return URL(fileURLWithPath: root).appendingPathComponent(expanded).path
            }
            let searchPaths = Self.searchPaths(from: env)
            return findExecutable(named: expanded, searchPaths: searchPaths)
        }()

        let name = resolvedPath.map { URL(fileURLWithPath: $0).lastPathComponent } ?? expanded
        return ExecCommandResolution(
            rawExecutable: expanded,
            resolvedPath: resolvedPath,
            executableName: name,
            cwd: cwd
        )
    }

    private static func parseFirstToken(_ command: String) -> String? {
        let trimmed = command.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, let first = trimmed.first else { return nil }

        if first == "\"" || first == "'" {
            let rest = trimmed.dropFirst()
            if let end = rest.firstIndex(of: first) {
                return String(rest[..<end])
            }
            return String(rest)
        }
        return trimmed.split(whereSeparator: { $0.isWhitespace }).first.map(String.init)
    }

    private static func searchPaths(from env: [String: String]?) -> [String] {
        let raw = env?["PATH"]
        if let raw, !raw.isEmpty {
            return raw.split(separator: ":").map(String.init)
        }
        return [
            "/opt/homebrew/bin",
            "/usr/local/bin",
            "/usr/bin",
            "/bin",
            "/usr/sbin",
            "/sbin",
        ]
    }

    private static func findExecutable(named name: String, searchPaths: [String]) -> String? {
        for path in searchPaths {
            let fullPath = URL(fileURLWithPath: path).appendingPathComponent(name).path
            if FileManager.default.isExecutableFile(atPath: fullPath) {
                return fullPath
            }
        }
        return nil
    }
}

/// Helpers for exec approval logic.
enum ExecApprovalHelpers {
    static func parseDecision(_ raw: String?) -> ExecApprovalDecision? {
        let trimmed = raw?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !trimmed.isEmpty else { return nil }
        return ExecApprovalDecision(rawValue: trimmed)
    }

    static func requiresAsk(
        ask: ExecAsk,
        security: ExecSecurity,
        allowlistMatch: ExecAllowlistEntry?,
        skillAllow: Bool
    ) -> Bool {
        if ask == .always { return true }
        if ask == .onMiss, security == .allowlist, allowlistMatch == nil, !skillAllow { return true }
        return false
    }

    static func allowlistPattern(
        command: [String],
        resolution: ExecCommandResolution?
    ) -> String? {
        let pattern = resolution?.resolvedPath ?? resolution?.rawExecutable ?? command.first ?? ""
        return pattern.isEmpty ? nil : pattern
    }
}

/// Matcher for allowlist entries against command resolutions.
enum ExecAllowlistMatcher {
    static func match(
        entries: [ExecAllowlistEntry],
        resolution: ExecCommandResolution?
    ) -> ExecAllowlistEntry? {
        guard let resolution, !entries.isEmpty else { return nil }
        let rawExecutable = resolution.rawExecutable
        let resolvedPath = resolution.resolvedPath
        let executableName = resolution.executableName

        for entry in entries {
            let pattern = entry.pattern.trimmingCharacters(in: .whitespacesAndNewlines)
            if pattern.isEmpty { continue }
            let hasPath = pattern.contains("/") || pattern.contains("~") || pattern.contains("\\")
            if hasPath {
                let target = resolvedPath ?? rawExecutable
                if matches(pattern: pattern, target: target) { return entry }
            } else if matches(pattern: pattern, target: executableName) {
                return entry
            }
        }
        return nil
    }

    private static func matches(pattern: String, target: String) -> Bool {
        let trimmed = pattern.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return false }
        let expanded = trimmed.hasPrefix("~")
            ? (trimmed as NSString).expandingTildeInPath
            : trimmed
        let normalizedPattern = normalizeMatchTarget(expanded)
        let normalizedTarget = normalizeMatchTarget(target)
        guard let regex = regex(for: normalizedPattern) else { return false }
        let range = NSRange(location: 0, length: normalizedTarget.utf16.count)
        return regex.firstMatch(in: normalizedTarget, options: [], range: range) != nil
    }

    private static func normalizeMatchTarget(_ value: String) -> String {
        value.replacingOccurrences(of: "\\\\", with: "/").lowercased()
    }

    private static func regex(for pattern: String) -> NSRegularExpression? {
        var regex = "^"
        var idx = pattern.startIndex
        while idx < pattern.endIndex {
            let ch = pattern[idx]
            if ch == "*" {
                let next = pattern.index(after: idx)
                if next < pattern.endIndex, pattern[next] == "*" {
                    regex += ".*"
                    idx = pattern.index(after: next)
                } else {
                    regex += "[^/]*"
                    idx = next
                }
                continue
            }
            if ch == "?" {
                regex += "."
                idx = pattern.index(after: idx)
                continue
            }
            regex += NSRegularExpression.escapedPattern(for: String(ch))
            idx = pattern.index(after: idx)
        }
        regex += "$"
        return try? NSRegularExpression(pattern: regex, options: [.caseInsensitive])
    }
}

/// Formats commands for display.
enum ExecCommandFormatter {
    static func displayString(for argv: [String]) -> String {
        argv.map { arg in
            let trimmed = arg.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !trimmed.isEmpty else { return "\"\"" }
            let needsQuotes = trimmed.contains { $0.isWhitespace || $0 == "\"" }
            if !needsQuotes { return trimmed }
            let escaped = trimmed.replacingOccurrences(of: "\"", with: "\\\"")
            return "\"\(escaped)\""
        }.joined(separator: " ")
    }

    static func displayString(for argv: [String], rawCommand: String?) -> String {
        let trimmed = rawCommand?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if !trimmed.isEmpty { return trimmed }
        return displayString(for: argv)
    }
}
