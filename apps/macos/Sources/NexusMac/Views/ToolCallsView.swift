import SwiftUI

/// Displays active and recent tool calls
struct ToolCallsView: View {
    @State private var tracker = AgentToolCallTracker.shared

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if !tracker.pendingToolCalls.isEmpty {
                Section {
                    ForEach(tracker.pendingToolCalls) { call in
                        ToolCallRow(call: call)
                    }
                } header: {
                    HStack {
                        Text("Active")
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(.secondary)
                        Spacer()
                        Text("\(tracker.activeCount)")
                            .font(.caption.monospacedDigit())
                            .foregroundStyle(.secondary)
                    }
                }
            }

            if !tracker.recentCompletedCalls.isEmpty {
                Section {
                    ForEach(tracker.recentCompletedCalls) { call in
                        ToolCallRow(call: call)
                    }
                } header: {
                    Text("Recent")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(.secondary)
                }
            }

            if tracker.toolCallsById.isEmpty {
                ContentUnavailableView(
                    "No Tool Calls",
                    systemImage: "hammer",
                    description: Text("Tool calls will appear here when the agent runs")
                )
            }
        }
        .padding()
    }
}

struct ToolCallRow: View {
    let call: TrackedToolCall

    private var metadata: ToolMetadata? {
        ToolMetadata.metadata(for: call.name)
    }

    private var summary: String? {
        metadata?.summaryExtractor(call.arguments)
    }

    var body: some View {
        HStack(spacing: 10) {
            // Icon
            Image(systemName: metadata?.icon ?? "questionmark.circle")
                .font(.system(size: 16))
                .foregroundStyle(iconColor)
                .frame(width: 24)

            // Info
            VStack(alignment: .leading, spacing: 2) {
                Text(metadata?.displayName ?? call.name)
                    .font(.subheadline.weight(.medium))

                if let summary {
                    Text(summary)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }

            Spacer()

            // Status
            statusView
        }
        .padding(.vertical, 6)
        .padding(.horizontal, 8)
        .background(
            RoundedRectangle(cornerRadius: 8)
                .fill(call.isActive ? Color.accentColor.opacity(0.1) : Color.clear)
        )
    }

    private var iconColor: Color {
        switch call.phase {
        case .pending: return .orange
        case .executing: return .blue
        case .completed: return .green
        case .failed: return .red
        }
    }

    @ViewBuilder
    private var statusView: some View {
        switch call.phase {
        case .pending, .executing:
            ProgressView()
                .controlSize(.small)
        case .completed:
            if let duration = call.duration {
                Text(String(format: "%.1fs", duration))
                    .font(.caption.monospacedDigit())
                    .foregroundStyle(.secondary)
            }
        case .failed:
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.red)
        }
    }
}

#Preview {
    ToolCallsView()
        .frame(width: 300, height: 400)
}
