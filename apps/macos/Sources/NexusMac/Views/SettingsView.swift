import SwiftUI

struct SettingsView: View {
    @EnvironmentObject var model: AppModel
    @State private var showSaved = false

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Settings")
                .font(.title2)

            VStack(alignment: .leading, spacing: 8) {
                Text("Gateway Base URL")
                    .font(.headline)
                TextField("http://localhost:8080", text: $model.baseURL)
                    .textFieldStyle(.roundedBorder)
            }

            VStack(alignment: .leading, spacing: 8) {
                Text("API Key")
                    .font(.headline)
                SecureField("X-API-Key", text: $model.apiKey)
                    .textFieldStyle(.roundedBorder)
                Text("Stored in Keychain")
                    .font(.caption)
                    .foregroundColor(.secondary)
            }

            HStack(spacing: 12) {
                Button("Save") {
                    model.saveSettings()
                    showSaved = true
                }
                Button("Test Connection") {
                    Task { await model.refreshStatus() }
                }
            }

            if showSaved {
                Text("Saved")
                    .foregroundColor(.secondary)
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
