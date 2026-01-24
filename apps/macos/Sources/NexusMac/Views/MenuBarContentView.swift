import SwiftUI

struct MenuBarContentView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Nexus Edge")
                .font(.headline)
            Text("Status: \(model.edgeServiceStatus.rawValue)")
                .font(.caption)

            if let error = model.lastError {
                Text(error)
                    .font(.caption)
                    .foregroundColor(.red)
            }

            Divider()

            Button("Open Nexus") {
                openWindow(id: "main")
            }
            Button("Refresh") {
                model.refreshEdgeServiceStatus()
                Task { await model.refreshAll() }
            }
            Button("Start Edge") { model.startService() }
            Button("Stop Edge") { model.stopService() }

            Divider()

            Button("Quit") { NSApplication.shared.terminate(nil) }
        }
        .padding(8)
        .frame(minWidth: 240)
    }
}
