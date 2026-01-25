import SwiftUI

/// Settings view for security controls including exec approvals.
struct SecuritySettingsView: View {
    @State private var execApprovals = ExecApprovalsService.shared
    @State private var defaults = ExecApprovalsStore.resolveDefaults()
    @State private var quickMode: ExecApprovalQuickMode = .ask
    @State private var allowlist: [ExecAllowlistEntry] = []
    @State private var newPattern = ""
    @State private var isAddingPattern = false

    var body: some View {
        Form {
            // Quick Mode Section
            Section("Command Execution") {
                Picker("Security Mode", selection: $quickMode) {
                    ForEach(ExecApprovalQuickMode.allCases, id: \.self) { mode in
                        Text(mode.title).tag(mode)
                    }
                }
                .pickerStyle(.segmented)
                .onChange(of: quickMode) { _, newValue in
                    updateQuickMode(newValue)
                }

                Text(quickModeDescription)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            // Advanced Settings
            Section("Advanced Settings") {
                Picker("Security Level", selection: Binding(
                    get: { defaults.security },
                    set: { newValue in
                        defaults.security = newValue
                        saveDefaults()
                    }
                )) {
                    ForEach(ExecSecurity.allCases, id: \.self) { level in
                        Text(level.title).tag(level)
                    }
                }

                Picker("Ask Mode", selection: Binding(
                    get: { defaults.ask },
                    set: { newValue in
                        defaults.ask = newValue
                        saveDefaults()
                    }
                )) {
                    ForEach(ExecAsk.allCases, id: \.self) { mode in
                        Text(mode.title).tag(mode)
                    }
                }

                Toggle("Auto-allow Skill Binaries", isOn: Binding(
                    get: { defaults.autoAllowSkills },
                    set: { newValue in
                        defaults.autoAllowSkills = newValue
                        saveDefaults()
                    }
                ))
            }

            // Allowlist Section
            Section("Command Allowlist") {
                if allowlist.isEmpty {
                    Text("No patterns added")
                        .foregroundStyle(.secondary)
                        .italic()
                } else {
                    ForEach(allowlist) { entry in
                        HStack {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(entry.pattern)
                                    .font(.system(.body, design: .monospaced))
                                if let lastUsed = entry.lastUsedDate {
                                    Text("Last used: \(lastUsed.formatted(.relative(presentation: .named)))")
                                        .font(.caption2)
                                        .foregroundStyle(.tertiary)
                                }
                            }
                            Spacer()
                            Button {
                                removeEntry(entry)
                            } label: {
                                Image(systemName: "trash")
                                    .foregroundStyle(.red)
                            }
                            .buttonStyle(.plain)
                        }
                    }
                }

                HStack {
                    TextField("Pattern (e.g., /usr/bin/git)", text: $newPattern)
                        .textFieldStyle(.roundedBorder)
                    Button("Add") {
                        addPattern()
                    }
                    .disabled(newPattern.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            }

            // Status Section
            Section("Service Status") {
                HStack {
                    Text("Approval Socket")
                    Spacer()
                    if execApprovals.isRunning {
                        Label("Running", systemImage: "checkmark.circle.fill")
                            .foregroundStyle(.green)
                    } else {
                        Label("Stopped", systemImage: "xmark.circle.fill")
                            .foregroundStyle(.red)
                    }
                }

                if !execApprovals.pendingRequests.isEmpty {
                    Text("\(execApprovals.pendingRequests.count) pending approval(s)")
                        .font(.caption)
                        .foregroundStyle(.orange)
                }
            }
        }
        .formStyle(.grouped)
        .padding()
        .onAppear {
            loadData()
        }
    }

    // MARK: - Helpers

    private var quickModeDescription: String {
        switch quickMode {
        case .deny:
            return "All command execution is blocked"
        case .ask:
            return "Commands are allowed if they match the allowlist, otherwise you'll be prompted"
        case .allow:
            return "All commands are allowed without prompting"
        }
    }

    private func loadData() {
        defaults = ExecApprovalsStore.resolveDefaults()
        quickMode = ExecApprovalQuickMode.from(security: defaults.security, ask: defaults.ask)
        let resolved = ExecApprovalsStore.resolve(agentId: nil)
        allowlist = resolved.allowlist
    }

    private func updateQuickMode(_ mode: ExecApprovalQuickMode) {
        defaults.security = mode.security
        defaults.ask = mode.ask
        saveDefaults()
    }

    private func saveDefaults() {
        ExecApprovalsStore.saveDefaults(ExecApprovalsDefaults(
            security: defaults.security,
            ask: defaults.ask,
            askFallback: defaults.askFallback,
            autoAllowSkills: defaults.autoAllowSkills
        ))
    }

    private func addPattern() {
        let trimmed = newPattern.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else { return }
        ExecApprovalsStore.addAllowlistEntry(agentId: nil, pattern: trimmed)
        newPattern = ""
        loadData()
    }

    private func removeEntry(_ entry: ExecAllowlistEntry) {
        ExecApprovalsStore.removeAllowlistEntry(agentId: nil, entryId: entry.id)
        loadData()
    }
}

#Preview {
    SecuritySettingsView()
}
