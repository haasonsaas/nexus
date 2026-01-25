import Foundation

/// ConfigSnapshot represents the response from config.get
struct ConfigSnapshot: Codable {
    struct Issue: Codable {
        let path: String
        let message: String
    }

    let path: String?
    let exists: Bool?
    let raw: String?
    let hash: String?
    let parsed: AnyCodable?
    let valid: Bool?
    let config: [String: AnyCodable]?
    let issues: [Issue]?
}

/// ConfigStore provides config persistence through the gateway with hash-based conflict detection.
enum ConfigStore {
    struct Overrides: Sendable {
        var loadLocal: (@MainActor @Sendable () -> [String: Any])?
        var saveLocal: (@MainActor @Sendable ([String: Any]) -> Void)?
        var loadRemote: (@MainActor @Sendable () async -> [String: Any])?
        var saveRemote: (@MainActor @Sendable ([String: Any]) async throws -> Void)?
    }

    private actor OverrideStore {
        var overrides = Overrides()

        func setOverride(_ overrides: Overrides) {
            self.overrides = overrides
        }
    }

    private static let overrideStore = OverrideStore()
    @MainActor private static var lastHash: String?

    /// Load configuration from the gateway, falling back to local config file
    @MainActor
    static func load() async -> [String: Any] {
        let overrides = await self.overrideStore.overrides
        if let override = overrides.loadRemote {
            return await override()
        }
        if let gateway = await self.loadFromGateway() {
            return gateway
        }
        if let override = overrides.loadLocal {
            return override()
        }
        return loadLocalConfigFile()
    }

    /// Save configuration to the gateway with optimistic locking
    @MainActor
    static func save(_ root: sending [String: Any]) async throws {
        let overrides = await self.overrideStore.overrides
        if let override = overrides.saveRemote {
            try await override(root)
        } else {
            do {
                try await self.saveToGateway(root)
            } catch {
                if let override = overrides.saveLocal {
                    override(root)
                } else {
                    saveLocalConfigFile(root)
                }
            }
        }
    }

    /// Request config.get from gateway, storing hash for optimistic locking
    @MainActor
    private static func loadFromGateway() async -> [String: Any]? {
        do {
            let data = try await GatewayConnection.shared.request(
                method: "config.get",
                params: nil,
                timeoutMs: 8000
            )
            let snap = try JSONDecoder().decode(ConfigSnapshot.self, from: data)
            self.lastHash = snap.hash
            return snap.config?.mapValues { $0.value } ?? [:]
        } catch {
            return nil
        }
    }

    /// Send config.set to gateway with raw JSON and baseHash for conflict detection
    @MainActor
    private static func saveToGateway(_ root: [String: Any]) async throws {
        // Ensure we have a hash for optimistic locking
        if self.lastHash == nil {
            _ = await self.loadFromGateway()
        }

        let data = try JSONSerialization.data(withJSONObject: root, options: [.prettyPrinted, .sortedKeys])
        guard let raw = String(data: data, encoding: .utf8) else {
            throw NSError(domain: "ConfigStore", code: 1, userInfo: [
                NSLocalizedDescriptionKey: "Failed to encode config."
            ])
        }

        var params: [String: AnyCodable] = ["raw": AnyCodable(raw)]
        if let baseHash = self.lastHash {
            params["baseHash"] = AnyCodable(baseHash)
        }

        _ = try await GatewayConnection.shared.request(
            method: "config.set",
            params: params,
            timeoutMs: 10000
        )

        // Reload to get the new hash
        _ = await self.loadFromGateway()
    }

    // MARK: - Local Config File Fallback

    private static func localConfigPath() -> URL {
        let home = FileManager.default.homeDirectoryForCurrentUser
        return home.appendingPathComponent(".nexus/config.json")
    }

    private static func loadLocalConfigFile() -> [String: Any] {
        let path = localConfigPath()
        guard FileManager.default.fileExists(atPath: path.path) else {
            return [:]
        }
        do {
            let data = try Data(contentsOf: path)
            if let dict = try JSONSerialization.jsonObject(with: data) as? [String: Any] {
                return dict
            }
        } catch {
            // Silent failure, return empty
        }
        return [:]
    }

    private static func saveLocalConfigFile(_ root: [String: Any]) {
        let path = localConfigPath()
        do {
            // Ensure directory exists
            let dir = path.deletingLastPathComponent()
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)

            let data = try JSONSerialization.data(withJSONObject: root, options: [.prettyPrinted, .sortedKeys])
            try data.write(to: path, options: .atomic)
        } catch {
            // Silent failure
        }
    }

    // MARK: - Testing Support

    #if DEBUG
    static func _testSetOverrides(_ overrides: Overrides) async {
        await self.overrideStore.setOverride(overrides)
    }

    static func _testClearOverrides() async {
        await self.overrideStore.setOverride(.init())
    }

    @MainActor
    static func _testClearHash() {
        self.lastHash = nil
    }
    #endif
}
