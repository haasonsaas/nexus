import SwiftUI

struct LogsView: View {
    @EnvironmentObject var model: AppModel
    @State private var isLoading = false
    @State private var searchText = ""
    @State private var selectedLogLevel: LogLevel = .all
    @State private var autoScroll = true

    enum LogLevel: String, CaseIterable {
        case all = "All"
        case error = "Error"
        case warning = "Warning"
        case info = "Info"
        case debug = "Debug"
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            // Header
            headerView

            // Toolbar
            toolbarView

            // Content
            Group {
                if isLoading && model.logText.isEmpty {
                    LoadingStateView(message: "Loading logs...", showSkeleton: true)
                } else if model.logText.isEmpty {
                    EmptyStateView(
                        icon: "doc.text.magnifyingglass",
                        title: "No Logs",
                        description: "Edge service logs will appear here when available.",
                        actionTitle: "Load Logs"
                    ) {
                        loadLogs()
                    }
                } else {
                    logContent
                }
            }
            .animation(.easeInOut(duration: 0.2), value: isLoading)
            .animation(.easeInOut(duration: 0.2), value: model.logText.isEmpty)

            Spacer()
        }
        .padding()
    }

    // MARK: - Header

    private var headerView: some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                Text("Edge Logs")
                    .font(.title2)
                Text("View edge service log output")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            // Log stats
            if !model.logText.isEmpty {
                let lineCount = model.logText.components(separatedBy: "\n").count
                Text("\(lineCount) lines")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(
                        Capsule()
                            .fill(Color.secondary.opacity(0.1))
                    )
            }
        }
    }

    // MARK: - Toolbar

    private var toolbarView: some View {
        HStack(spacing: 12) {
            // Search
            HStack {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(.tertiary)
                TextField("Search logs...", text: $searchText)
                    .textFieldStyle(.plain)
                if !searchText.isEmpty {
                    Button {
                        searchText = ""
                    } label: {
                        Image(systemName: "xmark.circle.fill")
                            .foregroundStyle(.tertiary)
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(8)
            .background(
                RoundedRectangle(cornerRadius: 8, style: .continuous)
                    .fill(Color(NSColor.controlBackgroundColor))
            )
            .frame(maxWidth: 240)

            // Level filter
            Picker("Level", selection: $selectedLogLevel) {
                ForEach(LogLevel.allCases, id: \.self) { level in
                    Text(level.rawValue).tag(level)
                }
            }
            .pickerStyle(.segmented)
            .frame(width: 200)

            Spacer()

            // Auto-scroll toggle
            Toggle(isOn: $autoScroll) {
                Label("Auto-scroll", systemImage: "arrow.down.circle")
            }
            .toggleStyle(.button)
            .controlSize(.small)

            // Actions
            Button {
                loadLogs()
            } label: {
                Label("Reload", systemImage: "arrow.clockwise")
            }
            .buttonStyle(.bordered)
            .controlSize(.small)
            .disabled(isLoading)

            Button {
                model.openLogs()
            } label: {
                Label("Open in Finder", systemImage: "folder")
            }
            .buttonStyle(.bordered)
            .controlSize(.small)
        }
    }

    // MARK: - Log Content

    private var logContent: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 2) {
                    let lines = filteredLogLines
                    ForEach(Array(lines.enumerated()), id: \.offset) { index, line in
                        LogLineView(line: line, searchText: searchText)
                            .id(index)
                    }
                }
                .padding(12)
            }
            .background(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .fill(Color(NSColor.textBackgroundColor))
            )
            .overlay(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .stroke(Color.gray.opacity(0.2), lineWidth: 1)
            )
            .onChange(of: model.logText) { _, _ in
                if autoScroll {
                    let lines = filteredLogLines
                    if !lines.isEmpty {
                        withAnimation {
                            proxy.scrollTo(lines.count - 1, anchor: .bottom)
                        }
                    }
                }
            }
        }
    }

    private var filteredLogLines: [String] {
        let lines = model.logText.components(separatedBy: "\n")

        return lines.filter { line in
            // Filter by search text
            let matchesSearch = searchText.isEmpty || line.localizedCaseInsensitiveContains(searchText)

            // Filter by log level
            let matchesLevel: Bool
            switch selectedLogLevel {
            case .all:
                matchesLevel = true
            case .error:
                matchesLevel = line.contains("ERROR") || line.contains("error") || line.contains("FATAL")
            case .warning:
                matchesLevel = line.contains("WARN") || line.contains("warning")
            case .info:
                matchesLevel = line.contains("INFO") || line.contains("info")
            case .debug:
                matchesLevel = line.contains("DEBUG") || line.contains("debug") || line.contains("TRACE")
            }

            return matchesSearch && matchesLevel
        }
    }

    // MARK: - Actions

    private func loadLogs() {
        isLoading = true
        model.loadLogs()
        // Simulate async load
        Task {
            try? await Task.sleep(for: .milliseconds(300))
            isLoading = false
        }
    }
}

// MARK: - Log Line View

struct LogLineView: View {
    let line: String
    let searchText: String

    @State private var isHovered = false

    private var logLevel: LogsView.LogLevel {
        if line.contains("ERROR") || line.contains("error") || line.contains("FATAL") {
            return .error
        }
        if line.contains("WARN") || line.contains("warning") {
            return .warning
        }
        if line.contains("DEBUG") || line.contains("debug") || line.contains("TRACE") {
            return .debug
        }
        return .info
    }

    private var levelColor: Color {
        switch logLevel {
        case .error: return .red
        case .warning: return .orange
        case .debug: return .secondary
        case .info, .all: return .primary
        }
    }

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            // Level indicator
            Circle()
                .fill(levelColor)
                .frame(width: 6, height: 6)
                .padding(.top, 5)

            // Log text
            Text(highlightedLine)
                .font(.system(.caption, design: .monospaced))
                .foregroundStyle(levelColor == .primary ? .primary : levelColor)
                .textSelection(.enabled)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(
            RoundedRectangle(cornerRadius: 4, style: .continuous)
                .fill(isHovered ? Color(NSColor.selectedTextBackgroundColor).opacity(0.3) : Color.clear)
        )
        .onHover { hovering in
            isHovered = hovering
        }
    }

    private var highlightedLine: AttributedString {
        var attributed = AttributedString(line)

        if !searchText.isEmpty, let range = attributed.range(of: searchText, options: .caseInsensitive) {
            attributed[range].backgroundColor = .yellow.opacity(0.3)
            attributed[range].foregroundColor = .primary
        }

        return attributed
    }
}
