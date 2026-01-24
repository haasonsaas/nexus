import SwiftUI

struct EdgeServiceView: View {
    @EnvironmentObject var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Edge Service")
                .font(.title2)

            HStack(spacing: 16) {
                Label("Status: \(model.edgeServiceStatus.rawValue)", systemImage: "bolt.horizontal")
                Button("Refresh") {
                    model.refreshEdgeServiceStatus()
                }
            }

            HStack(spacing: 12) {
                Button("Install / Update") { model.installService() }
                Button("Start") { model.startService() }
                Button("Stop") { model.stopService() }
                Button("Uninstall") { model.uninstallService() }
            }

            Divider()

            Text("Config")
                .font(.headline)
            Text(model.configPath)
                .font(.callout)
                .foregroundColor(.secondary)

            HStack(spacing: 12) {
                Button("Open Config Folder") { model.openConfigFolder() }
                Button("Open Logs") { model.openLogs() }
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
