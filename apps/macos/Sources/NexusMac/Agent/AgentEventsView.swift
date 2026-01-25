import OSLog
import SwiftUI

// MARK: - Agent Events View

struct AgentEventsView: View {
    @State private var store = AgentEventStore.shared
    @State private var selectedSessionId: String?
    @State private var selectedStreamType: AgentEventStreamType = .all
    @State private var searchText = ""
    @State private var autoScroll = true
    @State private var expandedEventIds: Set<UUID> = []
    @State private var showClearConfirmation = false
    @State private var startDate: Date?
    @State private var endDate: Date?

    private let logger = Logger(subsystem: "com.nexus.mac", category: "agent-events-view")

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            headerView
            filterToolbar
            contentView
        }
        .padding()
        .confirmationDialog(
            "Clear Events",
            isPresented: $showClearConfirmation,
            titleVisibility: .visible
        ) {
            if let sessionId = selectedSessionId {
                Button("Clear Session Events", role: .destructive) {
                    store.clear(sessionId: sessionId)
                }
            }
            Button("Clear All Events", role: .destructive) {
                store.clearAll()
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            if selectedSessionId != nil {
                Text("This will permanently delete events for this session or all events.")
            } else {
                Text("This will permanently delete all stored agent events.")
            }
        }
    }

    // MARK: - Header

    private var headerView: some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                Text("Agent Events")
                    .font(.title2)
                Text("\(store.events.count) events across \(store.sessionSummaries.count) sessions")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            // Event stats badge
            if !store.events.isEmpty {
                let errorCount = store.events.filter { $0.stream == "error" }.count
                if errorCount > 0 {
                    HStack(spacing: 4) {
                        Image(systemName: "exclamationmark.triangle.fill")
                            .foregroundStyle(.orange)
                        Text("\(errorCount) errors")
                            .font(.caption)
                    }
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(
                        Capsule()
                            .fill(Color.orange.opacity(0.1))
                    )
                }
            }
        }
    }

    // MARK: - Filter Toolbar

    private var filterToolbar: some View {
        HStack(spacing: 12) {
            // Session picker
            Picker("Session", selection: $selectedSessionId) {
                Text("All Sessions").tag(String?.none)
                Divider()
                ForEach(store.sessionSummaries) { summary in
                    HStack {
                        Text(truncatedSessionId(summary.id))
                        Text("(\(summary.eventCount))")
                            .foregroundStyle(.secondary)
                    }
                    .tag(Optional(summary.id))
                }
            }
            .frame(width: 180)

            // Stream type filter
            Picker("Type", selection: $selectedStreamType) {
                ForEach(AgentEventStreamType.allCases, id: \.self) { type in
                    Label(type.displayName, systemImage: type.systemImage)
                        .tag(type)
                }
            }
            .frame(width: 140)

            // Search field
            HStack {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(.tertiary)
                TextField("Search events...", text: $searchText)
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
            .frame(maxWidth: 200)

            Spacer()

            // Auto-scroll toggle
            Toggle(isOn: $autoScroll) {
                Label("Auto-scroll", systemImage: "arrow.down.circle")
            }
            .toggleStyle(.button)
            .controlSize(.small)

            // Clear button
            Button {
                showClearConfirmation = true
            } label: {
                Label("Clear", systemImage: "trash")
            }
            .buttonStyle(.bordered)
            .controlSize(.small)
            .disabled(store.events.isEmpty)
        }
    }

    // MARK: - Content View

    private var contentView: some View {
        Group {
            if store.isLoading {
                LoadingStateView(message: "Loading events...", showSkeleton: true)
            } else if store.events.isEmpty {
                EmptyStateView(
                    icon: "bolt.horizontal.circle",
                    title: "No Agent Events",
                    description: "Agent events will appear here when agents run tasks."
                )
            } else if filteredEvents.isEmpty {
                EmptyStateView(
                    icon: "magnifyingglass",
                    title: "No Matching Events",
                    description: "Try adjusting your filters or search query."
                )
            } else {
                eventsList
            }
        }
        .animation(.easeInOut(duration: 0.2), value: store.isLoading)
        .animation(.easeInOut(duration: 0.2), value: store.events.isEmpty)
    }

    private var eventsList: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 4) {
                    ForEach(filteredEvents) { event in
                        AgentEventRowView(
                            event: event,
                            isExpanded: expandedEventIds.contains(event.id),
                            searchText: searchText
                        ) {
                            withAnimation(.easeInOut(duration: 0.15)) {
                                if expandedEventIds.contains(event.id) {
                                    expandedEventIds.remove(event.id)
                                } else {
                                    expandedEventIds.insert(event.id)
                                }
                            }
                        }
                        .id(event.id)
                    }
                }
                .padding(8)
            }
            .background(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .fill(Color(NSColor.textBackgroundColor))
            )
            .overlay(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .stroke(Color.gray.opacity(0.2), lineWidth: 1)
            )
            .onChange(of: store.events.count) { _, _ in
                if autoScroll, let lastEvent = filteredEvents.last {
                    withAnimation {
                        proxy.scrollTo(lastEvent.id, anchor: .bottom)
                    }
                }
            }
        }
    }

    // MARK: - Helpers

    private var filteredEvents: [StoredAgentEvent] {
        store.filteredEvents(
            sessionId: selectedSessionId,
            streamType: selectedStreamType,
            startDate: startDate,
            endDate: endDate,
            searchText: searchText.isEmpty ? nil : searchText
        )
    }

    private func truncatedSessionId(_ id: String) -> String {
        if id.count > 12 {
            return String(id.prefix(8)) + "..."
        }
        return id
    }
}

// MARK: - Event Row View

struct AgentEventRowView: View {
    let event: StoredAgentEvent
    let isExpanded: Bool
    let searchText: String
    let onToggle: () -> Void

    @State private var isHovered = false

    private var streamType: AgentEventStreamType {
        AgentEventStreamType(rawValue: event.stream) ?? .status
    }

    private var streamColor: Color {
        switch event.stream {
        case "error": return .red
        case "tool_use": return .blue
        case "tool_result": return .green
        case "thinking": return .purple
        case "output": return .primary
        default: return .secondary
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header row
            HStack(spacing: 10) {
                // Type icon
                Image(systemName: streamType.systemImage)
                    .foregroundStyle(streamColor)
                    .frame(width: 20)

                // Stream type badge
                Text(event.stream)
                    .font(.caption.weight(.medium))
                    .foregroundStyle(streamColor)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 2)
                    .background(
                        Capsule()
                            .fill(streamColor.opacity(0.1))
                    )

                // Summary/preview
                Text(eventPreview)
                    .font(.system(.caption, design: .monospaced))
                    .foregroundStyle(.primary)
                    .lineLimit(1)

                Spacer()

                // Sequence number
                Text("#\(event.seq)")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)

                // Timestamp
                Text(event.timestamp, style: .time)
                    .font(.caption2)
                    .foregroundStyle(.tertiary)

                // Expand indicator
                Image(systemName: isExpanded ? "chevron.down" : "chevron.right")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 8)
            .contentShape(Rectangle())
            .onTapGesture(perform: onToggle)

            // Expanded details
            if isExpanded {
                expandedContent
            }
        }
        .background(
            RoundedRectangle(cornerRadius: 6, style: .continuous)
                .fill(isHovered ? Color(NSColor.selectedTextBackgroundColor).opacity(0.2) : Color.clear)
        )
        .onHover { hovering in
            isHovered = hovering
        }
    }

    private var eventPreview: String {
        if let summary = event.summary, !summary.isEmpty {
            return summary
        }
        // Try to extract a meaningful preview from data
        if let text = event.data["text"]?.value as? String {
            return String(text.prefix(60))
        }
        if let content = event.data["content"]?.value as? String {
            return String(content.prefix(60))
        }
        if let name = event.data["name"]?.value as? String {
            return name
        }
        return "Event data available"
    }

    private var expandedContent: some View {
        VStack(alignment: .leading, spacing: 8) {
            Divider()
                .padding(.horizontal, 10)

            // Metadata
            HStack(spacing: 16) {
                LabeledContent("Run ID") {
                    Text(event.runId)
                        .font(.caption.monospaced())
                        .textSelection(.enabled)
                }
                LabeledContent("Received") {
                    Text(event.receivedAt, style: .relative)
                        .font(.caption)
                }
            }
            .font(.caption)
            .foregroundStyle(.secondary)
            .padding(.horizontal, 10)

            // Data content
            if !event.data.isEmpty {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Data")
                        .font(.caption.weight(.medium))
                        .foregroundStyle(.secondary)

                    ScrollView(.horizontal, showsIndicators: false) {
                        Text(formatData(event.data))
                            .font(.system(.caption2, design: .monospaced))
                            .textSelection(.enabled)
                            .padding(8)
                            .background(
                                RoundedRectangle(cornerRadius: 4, style: .continuous)
                                    .fill(Color(NSColor.controlBackgroundColor))
                            )
                    }
                }
                .padding(.horizontal, 10)
            }

            Spacer()
                .frame(height: 4)
        }
        .padding(.bottom, 8)
        .transition(.opacity.combined(with: .move(edge: .top)))
    }

    private func formatData(_ data: [String: AnyCodable]) -> String {
        do {
            let rawDict = data.mapValues { $0.value }
            let jsonData = try JSONSerialization.data(withJSONObject: rawDict, options: [.prettyPrinted, .sortedKeys])
            return String(data: jsonData, encoding: .utf8) ?? "{}"
        } catch {
            return String(describing: data)
        }
    }
}

// MARK: - Preview

#if DEBUG
#Preview {
    AgentEventsView()
        .frame(width: 800, height: 600)
}
#endif
