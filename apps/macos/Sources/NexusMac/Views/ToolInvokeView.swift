import AppKit
import SwiftUI

struct ToolInvokeView: View {
    @EnvironmentObject var model: AppModel
    let node: NodeSummary
    let tool: NodeToolSummary

    @State private var input: String = "{}"
    @State private var approved: Bool = false
    @State private var resultText: String = ""
    @State private var resultArtifacts: [ToolInvocationArtifact] = []
    @State private var resultImages: [NSImage] = []
    @State private var resultDurationMs: Int64?
    @State private var resultErrorDetails: String?

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
                        resultErrorDetails = nil
                        resultDurationMs = nil
                        resultArtifacts = []
                        resultImages = []
                        if let result = await model.invokeTool(edgeID: node.edgeId, toolName: tool.name, input: input, approved: approved) {
                            resultText = result.content
                            resultArtifacts = result.artifacts ?? []
                            resultImages = model.images(from: result)
                            resultDurationMs = result.durationMs
                            resultErrorDetails = result.errorDetails
                        }
                    }
                }
                Spacer()
            }

            if let duration = resultDurationMs {
                Text("Duration: \(duration) ms")
                    .font(.caption)
                    .foregroundColor(.secondary)
            }

            if let details = resultErrorDetails, !details.isEmpty {
                Text(details)
                    .font(.caption)
                    .foregroundColor(.red)
            }

            if !resultText.isEmpty {
                Text("Result")
                    .font(.caption)
                TextEditor(text: $resultText)
                    .font(.system(.body, design: .monospaced))
                    .frame(minHeight: 120)
                    .border(Color.gray.opacity(0.2))
            }

            if !resultImages.isEmpty {
                Text("Images")
                    .font(.caption)
                ScrollView(.horizontal) {
                    HStack(spacing: 12) {
                        ForEach(resultImages.indices, id: \.self) { index in
                            Image(nsImage: resultImages[index])
                                .resizable()
                                .aspectRatio(contentMode: .fit)
                                .frame(width: 220, height: 160)
                                .cornerRadius(6)
                                .overlay(
                                    RoundedRectangle(cornerRadius: 6)
                                        .stroke(Color.gray.opacity(0.2))
                                )
                        }
                    }
                    .padding(.vertical, 4)
                }
            }

            if !resultArtifacts.isEmpty {
                Text("Artifacts")
                    .font(.caption)
                ForEach(resultArtifacts, id: \.id) { artifact in
                    HStack {
                        VStack(alignment: .leading) {
                            Text(artifact.filename ?? artifact.id)
                                .font(.caption)
                            Text("\(artifact.mimeType) - \(artifact.size) bytes")
                                .font(.caption2)
                                .foregroundColor(.secondary)
                        }
                        Spacer()
                    }
                    .padding(6)
                    .background(Color(NSColor.windowBackgroundColor))
                    .cornerRadius(6)
                    .overlay(
                        RoundedRectangle(cornerRadius: 6)
                            .stroke(Color.gray.opacity(0.2))
                    )
                }
            }
        }
    }
}
