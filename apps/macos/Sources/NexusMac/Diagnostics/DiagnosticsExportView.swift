import SwiftUI
import AppKit

/// Diagnostics export view for managing and exporting log files.
struct DiagnosticsExportView: View {
    @State private var logger = DiagnosticsFileLogger.shared
    @State private var healthStore = HealthStore.shared

    @State private var showLogViewer = false
    @State private var showClearConfirmation = false
    @State private var showExportSuccess = false
    @State private var exportedURL: URL?
    @State private var recentLogs = ""
    @State private var systemInfo: DiagnosticSystemInfo?
    @State private var isExporting = false
    @State private var copyFeedback = false

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            headerSection
            Divider()
            logFileSection
            Divider()
            systemInfoSection
            Divider()
            actionsSection
            Spacer(minLength: 0)
        }
        .padding()
        .task {
            await refreshInfo()
        }
        .sheet(isPresented: $showLogViewer) {
            DiagnosticsLogViewerSheet(logs: recentLogs)
        }
        .alert("Clear Logs", isPresented: $showClearConfirmation) {
            Button("Cancel", role: .cancel) {}
            Button("Clear", role: .destructive) {
                clearLogs()
            }
        } message: {
            Text("Are you sure you want to delete all diagnostic logs? This action cannot be undone.")
        }
        .alert("Export Complete", isPresented: $showExportSuccess) {
            Button("Show in Finder") {
                if let url = exportedURL {
                    NSWorkspace.shared.selectFile(url.path, inFileViewerRootedAtPath: "")
                }
            }
            Button("OK", role: .cancel) {}
        } message: {
            Text("Diagnostic logs have been exported successfully.")
        }
    }

    // MARK: - Header Section

    private var headerSection: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("Diagnostics")
                .font(.headline)
            Text("View, export, and manage diagnostic logs for troubleshooting.")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
    }

    // MARK: - Log File Section

    private var logFileSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Log File")
                .font(.subheadline)
                .fontWeight(.medium)

            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Text("Location:")
                        .foregroundStyle(.secondary)
                    Text(logger.logFileLocation)
                        .font(.system(.body, design: .monospaced))
                        .lineLimit(1)
                        .truncationMode(.middle)

                    Spacer()

                    Button {
                        openLogFolder()
                    } label: {
                        Image(systemName: "folder")
                    }
                    .buttonStyle(.borderless)
                    .help("Open in Finder")
                }

                HStack {
                    Text("Size:")
                        .foregroundStyle(.secondary)
                    Text(systemInfo?.logFileSizeFormatted ?? "0 bytes")
                        .fontWeight(.medium)

                    Spacer()

                    Button {
                        Task { await refreshInfo() }
                    } label: {
                        Image(systemName: "arrow.clockwise")
                    }
                    .buttonStyle(.borderless)
                    .help("Refresh")
                }
            }
            .padding(12)
            .background(Color(NSColor.controlBackgroundColor))
            .cornerRadius(8)
        }
    }

    // MARK: - System Info Section

    private var systemInfoSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("System Information")
                .font(.subheadline)
                .fontWeight(.medium)

            LazyVGrid(columns: [
                GridItem(.flexible(), alignment: .leading),
                GridItem(.flexible(), alignment: .leading)
            ], spacing: 8) {
                InfoRow(label: "App Version", value: systemInfo?.appVersion ?? "Loading...")
                InfoRow(label: "macOS Version", value: systemInfo?.macOSVersion ?? "Loading...")
                InfoRow(label: "Instance ID", value: truncatedInstanceId)
                InfoRow(label: "Gateway Status", value: gatewayStatusText, valueColor: gatewayStatusColor)
                InfoRow(label: "Memory Usage", value: systemInfo?.memoryUsageFormatted ?? "Loading...")
                InfoRow(label: "Log Size", value: systemInfo?.logFileSizeFormatted ?? "0 bytes")
            }
            .padding(12)
            .background(Color(NSColor.controlBackgroundColor))
            .cornerRadius(8)
        }
    }

    private var truncatedInstanceId: String {
        guard let id = systemInfo?.instanceId else { return "Loading..." }
        if id.count > 12 {
            return String(id.prefix(8)) + "..."
        }
        return id
    }

    private var gatewayStatusText: String {
        systemInfo?.gatewayStatus ?? "Unknown"
    }

    private var gatewayStatusColor: Color {
        guard let status = systemInfo?.gatewayStatus else { return .secondary }
        if status == "Connected" {
            return .green
        } else if status.hasPrefix("Degraded") {
            return .orange
        } else if status == "Linking Needed" {
            return .red
        }
        return .secondary
    }

    // MARK: - Actions Section

    private var actionsSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Actions")
                .font(.subheadline)
                .fontWeight(.medium)

            HStack(spacing: 12) {
                Button {
                    Task { await viewRecentLogs() }
                } label: {
                    Label("View Logs", systemImage: "doc.text.magnifyingglass")
                }
                .buttonStyle(.bordered)

                Button {
                    Task { await exportLogs() }
                } label: {
                    Label(isExporting ? "Exporting..." : "Export Logs", systemImage: "square.and.arrow.up")
                }
                .buttonStyle(.bordered)
                .disabled(isExporting)

                Button(role: .destructive) {
                    showClearConfirmation = true
                } label: {
                    Label("Clear Logs", systemImage: "trash")
                }
                .buttonStyle(.bordered)

                Spacer()

                Button {
                    copyDiagnosticReport()
                } label: {
                    Label(copyFeedback ? "Copied!" : "Copy Report", systemImage: copyFeedback ? "checkmark" : "doc.on.doc")
                }
                .buttonStyle(.borderedProminent)
                .animation(.easeInOut, value: copyFeedback)
            }
        }
    }

    // MARK: - Actions

    private func refreshInfo() async {
        logger.refreshFileStats()
        systemInfo = logger.gatherSystemInfo()
    }

    private func viewRecentLogs() async {
        recentLogs = logger.tail(lines: 500)
        showLogViewer = true
    }

    private func exportLogs() async {
        isExporting = true
        defer { isExporting = false }

        if let url = logger.exportLogs() {
            exportedURL = url
            showExportSuccess = true
        }
    }

    private func clearLogs() {
        logger.clearLogs()
        Task { await refreshInfo() }
    }

    private func openLogFolder() {
        let url = URL(fileURLWithPath: logger.logFileLocation).deletingLastPathComponent()
        NSWorkspace.shared.selectFile(nil, inFileViewerRootedAtPath: url.path)
    }

    private func copyDiagnosticReport() {
        let report = logger.generateDiagnosticReport()
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(report, forType: .string)

        copyFeedback = true
        DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
            copyFeedback = false
        }
    }
}

// MARK: - InfoRow

private struct InfoRow: View {
    let label: String
    let value: String
    var valueColor: Color = .primary

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(value)
                .font(.callout)
                .foregroundStyle(valueColor)
                .lineLimit(1)
        }
    }
}

// MARK: - LogViewerSheet

private struct DiagnosticsLogViewerSheet: View {
    let logs: String
    @Environment(\.dismiss) private var dismiss
    @State private var searchText = ""

    var body: some View {
        VStack(spacing: 0) {
            // Header
            HStack {
                Text("Recent Logs")
                    .font(.headline)
                Spacer()
                Button("Done") {
                    dismiss()
                }
                .keyboardShortcut(.escape)
            }
            .padding()

            Divider()

            // Search
            HStack {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(.secondary)
                TextField("Search logs...", text: $searchText)
                    .textFieldStyle(.plain)
                if !searchText.isEmpty {
                    Button {
                        searchText = ""
                    } label: {
                        Image(systemName: "xmark.circle.fill")
                            .foregroundStyle(.secondary)
                    }
                    .buttonStyle(.borderless)
                }
            }
            .padding(8)
            .background(Color(NSColor.controlBackgroundColor))
            .cornerRadius(6)
            .padding(.horizontal)
            .padding(.vertical, 8)

            Divider()

            // Log content
            ScrollView {
                ScrollViewReader { proxy in
                    LazyVStack(alignment: .leading, spacing: 0) {
                        ForEach(Array(filteredLines.enumerated()), id: \.offset) { index, line in
                            DiagnosticsLogLineView(line: line, searchText: searchText)
                                .id(index)
                        }
                    }
                    .padding()
                    .onAppear {
                        // Scroll to bottom
                        if let lastIndex = filteredLines.indices.last {
                            proxy.scrollTo(lastIndex, anchor: .bottom)
                        }
                    }
                }
            }
            .background(Color(NSColor.textBackgroundColor))

            Divider()

            // Footer
            HStack {
                Text("\(filteredLines.count) lines")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
                Button {
                    copyLogs()
                } label: {
                    Label("Copy All", systemImage: "doc.on.doc")
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
            }
            .padding()
        }
        .frame(width: 800, height: 600)
    }

    private var filteredLines: [String] {
        let lines = logs.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        if searchText.isEmpty {
            return lines
        }
        return lines.filter { $0.localizedCaseInsensitiveContains(searchText) }
    }

    private func copyLogs() {
        let content = filteredLines.joined(separator: "\n")
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(content, forType: .string)
    }
}

// MARK: - LogLineView

private struct DiagnosticsLogLineView: View {
    let line: String
    let searchText: String

    var body: some View {
        HStack(alignment: .top, spacing: 0) {
            Text(line)
                .font(.system(.caption, design: .monospaced))
                .foregroundStyle(lineColor)
                .textSelection(.enabled)
            Spacer(minLength: 0)
        }
        .padding(.vertical, 1)
        .background(backgroundColor)
    }

    private var lineColor: Color {
        if line.contains("[ERROR]") {
            return .red
        } else if line.contains("[WARNING]") {
            return .orange
        } else if line.contains("[DEBUG]") {
            return .secondary
        }
        return .primary
    }

    private var backgroundColor: Color {
        if !searchText.isEmpty && line.localizedCaseInsensitiveContains(searchText) {
            return .yellow.opacity(0.2)
        }
        return .clear
    }
}

// MARK: - Preview

#Preview {
    DiagnosticsExportView()
        .frame(width: 600, height: 500)
}
