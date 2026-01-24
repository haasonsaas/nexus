import SwiftUI

struct OverviewView: View {
    @EnvironmentObject var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                Text("System Status")
                    .font(.title2)
                Spacer()
                Button("Refresh") {
                    Task { await model.refreshStatus() }
                }
            }

            if let status = model.status {
                HStack(spacing: 24) {
                    StatCard(title: "Uptime", value: status.uptimeString)
                    StatCard(title: "Sessions", value: "\(status.sessionCount)")
                    StatCard(title: "Go", value: status.goVersion)
                    StatCard(title: "Memory", value: String(format: "%.0f MB", status.memAllocMb))
                    StatCard(title: "DB", value: status.databaseStatus)
                }
                .padding(.bottom, 8)

                Text("Channels")
                    .font(.headline)

                Table(status.channels) {
                    TableColumn("Name") { Text($0.name) }
                    TableColumn("Status") { Text($0.status) }
                    TableColumn("Enabled") { Text($0.enabled ? "Yes" : "No") }
                    TableColumn("Healthy") { Text(($0.healthy ?? false) ? "Yes" : "No") }
                }
            } else {
                Text("No status loaded. Configure base URL and API key in Settings.")
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

struct StatCard: View {
    let title: String
    let value: String

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title)
                .font(.caption)
                .foregroundColor(.secondary)
            Text(value)
                .font(.headline)
        }
        .padding(10)
        .background(Color(NSColor.windowBackgroundColor))
        .cornerRadius(8)
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(Color.gray.opacity(0.2))
        )
    }
}
