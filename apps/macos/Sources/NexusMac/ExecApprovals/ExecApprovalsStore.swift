import CryptoKit
import Foundation
import OSLog
import Security

/// Persistent storage for exec approval settings and allowlists.
enum ExecApprovalsStore {
    private static let logger = Logger(subsystem: "com.nexus.mac", category: "exec-approvals")
    private static let defaultAgentId = "main"
    private static let defaultSecurity: ExecSecurity = .deny
    private static let defaultAsk: ExecAsk = .onMiss
    private static let defaultAskFallback: ExecSecurity = .deny
    private static let defaultAutoAllowSkills = false

    // MARK: - File Paths

    static func stateDirectory() -> URL {
        let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        return appSupport.appendingPathComponent("Nexus", isDirectory: true)
    }

    static func fileURL() -> URL {
        stateDirectory().appendingPathComponent("exec-approvals.json")
    }

    static func socketPath() -> String {
        stateDirectory().appendingPathComponent("exec-approvals.sock").path
    }

    // MARK: - File Operations

    static func loadFile() -> ExecApprovalsFile {
        let url = fileURL()
        guard FileManager.default.fileExists(atPath: url.path) else {
            return ExecApprovalsFile(version: 1, socket: nil, defaults: nil, agents: [:])
        }
        do {
            let data = try Data(contentsOf: url)
            let decoded = try JSONDecoder().decode(ExecApprovalsFile.self, from: data)
            if decoded.version != 1 {
                return ExecApprovalsFile(version: 1, socket: nil, defaults: nil, agents: [:])
            }
            return decoded
        } catch {
            logger.warning("exec approvals load failed: \(error.localizedDescription, privacy: .public)")
            return ExecApprovalsFile(version: 1, socket: nil, defaults: nil, agents: [:])
        }
    }

    static func saveFile(_ file: ExecApprovalsFile) {
        do {
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
            let data = try encoder.encode(file)
            let url = fileURL()
            try FileManager.default.createDirectory(
                at: url.deletingLastPathComponent(),
                withIntermediateDirectories: true
            )
            try data.write(to: url, options: [.atomic])
            try? FileManager.default.setAttributes(
                [.posixPermissions: 0o600],
                ofItemAtPath: url.path
            )
        } catch {
            logger.error("exec approvals save failed: \(error.localizedDescription, privacy: .public)")
        }
    }

    static func ensureFile() -> ExecApprovalsFile {
        var file = loadFile()
        if file.socket == nil {
            file.socket = ExecApprovalsSocketConfig(path: nil, token: nil)
        }
        let path = file.socket?.path?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if path.isEmpty {
            file.socket?.path = socketPath()
        }
        let token = file.socket?.token?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if token.isEmpty {
            file.socket?.token = generateToken()
        }
        if file.agents == nil {
            file.agents = [:]
        }
        saveFile(file)
        return file
    }

    // MARK: - Resolution

    static func resolve(agentId: String?) -> ExecApprovalsResolved {
        let file = ensureFile()
        let defaults = file.defaults ?? ExecApprovalsDefaults()
        let resolvedDefaults = ExecApprovalsResolvedDefaults(
            security: defaults.security ?? defaultSecurity,
            ask: defaults.ask ?? defaultAsk,
            askFallback: defaults.askFallback ?? defaultAskFallback,
            autoAllowSkills: defaults.autoAllowSkills ?? defaultAutoAllowSkills
        )

        let key = agentKey(agentId)
        let agentEntry = file.agents?[key] ?? ExecApprovalsAgent()
        let wildcardEntry = file.agents?["*"] ?? ExecApprovalsAgent()

        let resolvedAgent = ExecApprovalsResolvedDefaults(
            security: agentEntry.security ?? wildcardEntry.security ?? resolvedDefaults.security,
            ask: agentEntry.ask ?? wildcardEntry.ask ?? resolvedDefaults.ask,
            askFallback: agentEntry.askFallback ?? wildcardEntry.askFallback ?? resolvedDefaults.askFallback,
            autoAllowSkills: agentEntry.autoAllowSkills ?? wildcardEntry.autoAllowSkills ?? resolvedDefaults.autoAllowSkills
        )

        let allowlist = ((wildcardEntry.allowlist ?? []) + (agentEntry.allowlist ?? []))
            .map { entry in
                ExecAllowlistEntry(
                    id: entry.id,
                    pattern: entry.pattern.trimmingCharacters(in: .whitespacesAndNewlines),
                    lastUsedAt: entry.lastUsedAt,
                    lastUsedCommand: entry.lastUsedCommand,
                    lastResolvedPath: entry.lastResolvedPath
                )
            }
            .filter { !$0.pattern.isEmpty }

        let socketPath = expandPath(file.socket?.path ?? Self.socketPath())
        let token = file.socket?.token ?? ""

        return ExecApprovalsResolved(
            url: fileURL(),
            socketPath: socketPath,
            token: token,
            defaults: resolvedDefaults,
            agent: resolvedAgent,
            allowlist: allowlist,
            file: file
        )
    }

    static func resolveDefaults() -> ExecApprovalsResolvedDefaults {
        let file = ensureFile()
        let defaults = file.defaults ?? ExecApprovalsDefaults()
        return ExecApprovalsResolvedDefaults(
            security: defaults.security ?? defaultSecurity,
            ask: defaults.ask ?? defaultAsk,
            askFallback: defaults.askFallback ?? defaultAskFallback,
            autoAllowSkills: defaults.autoAllowSkills ?? defaultAutoAllowSkills
        )
    }

    // MARK: - Updates

    static func saveDefaults(_ defaults: ExecApprovalsDefaults) {
        updateFile { file in
            file.defaults = defaults
        }
    }

    static func updateDefaults(_ mutate: (inout ExecApprovalsDefaults) -> Void) {
        updateFile { file in
            var defaults = file.defaults ?? ExecApprovalsDefaults()
            mutate(&defaults)
            file.defaults = defaults
        }
    }

    static func addAllowlistEntry(agentId: String?, pattern: String) {
        let trimmed = pattern.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        updateFile { file in
            let key = agentKey(agentId)
            var agents = file.agents ?? [:]
            var entry = agents[key] ?? ExecApprovalsAgent()
            var allowlist = entry.allowlist ?? []
            if allowlist.contains(where: { $0.pattern == trimmed }) { return }
            allowlist.append(ExecAllowlistEntry(
                pattern: trimmed,
                lastUsedAt: Date().timeIntervalSince1970 * 1000
            ))
            entry.allowlist = allowlist
            agents[key] = entry
            file.agents = agents
        }
    }

    static func recordAllowlistUse(
        agentId: String?,
        pattern: String,
        command: String,
        resolvedPath: String?
    ) {
        updateFile { file in
            let key = agentKey(agentId)
            var agents = file.agents ?? [:]
            var entry = agents[key] ?? ExecApprovalsAgent()
            let allowlist = (entry.allowlist ?? []).map { item -> ExecAllowlistEntry in
                guard item.pattern == pattern else { return item }
                return ExecAllowlistEntry(
                    id: item.id,
                    pattern: item.pattern,
                    lastUsedAt: Date().timeIntervalSince1970 * 1000,
                    lastUsedCommand: command,
                    lastResolvedPath: resolvedPath
                )
            }
            entry.allowlist = allowlist
            agents[key] = entry
            file.agents = agents
        }
    }

    static func updateAllowlist(agentId: String?, allowlist: [ExecAllowlistEntry]) {
        updateFile { file in
            let key = agentKey(agentId)
            var agents = file.agents ?? [:]
            var entry = agents[key] ?? ExecApprovalsAgent()
            let cleaned = allowlist
                .map { item in
                    ExecAllowlistEntry(
                        id: item.id,
                        pattern: item.pattern.trimmingCharacters(in: .whitespacesAndNewlines),
                        lastUsedAt: item.lastUsedAt,
                        lastUsedCommand: item.lastUsedCommand,
                        lastResolvedPath: item.lastResolvedPath
                    )
                }
                .filter { !$0.pattern.isEmpty }
            entry.allowlist = cleaned
            agents[key] = entry
            file.agents = agents
        }
    }

    static func removeAllowlistEntry(agentId: String?, entryId: UUID) {
        updateFile { file in
            let key = agentKey(agentId)
            var agents = file.agents ?? [:]
            var entry = agents[key] ?? ExecApprovalsAgent()
            entry.allowlist = entry.allowlist?.filter { $0.id != entryId }
            agents[key] = entry
            file.agents = agents
        }
    }

    static func updateAgentSettings(agentId: String?, mutate: (inout ExecApprovalsAgent) -> Void) {
        updateFile { file in
            let key = agentKey(agentId)
            var agents = file.agents ?? [:]
            var entry = agents[key] ?? ExecApprovalsAgent()
            mutate(&entry)
            if entry.isEmpty {
                agents.removeValue(forKey: key)
            } else {
                agents[key] = entry
            }
            file.agents = agents.isEmpty ? nil : agents
        }
    }

    // MARK: - Snapshot

    static func readSnapshot() -> ExecApprovalsSnapshot {
        let url = fileURL()
        guard FileManager.default.fileExists(atPath: url.path) else {
            return ExecApprovalsSnapshot(
                path: url.path,
                exists: false,
                hash: hashRaw(nil),
                file: ExecApprovalsFile(version: 1, socket: nil, defaults: nil, agents: [:])
            )
        }
        let raw = try? String(contentsOf: url, encoding: .utf8)
        let data = raw.flatMap { $0.data(using: .utf8) }
        let decoded: ExecApprovalsFile = {
            if let data,
               let file = try? JSONDecoder().decode(ExecApprovalsFile.self, from: data),
               file.version == 1
            {
                return file
            }
            return ExecApprovalsFile(version: 1, socket: nil, defaults: nil, agents: [:])
        }()
        return ExecApprovalsSnapshot(
            path: url.path,
            exists: true,
            hash: hashRaw(raw),
            file: decoded
        )
    }

    // MARK: - Private Helpers

    private static func updateFile(_ mutate: (inout ExecApprovalsFile) -> Void) {
        var file = ensureFile()
        mutate(&file)
        saveFile(file)
    }

    private static func generateToken() -> String {
        var bytes = [UInt8](repeating: 0, count: 24)
        let status = SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes)
        if status == errSecSuccess {
            return Data(bytes)
                .base64EncodedString()
                .replacingOccurrences(of: "+", with: "-")
                .replacingOccurrences(of: "/", with: "_")
                .replacingOccurrences(of: "=", with: "")
        }
        return UUID().uuidString
    }

    private static func hashRaw(_ raw: String?) -> String {
        let data = Data((raw ?? "").utf8)
        let digest = SHA256.hash(data: data)
        return digest.map { String(format: "%02x", $0) }.joined()
    }

    private static func expandPath(_ raw: String) -> String {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed == "~" {
            return FileManager.default.homeDirectoryForCurrentUser.path
        }
        if trimmed.hasPrefix("~/") {
            let suffix = trimmed.dropFirst(2)
            return FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent(String(suffix)).path
        }
        return trimmed
    }

    private static func agentKey(_ agentId: String?) -> String {
        let trimmed = agentId?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return trimmed.isEmpty ? defaultAgentId : trimmed
    }
}
