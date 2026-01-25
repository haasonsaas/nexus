import SwiftUI

struct NodesView: View {
    @EnvironmentObject var model: AppModel
    @State private var selectedNode: NodeSummary?
    @State private var selectedTool: NodeToolSummary?
    @State private var isLoadingNodes = false
    @State private var isLoadingTools = false

    var body: some View {
        HSplitView {
            nodesListPane
            nodeDetailPane
        }
    }

    // MARK: - Nodes List Pane

    private var nodesListPane: some View {
        VStack(alignment: .leading, spacing: 8) {
            // Header
            HStack {
                Text("Nodes")
                    .font(.title2)
                Spacer()

                StatusBadge(
                    status: model.isWebSocketConnected ? .online : .offline,
                    variant: .animated
                )

                Button {
                    refreshNodes()
                } label: {
                    Image(systemName: "arrow.clockwise")
                }
                .disabled(isLoadingNodes)
            }

            // Content
            Group {
                if isLoadingNodes && model.nodes.isEmpty {
                    LoadingStateView(message: "Loading nodes...", showSkeleton: true)
                } else if model.nodes.isEmpty {
                    EmptyStateView(
                        icon: "desktopcomputer",
                        title: "No Nodes",
                        description: "Connect edge nodes to see them here.",
                        actionTitle: "Refresh"
                    ) {
                        refreshNodes()
                    }
                } else {
                    nodesList
                }
            }
            .animation(.easeInOut(duration: 0.2), value: isLoadingNodes)
            .animation(.easeInOut(duration: 0.2), value: model.nodes.isEmpty)
        }
        .frame(minWidth: 260)
        .padding()
    }

    private var nodesList: some View {
        List(model.nodes, selection: $selectedNode) { node in
            HStack(spacing: 10) {
                NodeStatusIndicator(status: node.status)

                VStack(alignment: .leading, spacing: 4) {
                    Text(node.name)
                        .font(.headline)
                        .lineLimit(1)
                    HStack(spacing: 8) {
                        Text(String(node.edgeId.prefix(8)) + "...")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        Text(node.status)
                            .font(.caption)
                            .foregroundStyle(node.status == "online" ? .green : .orange)
                        if let version = node.version {
                            Text("v\(version)")
                                .font(.caption2)
                                .foregroundStyle(.tertiary)
                        }
                    }
                }
            }
            .padding(.vertical, 2)
        }
        .onChange(of: selectedNode) { _, newNode in
            guard let node = newNode else { return }
            loadTools(for: node)
        }
    }

    // MARK: - Node Detail Pane

    private var nodeDetailPane: some View {
        VStack(alignment: .leading, spacing: 12) {
            if let node = selectedNode {
                // Header
                VStack(alignment: .leading, spacing: 4) {
                    Text(node.name)
                        .font(.title2)
                    HStack(spacing: 8) {
                        Text("Edge ID:")
                            .foregroundStyle(.secondary)
                        Text(node.edgeId)
                            .font(.system(.caption, design: .monospaced))
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                    }
                    .font(.caption)
                }

                Divider()

                // Tools section
                toolsSection(for: node)

                // Tool invoke section
                if let tool = selectedTool {
                    Divider()
                    ToolInvokeView(node: node, tool: tool)
                }
            } else {
                EmptyStateView(
                    icon: "arrow.left.circle",
                    title: "Select a Node",
                    description: "Choose a node from the list to view its details and tools."
                )
            }
            Spacer()
        }
        .padding()
    }

    @ViewBuilder
    private func toolsSection(for node: NodeSummary) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Tools")
                .font(.headline)

            if isLoadingTools {
                LoadingStateView(message: "Loading tools...")
            } else if let tools = model.nodeTools[node.edgeId] {
                if tools.isEmpty {
                    HStack {
                        Image(systemName: "wrench.and.screwdriver")
                            .foregroundStyle(.tertiary)
                        Text("No tools available")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    .padding(.vertical, 8)
                } else {
                    List(tools, selection: $selectedTool) { tool in
                        VStack(alignment: .leading, spacing: 4) {
                            HStack {
                                Image(systemName: "wrench.fill")
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                                Text(tool.name)
                                    .font(.headline)
                            }
                            Text(tool.description)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                                .lineLimit(2)
                        }
                        .padding(.vertical, 2)
                    }
                    .frame(minHeight: 200)
                }
            } else {
                HStack {
                    Image(systemName: "exclamationmark.triangle")
                        .foregroundStyle(.orange)
                    Text("Select a tool to invoke")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .padding(.vertical, 8)
            }
        }
    }

    // MARK: - Actions

    private func refreshNodes() {
        isLoadingNodes = true
        Task {
            await model.refreshNodes()
            isLoadingNodes = false
        }
    }

    private func loadTools(for node: NodeSummary) {
        isLoadingTools = true
        selectedTool = nil
        Task {
            await model.loadTools(for: node)
            isLoadingTools = false
        }
    }
}

// MARK: - Node Status Indicator

struct NodeStatusIndicator: View {
    let status: String

    var body: some View {
        ZStack {
            Circle()
                .fill(statusColor.opacity(0.2))
                .frame(width: 24, height: 24)

            Circle()
                .fill(statusColor)
                .frame(width: 10, height: 10)

            // Pulsing animation for online status
            if status == "online" {
                Circle()
                    .stroke(statusColor, lineWidth: 2)
                    .frame(width: 24, height: 24)
                    .opacity(0.5)
            }
        }
    }

    private var statusColor: Color {
        switch status.lowercased() {
        case "online":
            return .green
        case "offline":
            return .red
        case "connecting":
            return .orange
        default:
            return .gray
        }
    }
}
