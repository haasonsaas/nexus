import SwiftUI

struct NodesView: View {
    @EnvironmentObject var model: AppModel
    @State private var selectedNode: NodeSummary?
    @State private var selectedTool: NodeToolSummary?

    var body: some View {
        HSplitView {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Text("Nodes")
                        .font(.title2)
                    Spacer()

                    // Real-time connection indicator
                    ConnectionStatusBadge(isConnected: model.isWebSocketConnected)

                    Button("Refresh") {
                        Task { await model.refreshNodes() }
                    }
                }

                List(model.nodes, selection: $selectedNode) { node in
                    HStack {
                        // Status indicator
                        NodeStatusIndicator(status: node.status)

                        VStack(alignment: .leading, spacing: 4) {
                            Text(node.name)
                                .font(.headline)
                            HStack(spacing: 8) {
                                Text(String(node.edgeId.prefix(8)) + "...")
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                                Text(node.status)
                                    .font(.caption)
                                    .foregroundColor(node.status == "online" ? .green : .orange)
                                if let version = node.version {
                                    Text("v\(version)")
                                        .font(.caption2)
                                        .foregroundColor(.secondary)
                                }
                            }
                        }
                    }
                }
                .onChange(of: selectedNode) { newNode in
                    guard let node = newNode else { return }
                    Task { await model.loadTools(for: node) }
                }
            }
            .frame(minWidth: 260)
            .padding()

            VStack(alignment: .leading, spacing: 12) {
                if let node = selectedNode {
                    Text(node.name)
                        .font(.title2)
                    Text("Edge ID: \(node.edgeId)")
                        .font(.caption)
                        .foregroundColor(.secondary)

                    if let tools = model.nodeTools[node.edgeId] {
                        Text("Tools")
                            .font(.headline)
                        List(tools, selection: $selectedTool) { tool in
                            VStack(alignment: .leading, spacing: 4) {
                                Text(tool.name)
                                    .font(.headline)
                                Text(tool.description)
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                            }
                        }
                        .frame(minHeight: 200)
                    } else {
                        Text("Loading tools...")
                            .foregroundColor(.secondary)
                    }

                    if let tool = selectedTool {
                        Divider()
                        ToolInvokeView(node: node, tool: tool)
                    } else {
                        Text("Select a tool to invoke")
                            .foregroundColor(.secondary)
                    }
                } else {
                    Text("Select a node to view details")
                        .foregroundColor(.secondary)
                }
                Spacer()
            }
            .padding()
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
