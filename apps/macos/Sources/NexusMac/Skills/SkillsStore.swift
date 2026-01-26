import Foundation
import Foundation
import OSLog

/// Manages skill extensions for Nexus.
/// Skills are modular capabilities that can be enabled/disabled.
@MainActor
@Observable
final class SkillsStore {
    static let shared = SkillsStore()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "skills")

    // MARK: - State

    private(set) var skills: [Skill] = []
    private(set) var isLoading = false
    private(set) var error: String?
    private(set) var statusMessage: String?

    private var busySkills: Set<String> = []

    private init() {}

    // MARK: - Models

    struct Skill: Identifiable, Codable {
        let name: String
        let description: String
        let source: SkillSource
        let skillKey: String
        let emoji: String?
        let homepage: String?
        var isEnabled: Bool
        var isEligible: Bool
        let requirements: SkillRequirements
        let missing: SkillMissing
        let installOptions: [InstallOption]

        var id: String { skillKey }
    }

    enum SkillSource: String, Codable {
        case bundled = "nexus-bundled"
        case managed = "nexus-managed"
        case workspace = "nexus-workspace"
        case custom = "custom"
    }

    struct SkillRequirements: Codable {
        let binaries: [String]
        let environment: [String]
        let config: [String]
    }

    struct SkillMissing: Codable {
        let binaries: [String]
        let environment: [String]
        let config: [String]

        var isEmpty: Bool {
            binaries.isEmpty && environment.isEmpty && config.isEmpty
        }
    }

    struct InstallOption: Identifiable, Codable {
        let id: String
        let kind: String
        let label: String
        let binaries: [String]
    }

    // MARK: - Public API

    /// Refresh skills from gateway
    func refresh() async {
        guard !isLoading else { return }
        isLoading = true
        error = nil

        do {
            let data = try await ControlChannel.shared.requestAny(
                method: "skills.status",
                params: nil
            )
            let response = decodeResponse(data)

            if let skillsData = response["skills"] as? [[String: Any]] {
                skills = skillsData.compactMap { parseSkill($0) }
                    .sorted { $0.name < $1.name }
                logger.info("loaded \(self.skills.count) skills")
            }
        } catch {
            self.error = error.localizedDescription
            logger.error("failed to load skills: \(error.localizedDescription)")
        }

        isLoading = false
    }

    /// Enable or disable a skill
    func setEnabled(_ enabled: Bool, skillKey: String) async {
        await withBusy(skillKey) {
            do {
                _ = try await ControlChannel.shared.requestAny(
                    method: "skills.update",
                    params: [
                        "skillKey": skillKey,
                        "enabled": enabled,
                    ]
                )
                self.statusMessage = enabled ? "Skill enabled" : "Skill disabled"
                await self.refresh()
            } catch {
                self.statusMessage = error.localizedDescription
            }
        }
    }

    /// Install a skill dependency
    func install(skillKey: String, optionId: String) async {
        await withBusy(skillKey) {
            do {
                let data = try await ControlChannel.shared.requestAny(
                    method: "skills.install",
                    params: [
                        "skillKey": skillKey,
                        "installId": optionId,
                        "timeoutMs": 300_000,
                    ]
                )
                let result = self.decodeResponse(data)

                if let message = result["message"] as? String {
                    self.statusMessage = message
                }
                await self.refresh()
            } catch {
                self.statusMessage = error.localizedDescription
            }
        }
    }

    /// Set environment variable for a skill
    func setEnvironment(skillKey: String, key: String, value: String) async {
        await withBusy(skillKey) {
            do {
                _ = try await ControlChannel.shared.requestAny(
                    method: "skills.update",
                    params: [
                        "skillKey": skillKey,
                        "env": [key: value],
                    ]
                )
                self.statusMessage = "Saved \(key)"
                await self.refresh()
            } catch {
                self.statusMessage = error.localizedDescription
            }
        }
    }

    /// Check if a skill is currently busy
    func isBusy(_ skillKey: String) -> Bool {
        busySkills.contains(skillKey)
    }

    // MARK: - Private

    private func withBusy(_ skillKey: String, work: @escaping () async -> Void) async {
        busySkills.insert(skillKey)
        defer { busySkills.remove(skillKey) }
        await work()
    }

    private func decodeResponse(_ data: Data) -> [String: Any] {
        (try? JSONSerialization.jsonObject(with: data) as? [String: Any]) ?? [:]
    }

    private func parseSkill(_ data: [String: Any]) -> Skill? {
        guard let name = data["name"] as? String,
              let description = data["description"] as? String,
              let skillKey = data["skillKey"] as? String else {
            return nil
        }

        let sourceRaw = data["source"] as? String ?? "custom"
        let source = SkillSource(rawValue: sourceRaw) ?? .custom

        let reqData = data["requirements"] as? [String: Any] ?? [:]
        let requirements = SkillRequirements(
            binaries: reqData["bins"] as? [String] ?? [],
            environment: reqData["env"] as? [String] ?? [],
            config: reqData["config"] as? [String] ?? []
        )

        let missingData = data["missing"] as? [String: Any] ?? [:]
        let missing = SkillMissing(
            binaries: missingData["bins"] as? [String] ?? [],
            environment: missingData["env"] as? [String] ?? [],
            config: missingData["config"] as? [String] ?? []
        )

        let installData = data["install"] as? [[String: Any]] ?? []
        let installOptions = installData.compactMap { opt -> InstallOption? in
            guard let id = opt["id"] as? String,
                  let kind = opt["kind"] as? String,
                  let label = opt["label"] as? String else {
                return nil
            }
            return InstallOption(
                id: id,
                kind: kind,
                label: label,
                binaries: opt["bins"] as? [String] ?? []
            )
        }

        return Skill(
            name: name,
            description: description,
            source: source,
            skillKey: skillKey,
            emoji: data["emoji"] as? String,
            homepage: data["homepage"] as? String,
            isEnabled: !(data["disabled"] as? Bool ?? false),
            isEligible: data["eligible"] as? Bool ?? false,
            requirements: requirements,
            missing: missing,
            installOptions: installOptions
        )
    }
}
