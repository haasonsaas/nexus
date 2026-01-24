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
                    Button("Refresh") {
                        Task { await model.refreshNodes() }
                    }
                }

                List(model.nodes, selection: $selectedNode) { node in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(node.name)
                            .font(.headline)
                        Text("\(node.edgeId) • \(node.status)")
                            .font(.caption)
                            .foregroundColor(.secondary)
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
