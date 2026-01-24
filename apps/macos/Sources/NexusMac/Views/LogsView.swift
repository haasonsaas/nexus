import SwiftUI

struct LogsView: View {
    @EnvironmentObject var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("Edge Logs")
                    .font(.title2)
                Spacer()
                Button("Reload") { model.loadLogs() }
                Button("Open in Finder") { model.openLogs() }
            }

            TextEditor(text: $model.logText)
                .font(.system(.body, design: .monospaced))
                .frame(minHeight: 400)
                .border(Color.gray.opacity(0.2))

            Spacer()
        }
        .padding()
    }
}
