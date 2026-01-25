import SwiftUI

/// View for managing CLI installation.
struct CLIInstallerView: View {
    @State private var installer = CLIInstaller.shared
    @State private var selectedMethod: CLIInstaller.InstallMethod = .direct
    @State private var methodAvailability: [CLIInstaller.InstallMethod: Bool] = [:]
    @State private var showingPathInstructions = false

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            headerSection
            statusSection

            if !installer.state.isInstalled {
                installMethodSection
                installButton
            } else {
                installedActionsSection
            }

            if showingPathInstructions {
                pathInstructionsSection
            }

            Spacer(minLength: 0)
        }
        .padding()
        .task {
            await installer.checkInstallation()
            await checkMethodAvailability()
        }
    }

    // MARK: - Header

    private var headerSection: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("CLI Installation")
                .font(.headline)

            Text("Install the Nexus command-line tool to use Nexus from your terminal.")
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
    }

    // MARK: - Status Section

    @ViewBuilder
    private var statusSection: some View {
        GroupBox {
            HStack(spacing: 12) {
                statusIcon
                    .font(.title)
                    .frame(width: 40)

                VStack(alignment: .leading, spacing: 4) {
                    Text(statusTitle)
                        .font(.headline)

                    Text(statusSubtitle)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)

                    if let path = installer.state.installedPath {
                        HStack(spacing: 4) {
                            Text(path)
                                .font(.caption.monospaced())
                                .foregroundStyle(.secondary)

                            Button {
                                NSPasteboard.general.clearContents()
                                NSPasteboard.general.setString(path, forType: .string)
                            } label: {
                                Image(systemName: "doc.on.doc")
                                    .font(.caption)
                            }
                            .buttonStyle(.plain)
                            .foregroundStyle(.secondary)
                            .help("Copy path")
                        }
                    }
                }

                Spacer()

                if installer.isChecking {
                    ProgressView()
                        .controlSize(.small)
                } else {
                    Button {
                        Task { await installer.checkInstallation() }
                    } label: {
                        Image(systemName: "arrow.clockwise")
                    }
                    .buttonStyle(.borderless)
                    .help("Refresh status")
                }
            }
            .padding(.vertical, 4)
        }

        if let error = installer.error {
            ErrorBanner(message: error.localizedDescription)
        }
    }

    private var statusIcon: some View {
        Group {
            switch installer.state {
            case .notInstalled:
                Image(systemName: "xmark.circle.fill")
                    .foregroundStyle(.secondary)
            case .installed:
                Image(systemName: "checkmark.circle.fill")
                    .foregroundStyle(.green)
            case .outdated:
                Image(systemName: "exclamationmark.triangle.fill")
                    .foregroundStyle(.orange)
            case .installing:
                ProgressView()
            }
        }
    }

    private var statusTitle: String {
        switch installer.state {
        case .notInstalled:
            return "Not Installed"
        case .installed(let version, _):
            return "Installed (v\(version))"
        case .outdated(let installed, let latest, _):
            return "Update Available (v\(installed) -> v\(latest))"
        case .installing:
            return "Installing..."
        }
    }

    private var statusSubtitle: String {
        switch installer.state {
        case .notInstalled:
            return "The Nexus CLI is not installed on this system."
        case .installed:
            return "Ready to use from your terminal."
        case .outdated:
            return "A newer version is available."
        case .installing:
            return installer.installProgress ?? "Please wait..."
        }
    }

    // MARK: - Install Method Section

    private var installMethodSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Installation Method")
                .font(.subheadline.weight(.medium))

            VStack(spacing: 8) {
                ForEach(CLIInstaller.InstallMethod.allCases) { method in
                    InstallMethodRow(
                        method: method,
                        isSelected: selectedMethod == method,
                        isAvailable: methodAvailability[method] ?? false,
                        onSelect: { selectedMethod = method }
                    )
                }
            }
        }
    }

    // MARK: - Install Button

    private var installButton: some View {
        HStack {
            Spacer()

            Button {
                Task {
                    do {
                        try await installer.install(method: selectedMethod)
                        showingPathInstructions = true
                    } catch {
                        // Error is already captured in installer.error
                    }
                }
            } label: {
                if installer.isInstalling {
                    ProgressView()
                        .controlSize(.small)
                        .padding(.horizontal, 8)
                } else {
                    Text("Install")
                }
            }
            .buttonStyle(.borderedProminent)
            .disabled(installer.isInstalling || !(methodAvailability[selectedMethod] ?? false))
        }
    }

    // MARK: - Installed Actions

    private var installedActionsSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Actions")
                .font(.subheadline.weight(.medium))

            HStack(spacing: 12) {
                Button {
                    installer.openTerminal(at: installer.state.installedPath)
                } label: {
                    Label("Open in Terminal", systemImage: "terminal")
                }
                .buttonStyle(.bordered)

                if case .outdated = installer.state {
                    Button {
                        Task {
                            try? await installer.update()
                        }
                    } label: {
                        Label("Update", systemImage: "arrow.down.circle")
                    }
                    .buttonStyle(.borderedProminent)
                }

                Spacer()

                Button(role: .destructive) {
                    Task {
                        try? await installer.uninstall()
                    }
                } label: {
                    Label("Uninstall", systemImage: "trash")
                }
                .buttonStyle(.bordered)
            }

            Button {
                showingPathInstructions.toggle()
            } label: {
                HStack {
                    Text("PATH Configuration")
                        .font(.subheadline)
                    Spacer()
                    Image(systemName: showingPathInstructions ? "chevron.up" : "chevron.down")
                        .font(.caption)
                }
                .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)
        }
    }

    // MARK: - PATH Instructions

    private var pathInstructionsSection: some View {
        GroupBox {
            VStack(alignment: .leading, spacing: 12) {
                Label("PATH Configuration", systemImage: "terminal")
                    .font(.subheadline.weight(.medium))

                Text("To use the `nexus` command from any terminal, ensure the installation directory is in your PATH.")
                    .font(.caption)
                    .foregroundStyle(.secondary)

                Divider()

                VStack(alignment: .leading, spacing: 8) {
                    Text("Add to ~/.zshrc or ~/.bashrc:")
                        .font(.caption.weight(.medium))

                    HStack {
                        Text("export PATH=\"\(CLIInstaller.defaultInstallDir):$PATH\"")
                            .font(.caption.monospaced())
                            .padding(8)
                            .background(Color(NSColor.textBackgroundColor))
                            .cornerRadius(4)

                        Button {
                            let cmd = "export PATH=\"\(CLIInstaller.defaultInstallDir):$PATH\""
                            NSPasteboard.general.clearContents()
                            NSPasteboard.general.setString(cmd, forType: .string)
                        } label: {
                            Image(systemName: "doc.on.doc")
                        }
                        .buttonStyle(.borderless)
                        .help("Copy to clipboard")
                    }
                }

                let profiles = installer.shellProfilesNeedingUpdate()
                if !profiles.isEmpty {
                    HStack(spacing: 4) {
                        Image(systemName: "exclamationmark.triangle.fill")
                            .foregroundStyle(.orange)
                            .font(.caption)

                        Text("PATH not configured in: \(profiles.map { ($0 as NSString).lastPathComponent }.joined(separator: ", "))")
                            .font(.caption)
                            .foregroundStyle(.orange)
                    }
                } else if installer.state.isInstalled {
                    HStack(spacing: 4) {
                        Image(systemName: "checkmark.circle.fill")
                            .foregroundStyle(.green)
                            .font(.caption)

                        Text("PATH is configured correctly")
                            .font(.caption)
                            .foregroundStyle(.green)
                    }
                }
            }
        }
    }

    // MARK: - Helpers

    private func checkMethodAvailability() async {
        methodAvailability[.direct] = installer.isBundledBinaryAvailable()
        methodAvailability[.homebrew] = await installer.isHomebrewAvailable()
        methodAvailability[.goInstall] = await installer.isGoAvailable()

        // Select first available method
        if let first = CLIInstaller.InstallMethod.allCases.first(where: { methodAvailability[$0] == true }) {
            selectedMethod = first
        }
    }
}

// MARK: - Install Method Row

struct InstallMethodRow: View {
    let method: CLIInstaller.InstallMethod
    let isSelected: Bool
    let isAvailable: Bool
    let onSelect: () -> Void

    var body: some View {
        Button(action: onSelect) {
            HStack(spacing: 12) {
                Image(systemName: method.icon)
                    .font(.title3)
                    .foregroundStyle(isAvailable ? (isSelected ? Color.accentColor : .secondary) : .secondary.opacity(0.5))
                    .frame(width: 24)

                VStack(alignment: .leading, spacing: 2) {
                    HStack {
                        Text(method.rawValue)
                            .font(.subheadline.weight(.medium))

                        if !isAvailable {
                            Text("(Not Available)")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }

                    Text(method.description)
                        .font(.caption)
                        .foregroundStyle(.secondary)

                    Text(method.command)
                        .font(.caption.monospaced())
                        .foregroundStyle(.tertiary)
                }

                Spacer()

                if isSelected && isAvailable {
                    Image(systemName: "checkmark.circle.fill")
                        .foregroundStyle(.accent)
                }
            }
            .padding(10)
            .background(
                RoundedRectangle(cornerRadius: 8, style: .continuous)
                    .fill(isSelected && isAvailable ? Color.accentColor.opacity(0.1) : Color.clear)
            )
            .overlay(
                RoundedRectangle(cornerRadius: 8, style: .continuous)
                    .strokeBorder(isSelected && isAvailable ? Color.accentColor.opacity(0.4) : Color.secondary.opacity(0.2), lineWidth: 1)
            )
        }
        .buttonStyle(.plain)
        .disabled(!isAvailable)
        .opacity(isAvailable ? 1 : 0.6)
    }
}

// MARK: - Error Banner

struct ErrorBanner: View {
    let message: String

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.red)

            Text(message)
                .font(.caption)
                .foregroundStyle(.red)

            Spacer()
        }
        .padding(10)
        .background(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .fill(Color.red.opacity(0.1))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .strokeBorder(Color.red.opacity(0.3), lineWidth: 1)
        )
    }
}

// MARK: - Compact Variant

/// Compact view for showing CLI status in settings
struct CLIInstallerCompactView: View {
    @State private var installer = CLIInstaller.shared

    var body: some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text("CLI Tool")
                    .font(.subheadline.weight(.medium))

                Text(statusText)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            statusBadge
        }
        .task {
            await installer.checkInstallation()
        }
    }

    private var statusText: String {
        switch installer.state {
        case .notInstalled:
            return "Not installed"
        case .installed(let version, _):
            return "v\(version) installed"
        case .outdated(let installed, let latest, _):
            return "v\(installed) (v\(latest) available)"
        case .installing:
            return "Installing..."
        }
    }

    @ViewBuilder
    private var statusBadge: some View {
        switch installer.state {
        case .notInstalled:
            Image(systemName: "xmark.circle.fill")
                .foregroundStyle(.secondary)
        case .installed:
            Image(systemName: "checkmark.circle.fill")
                .foregroundStyle(.green)
        case .outdated:
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.orange)
        case .installing:
            ProgressView()
                .controlSize(.small)
        }
    }
}

// MARK: - Preview

#Preview("Full View") {
    CLIInstallerView()
        .frame(width: 500, height: 600)
}

#Preview("Compact View") {
    CLIInstallerCompactView()
        .padding()
        .frame(width: 300)
}
