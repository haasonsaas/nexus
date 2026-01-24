import SwiftUI

struct ConfigView: View {
    @EnvironmentObject var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("Edge Config")
                    .font(.title2)
                Spacer()
                Button("Reload") { model.loadConfig() }
                Button("Save") { model.saveConfig() }
            }

            Text(model.configPath)
                .font(.caption)
                .foregroundColor(.secondary)

            TextEditor(text: $model.configText)
                .font(.system(.body, design: .monospaced))
                .frame(minHeight: 400)
                .border(Color.gray.opacity(0.2))

            if let error = model.lastError {
                Text(error)
                    .foregroundColor(.red)
            }

            Spacer()
        }
        .padding()
    }
}
