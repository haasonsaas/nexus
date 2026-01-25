import SwiftUI

/// Skills management settings view.
struct SkillsSettingsView: View {
    @State private var store = SkillsStore.shared
    @State private var filter: SkillsFilter = .all
    @State private var envEditorSkill: SkillsStore.Skill?
    @State private var envEditorKey: String = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            headerView
            statusBanner
            skillsList
            Spacer(minLength: 0)
        }
        .task {
            await store.refresh()
        }
        .sheet(item: $envEditorSkill) { skill in
            EnvEditorSheet(
                skillName: skill.name,
                envKey: envEditorKey,
                onSave: { value in
                    Task {
                        await store.setEnvironment(
                            skillKey: skill.skillKey,
                            key: envEditorKey,
                            value: value
                        )
                    }
                }
            )
        }
    }

    // MARK: - Header

    private var headerView: some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                Text("Skills")
                    .font(.headline)
                Text("Skills are enabled when their requirements are met.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            if store.isLoading {
                ProgressView()
            } else {
                Button {
                    Task { await store.refresh() }
                } label: {
                    Label("Refresh", systemImage: "arrow.clockwise")
                }
                .buttonStyle(.bordered)
            }

            Picker("Filter", selection: $filter) {
                ForEach(SkillsFilter.allCases) { filter in
                    Text(filter.title).tag(filter)
                }
            }
            .labelsHidden()
            .pickerStyle(.menu)
            .frame(width: 140)
        }
        .padding(.horizontal)
    }

    // MARK: - Status Banner

    @ViewBuilder
    private var statusBanner: some View {
        if let error = store.error {
            Text(error)
                .font(.footnote)
                .foregroundStyle(.orange)
                .padding(.horizontal)
        } else if let message = store.statusMessage {
            Text(message)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .padding(.horizontal)
        }
    }

    // MARK: - Skills List

    @ViewBuilder
    private var skillsList: some View {
        if store.skills.isEmpty && !store.isLoading {
            ContentUnavailableView(
                "No Skills",
                systemImage: "puzzlepiece.extension",
                description: Text("Connect to the gateway to see available skills")
            )
        } else {
            List {
                ForEach(filteredSkills) { skill in
                    SkillRowView(
                        skill: skill,
                        isBusy: store.isBusy(skill.skillKey),
                        onToggleEnabled: { enabled in
                            Task { await store.setEnabled(enabled, skillKey: skill.skillKey) }
                        },
                        onInstall: { optionId in
                            Task { await store.install(skillKey: skill.skillKey, optionId: optionId) }
                        },
                        onSetEnv: { envKey in
                            envEditorKey = envKey
                            envEditorSkill = skill
                        }
                    )
                }

                if !store.skills.isEmpty && filteredSkills.isEmpty {
                    Text("No skills match this filter.")
                        .font(.callout)
                        .foregroundStyle(.secondary)
                }
            }
            .listStyle(.inset)
        }
    }

    private var filteredSkills: [SkillsStore.Skill] {
        store.skills.filter { skill in
            switch filter {
            case .all:
                return true
            case .ready:
                return skill.isEnabled && skill.isEligible
            case .needsSetup:
                return skill.isEnabled && !skill.isEligible
            case .disabled:
                return !skill.isEnabled
            }
        }
    }
}

// MARK: - Filter

enum SkillsFilter: String, CaseIterable, Identifiable {
    case all
    case ready
    case needsSetup
    case disabled

    var id: String { rawValue }

    var title: String {
        switch self {
        case .all: return "All"
        case .ready: return "Ready"
        case .needsSetup: return "Needs Setup"
        case .disabled: return "Disabled"
        }
    }
}

// MARK: - Skill Row

struct SkillRowView: View {
    let skill: SkillsStore.Skill
    let isBusy: Bool
    let onToggleEnabled: (Bool) -> Void
    let onInstall: (String) -> Void
    let onSetEnv: (String) -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            // Emoji
            Text(skill.emoji ?? "")
                .font(.title2)

            // Content
            VStack(alignment: .leading, spacing: 6) {
                Text(skill.name)
                    .font(.headline)

                Text(skill.description)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)

                // Source tag
                HStack(spacing: 8) {
                    SkillTag(text: skill.source.displayName)

                    if let url = skill.homepage.flatMap({ URL(string: $0) }) {
                        Link(destination: url) {
                            Label("Website", systemImage: "link")
                                .font(.caption2.weight(.semibold))
                        }
                        .buttonStyle(.link)
                    }
                }

                // Missing requirements
                if !skill.missing.isEmpty {
                    VStack(alignment: .leading, spacing: 2) {
                        if !skill.missing.binaries.isEmpty {
                            Text("Missing: \(skill.missing.binaries.joined(separator: ", "))")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        if !skill.missing.environment.isEmpty {
                            Text("Missing env: \(skill.missing.environment.joined(separator: ", "))")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                }

                // Environment actions
                if !skill.missing.environment.isEmpty {
                    HStack(spacing: 8) {
                        ForEach(skill.missing.environment, id: \.self) { envKey in
                            Button("Set \(envKey)") {
                                onSetEnv(envKey)
                            }
                            .buttonStyle(.bordered)
                            .controlSize(.small)
                            .disabled(isBusy)
                        }
                    }
                }
            }

            Spacer(minLength: 0)

            // Actions
            VStack(alignment: .trailing, spacing: 8) {
                if !skill.installOptions.isEmpty && !skill.missing.binaries.isEmpty {
                    ForEach(skill.installOptions) { option in
                        Button(option.label) {
                            onInstall(option.id)
                        }
                        .buttonStyle(.borderedProminent)
                        .controlSize(.small)
                        .disabled(isBusy)
                    }
                } else {
                    Toggle("", isOn: Binding(
                        get: { skill.isEnabled },
                        set: { onToggleEnabled($0) }
                    ))
                    .toggleStyle(.switch)
                    .labelsHidden()
                    .disabled(isBusy || !skill.missing.isEmpty)
                }

                if isBusy {
                    ProgressView()
                        .controlSize(.small)
                }
            }
        }
        .padding(.vertical, 6)
    }
}

struct SkillTag: View {
    let text: String

    var body: some View {
        Text(text)
            .font(.caption2.weight(.semibold))
            .foregroundStyle(.secondary)
            .padding(.horizontal, 8)
            .padding(.vertical, 2)
            .background(Color.secondary.opacity(0.12))
            .clipShape(Capsule())
    }
}

// MARK: - Env Editor

struct EnvEditorSheet: View {
    let skillName: String
    let envKey: String
    let onSave: (String) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var value = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Set Environment Variable")
                .font(.headline)

            Text("Skill: \(skillName)")
                .font(.subheadline)
                .foregroundStyle(.secondary)

            SecureField(envKey, text: $value)
                .textFieldStyle(.roundedBorder)

            HStack {
                Button("Cancel") {
                    dismiss()
                }

                Spacer()

                Button("Save") {
                    onSave(value)
                    dismiss()
                }
                .buttonStyle(.borderedProminent)
                .disabled(value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
        }
        .padding(20)
        .frame(width: 400)
    }
}

// MARK: - Extensions

extension SkillsStore.SkillSource {
    var displayName: String {
        switch self {
        case .bundled: return "Bundled"
        case .managed: return "Managed"
        case .workspace: return "Workspace"
        case .custom: return "Custom"
        }
    }
}

extension SkillsStore.Skill: @unchecked Sendable {}

#Preview {
    SkillsSettingsView()
        .frame(width: 500, height: 600)
}
