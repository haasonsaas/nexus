import OSLog
import SwiftUI

private let logger = Logger(subsystem: "com.nexus.mac", category: "exec-approvals-settings")

// MARK: - Main Settings View

/// Settings view for configuring exec approval policies and allowlist.
struct ExecApprovalsSettingsView: View {
    @State private var settings: ExecApprovalsResolvedDefaults
    @State private var allowlist: [ExecAllowlistEntry]
    @State private var newPattern: String = ""
    @State private var editingEntryId: UUID?
    @State private var isLoading = false
    @State private var errorMessage: String?

    init() {
        let resolved = ExecApprovalsStore.resolveDefaults()
        let fullResolved = ExecApprovalsStore.resolve(agentId: nil)
        _settings = State(initialValue: resolved)
        _allowlist = State(initialValue: fullResolved.allowlist)
    }

    private var quickMode: ExecApprovalQuickMode {
        ExecApprovalQuickMode.from(security: settings.security, ask: settings.ask)
    }

    var body: some View {
        Form {
            quickModeSection
            advancedSection
            allowlistSection
        }
        .formStyle(.grouped)
        .frame(minWidth: 450)
        .animation(.spring(response: 0.3, dampingFraction: 0.8), value: settings.security)
        .animation(.spring(response: 0.3, dampingFraction: 0.8), value: allowlist.count)
        .overlay {
            if isLoading {
                LoadingOverlay(message: "Saving...", isPresented: true)
            }
        }
        .overlay(alignment: .top) {
            if let error = errorMessage {
                ErrorBanner(message: error, severity: .warning) {
                    withAnimation {
                        errorMessage = nil
                    }
                }
                .padding()
            }
        }
    }

    // MARK: - Quick Mode Section

    private var quickModeSection: some View {
        Section {
            VStack(alignment: .leading, spacing: 12) {
                Picker("Mode", selection: Binding(
                    get: { quickMode },
                    set: { applyQuickMode($0) }
                )) {
                    ForEach(ExecApprovalQuickMode.allCases) { mode in
                        Text(mode.title).tag(mode)
                    }
                }
                .pickerStyle(.segmented)

                Text(quickModeDescription)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        } header: {
            Text("Quick Mode")
        }
    }

    private var quickModeDescription: String {
        switch quickMode {
        case .deny:
            return "Block all command execution. No commands will be allowed to run."
        case .ask:
            return "Prompt for approval when a command doesn't match the allowlist."
        case .allow:
            return "Allow all commands without prompting. Use with caution."
        }
    }

    private func applyQuickMode(_ mode: ExecApprovalQuickMode) {
        settings.security = mode.security
        settings.ask = mode.ask
        saveDefaults()
        logger.info("Quick mode changed to: \(mode.rawValue)")
    }

    // MARK: - Advanced Section

    private var advancedSection: some View {
        Section {
            Picker("Security Level", selection: Binding(
                get: { settings.security },
                set: { newValue in
                    settings.security = newValue
                    saveDefaults()
                }
            )) {
                ForEach(ExecSecurity.allCases) { level in
                    VStack(alignment: .leading) {
                        Text(level.title)
                    }
                    .tag(level)
                }
            }
            .pickerStyle(.menu)

            Text(settings.security.description)
                .font(.caption)
                .foregroundStyle(.secondary)

            Picker("Ask Mode", selection: Binding(
                get: { settings.ask },
                set: { newValue in
                    settings.ask = newValue
                    saveDefaults()
                }
            )) {
                ForEach(ExecAsk.allCases) { mode in
                    Text(mode.title).tag(mode)
                }
            }
            .pickerStyle(.menu)

            Text(settings.ask.description)
                .font(.caption)
                .foregroundStyle(.secondary)

            Toggle("Auto-allow Skills", isOn: Binding(
                get: { settings.autoAllowSkills },
                set: { newValue in
                    settings.autoAllowSkills = newValue
                    saveDefaults()
                }
            ))

            Text("Automatically allow commands from trusted skills.")
                .font(.caption)
                .foregroundStyle(.secondary)
        } header: {
            Text("Advanced")
        }
    }

    // MARK: - Persistence

    private func saveDefaults() {
        let defaults = ExecApprovalsDefaults(
            security: settings.security,
            ask: settings.ask,
            askFallback: settings.askFallback,
            autoAllowSkills: settings.autoAllowSkills
        )
        ExecApprovalsStore.saveDefaults(defaults)
    }

    // MARK: - Allowlist Section

    private var allowlistSection: some View {
        Section {
            if allowlist.isEmpty {
                EmptyAllowlistView()
            } else {
                ForEach(allowlist) { entry in
                    AllowlistEntryRow(
                        entry: entry,
                        isEditing: editingEntryId == entry.id,
                        onStartEdit: { editingEntryId = entry.id },
                        onSave: { newPattern in
                            updateEntry(entry, pattern: newPattern)
                            editingEntryId = nil
                        },
                        onCancel: { editingEntryId = nil },
                        onDelete: { deleteEntry(entry) }
                    )
                }
            }

            AddPatternRow(
                pattern: $newPattern,
                onAdd: addNewPattern
            )
        } header: {
            HStack {
                Text("Allowlist")
                Spacer()
                Text("\(allowlist.count) patterns")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        } footer: {
            Text("Patterns support wildcards: * matches any characters in a path segment, ** matches across directories.")
                .font(.caption)
                .foregroundStyle(.tertiary)
        }
    }

    private func addNewPattern() {
        let trimmed = newPattern.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        guard !allowlist.contains(where: { $0.pattern == trimmed }) else {
            errorMessage = "Pattern already exists"
            return
        }

        ExecApprovalsStore.addAllowlistEntry(agentId: nil, pattern: trimmed)
        allowlist = ExecApprovalsStore.resolve(agentId: nil).allowlist
        newPattern = ""
        logger.info("Added allowlist pattern: \(trimmed)")
    }

    private func updateEntry(_ entry: ExecAllowlistEntry, pattern: String) {
        let trimmed = pattern.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }

        var updated = allowlist
        if let index = updated.firstIndex(where: { $0.id == entry.id }) {
            updated[index] = ExecAllowlistEntry(
                id: entry.id,
                pattern: trimmed,
                lastUsedAt: entry.lastUsedAt,
                lastUsedCommand: entry.lastUsedCommand,
                lastResolvedPath: entry.lastResolvedPath
            )
        }
        ExecApprovalsStore.updateAllowlist(agentId: nil, allowlist: updated)
        allowlist = ExecApprovalsStore.resolve(agentId: nil).allowlist
        logger.info("Updated allowlist pattern: \(trimmed)")
    }

    private func deleteEntry(_ entry: ExecAllowlistEntry) {
        ExecApprovalsStore.removeAllowlistEntry(agentId: nil, entryId: entry.id)
        allowlist = ExecApprovalsStore.resolve(agentId: nil).allowlist
        logger.info("Deleted allowlist pattern: \(entry.pattern)")
    }
}

// MARK: - Empty Allowlist View

private struct EmptyAllowlistView: View {
    var body: some View {
        HStack {
            Spacer()
            VStack(spacing: 8) {
                Image(systemName: "list.bullet.rectangle")
                    .font(.system(size: 24))
                    .foregroundStyle(.tertiary)
                Text("No patterns")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                Text("Add patterns to automatically allow specific commands")
                    .font(.caption)
                    .foregroundStyle(.tertiary)
                    .multilineTextAlignment(.center)
            }
            .padding(.vertical, 12)
            Spacer()
        }
    }
}

// MARK: - Allowlist Entry Row

private struct AllowlistEntryRow: View {
    let entry: ExecAllowlistEntry
    let isEditing: Bool
    let onStartEdit: () -> Void
    let onSave: (String) -> Void
    let onCancel: () -> Void
    let onDelete: () -> Void

    @State private var editedPattern: String = ""
    @State private var isHovered = false

    var body: some View {
        if isEditing {
            editingView
        } else {
            displayView
        }
    }

    private var displayView: some View {
        HStack(spacing: 12) {
            Image(systemName: "terminal")
                .font(.system(size: 14))
                .foregroundStyle(.secondary)
                .frame(width: 20)

            VStack(alignment: .leading, spacing: 2) {
                Text(entry.pattern)
                    .font(.body.monospaced())

                if let lastUsed = entry.lastUsedDate {
                    Text("Last used: \(lastUsed, style: .relative) ago")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
            }

            Spacer()

            if isHovered {
                HStack(spacing: 4) {
                    Button {
                        editedPattern = entry.pattern
                        onStartEdit()
                    } label: {
                        Image(systemName: "pencil")
                            .font(.caption)
                    }
                    .buttonStyle(.borderless)

                    Button(role: .destructive) {
                        onDelete()
                    } label: {
                        Image(systemName: "trash")
                            .font(.caption)
                    }
                    .buttonStyle(.borderless)
                }
                .transition(.opacity.combined(with: .scale(scale: 0.9)))
            }
        }
        .padding(.vertical, 4)
        .padding(.horizontal, 8)
        .background(
            RoundedRectangle(cornerRadius: 6, style: .continuous)
                .fill(isHovered ? Color.accentColor.opacity(0.08) : Color.clear)
        )
        .contentShape(Rectangle())
        .onHover { hovering in
            withAnimation(.easeOut(duration: 0.15)) {
                isHovered = hovering
            }
        }
    }

    private var editingView: some View {
        HStack(spacing: 8) {
            TextField("Pattern", text: $editedPattern)
                .textFieldStyle(.roundedBorder)
                .font(.body.monospaced())
                .onSubmit {
                    onSave(editedPattern)
                }

            Button("Save") {
                onSave(editedPattern)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.small)
            .disabled(editedPattern.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)

            Button("Cancel") {
                onCancel()
            }
            .buttonStyle(.bordered)
            .controlSize(.small)
        }
        .padding(.vertical, 4)
    }
}

// MARK: - Add Pattern Row

private struct AddPatternRow: View {
    @Binding var pattern: String
    let onAdd: () -> Void

    @FocusState private var isFocused: Bool

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "plus.circle")
                .font(.system(size: 14))
                .foregroundStyle(.secondary)
                .frame(width: 20)

            TextField("Add pattern (e.g., /usr/bin/git, npm)", text: $pattern)
                .textFieldStyle(.roundedBorder)
                .font(.body.monospaced())
                .focused($isFocused)
                .onSubmit {
                    onAdd()
                }

            Button("Add") {
                onAdd()
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.small)
            .disabled(pattern.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        }
        .padding(.vertical, 4)
    }
}

// MARK: - Preview

#Preview {
    ExecApprovalsSettingsView()
        .frame(width: 500, height: 600)
}
