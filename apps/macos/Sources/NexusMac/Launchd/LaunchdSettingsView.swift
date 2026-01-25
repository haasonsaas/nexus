import SwiftUI

/// SwiftUI settings view for managing the Nexus Edge LaunchAgent.
struct LaunchdSettingsView: View {
    @State private var manager = LaunchdManager.shared
    @State private var showingLogs = false
    @State private var logContent = ""
    @State private var selectedLogType: LogType = .stdout

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            headerSection
            Divider()
            statusSection
            Divider()
            controlsSection
            Divider()
            optionsSection
            Divider()
            logsSection

            if let error = manager.lastError {
                errorBanner(error)
            }

            Spacer(minLength: 0)
        }
        .task {
            await manager.refreshStatus()
        }
        .sheet(isPresented: $showingLogs) {
            LogViewerSheet(
                logType: $selectedLogType,
                content: $logContent,
                onRefresh: refreshLogContent
            )
        }
    }

    // MARK: - Header

    private var headerSection: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("Edge Service")
                .font(.headline)
            Text("Manage the Nexus Edge background service via launchd.")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
    }

    // MARK: - Status

    private var statusSection: some View {
        HStack(spacing: 12) {
            statusIndicator
            VStack(alignment: .leading, spacing: 2) {
                Text("Status")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                Text(manager.status.displayName)
                    .font(.body)
            }
            Spacer()
            if manager.isOperating {
                ProgressView()
                    .controlSize(.small)
            } else {
                Button {
                    Task { await manager.refreshStatus() }
                } label: {
                    Label("Refresh", systemImage: "arrow.clockwise")
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
            }
        }
    }

    @ViewBuilder
    private var statusIndicator: some View {
        Circle()
            .fill(statusColor)
            .frame(width: 12, height: 12)
    }

    private var statusColor: Color {
        switch manager.status {
        case .running:
            return .green
        case .loaded:
            return .yellow
        case .installed:
            return .orange
        case .notInstalled:
            return .gray
        case .failed:
            return .red
        }
    }

    // MARK: - Controls

    private var controlsSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Service Controls")
                .font(.subheadline)
                .foregroundStyle(.secondary)

            HStack(spacing: 12) {
                if !manager.isInstalled {
                    Button {
                        Task { await manager.install() }
                    } label: {
                        Label("Install", systemImage: "square.and.arrow.down")
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(manager.isOperating)
                } else {
                    Button {
                        Task { await manager.uninstall() }
                    } label: {
                        Label("Uninstall", systemImage: "trash")
                    }
                    .buttonStyle(.bordered)
                    .disabled(manager.isOperating)
                }

                Divider()
                    .frame(height: 20)

                Button {
                    Task { await manager.start() }
                } label: {
                    Label("Start", systemImage: "play.fill")
                }
                .buttonStyle(.bordered)
                .disabled(!manager.isInstalled || manager.isOperating || manager.status == .running)

                Button {
                    Task { await manager.stop() }
                } label: {
                    Label("Stop", systemImage: "stop.fill")
                }
                .buttonStyle(.bordered)
                .disabled(!manager.isInstalled || manager.isOperating || !manager.status.isHealthy)
            }
        }
    }

    // MARK: - Options

    private var optionsSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Options")
                .font(.subheadline)
                .foregroundStyle(.secondary)

            Toggle(isOn: Binding(
                get: { manager.autoStartEnabled },
                set: { enabled in
                    Task { await manager.setAutoStart(enabled) }
                }
            )) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Start at Login")
                        .font(.body)
                    Text("Automatically start the edge service when you log in")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .toggleStyle(.switch)
            .disabled(!manager.isInstalled || manager.isOperating)

            Toggle(isOn: Binding(
                get: { manager.attachOnlyMode },
                set: { manager.setAttachOnlyMode($0) }
            )) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Attach-Only Mode")
                        .font(.body)
                    Text("Connect to existing sessions without starting new agents")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .toggleStyle(.switch)

            if manager.attachOnlyMode && manager.isInstalled {
                HStack {
                    Image(systemName: "info.circle")
                        .foregroundStyle(.blue)
                    Text("Reinstall the service to apply attach-only mode changes")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    // MARK: - Logs

    private var logsSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Logs")
                .font(.subheadline)
                .foregroundStyle(.secondary)

            HStack(spacing: 12) {
                Button {
                    selectedLogType = .stdout
                    refreshLogContent()
                    showingLogs = true
                } label: {
                    Label("View Logs", systemImage: "doc.text")
                }
                .buttonStyle(.bordered)

                Button {
                    selectedLogType = .stderr
                    refreshLogContent()
                    showingLogs = true
                } label: {
                    Label("View Errors", systemImage: "exclamationmark.triangle")
                }
                .buttonStyle(.bordered)

                Button {
                    manager.openLogsFolder()
                } label: {
                    Label("Open Folder", systemImage: "folder")
                }
                .buttonStyle(.bordered)
            }

            VStack(alignment: .leading, spacing: 4) {
                pathRow(label: "Stdout:", path: manager.stdoutLogPath)
                pathRow(label: "Stderr:", path: manager.stderrLogPath)
            }
        }
    }

    private func pathRow(label: String, path: String) -> some View {
        HStack(spacing: 4) {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(path)
                .font(.caption.monospaced())
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .truncationMode(.middle)
        }
    }

    // MARK: - Error Banner

    private func errorBanner(_ message: String) -> some View {
        HStack {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.orange)
            Text(message)
                .font(.callout)
            Spacer()
        }
        .padding(10)
        .background(Color.orange.opacity(0.1))
        .cornerRadius(8)
    }

    // MARK: - Helpers

    private func refreshLogContent() {
        switch selectedLogType {
        case .stdout:
            logContent = manager.readStdoutLog(lines: 200)
        case .stderr:
            logContent = manager.readStderrLog(lines: 200)
        }
    }
}

// MARK: - Log Type

enum LogType: String, CaseIterable, Identifiable {
    case stdout = "Output"
    case stderr = "Errors"

    var id: String { rawValue }
}

// MARK: - Log Viewer Sheet

struct LogViewerSheet: View {
    @Binding var logType: LogType
    @Binding var content: String
    let onRefresh: () -> Void

    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(spacing: 0) {
            // Header
            HStack {
                Picker("Log Type", selection: $logType) {
                    ForEach(LogType.allCases) { type in
                        Text(type.rawValue).tag(type)
                    }
                }
                .pickerStyle(.segmented)
                .frame(width: 200)
                .onChange(of: logType) { _, _ in
                    onRefresh()
                }

                Spacer()

                Button {
                    onRefresh()
                } label: {
                    Label("Refresh", systemImage: "arrow.clockwise")
                }
                .buttonStyle(.bordered)
                .controlSize(.small)

                Button("Done") {
                    dismiss()
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.small)
            }
            .padding()

            Divider()

            // Log content
            ScrollView {
                if content.isEmpty {
                    ContentUnavailableView(
                        "No Logs",
                        systemImage: "doc.text",
                        description: Text("The log file is empty or does not exist")
                    )
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    Text(content)
                        .font(.system(.caption, design: .monospaced))
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding()
                        .textSelection(.enabled)
                }
            }
            .background(Color(NSColor.textBackgroundColor))
        }
        .frame(width: 700, height: 500)
    }
}

// MARK: - Preview

#Preview {
    LaunchdSettingsView()
        .padding()
        .frame(width: 500, height: 600)
}
