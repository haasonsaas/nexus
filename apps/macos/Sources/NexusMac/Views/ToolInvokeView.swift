import SwiftUI

struct ToolInvokeView: View {
    @EnvironmentObject var model: AppModel
    let node: NodeSummary
    let tool: NodeToolSummary

    @State private var input: String = "{}"
    @State private var approved: Bool = false
    @State private var resultText: String = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Invoke \(tool.name)")
                .font(.headline)

            Text("Input (JSON)")
                .font(.caption)
            TextEditor(text: $input)
                .font(.system(.body, design: .monospaced))
                .frame(minHeight: 120)
                .border(Color.gray.opacity(0.2))

            Toggle("Approved", isOn: $approved)
                .toggleStyle(.switch)

            HStack {
                Button("Run") {
                    Task {
                        if let result = await model.invokeTool(edgeID: node.edgeId, toolName: tool.name, input: input, approved: approved) {
                            resultText = result.content
                        }
                    }
                }
                Spacer()
            }

            if !resultText.isEmpty {
                Text("Result")
                    .font(.caption)
                TextEditor(text: $resultText)
                    .font(.system(.body, design: .monospaced))
                    .frame(minHeight: 120)
                    .border(Color.gray.opacity(0.2))
            }
        }
    }
}
