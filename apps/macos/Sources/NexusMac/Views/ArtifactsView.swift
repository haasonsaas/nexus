import SwiftUI

struct ArtifactsView: View {
    @EnvironmentObject var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("Artifacts")
                    .font(.title2)
                Spacer()
                Button("Refresh") {
                    Task { await model.refreshArtifacts() }
                }
            }

            Table(model.artifacts) {
                TableColumn("Type") { Text($0.type) }
                TableColumn("Filename") { Text($0.filename) }
                TableColumn("Size") { Text("\($0.size) bytes") }
                TableColumn("") { art in
                    Button("Open") {
                        model.openArtifact(art)
                    }
                }
            }

            if let error = model.lastError {
                Text(error)
                    .foregroundColor(.red)
            }

            Spacer()
        }
        .padding()
    }
}
