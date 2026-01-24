import SwiftUI

struct CronView: View {
    @EnvironmentObject var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("Cron")
                    .font(.title2)
                Spacer()
                Text(model.cronEnabled ? "Enabled" : "Disabled")
                    .font(.caption)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(model.cronEnabled ? Color.green.opacity(0.2) : Color.red.opacity(0.2))
                    .cornerRadius(6)
                Button("Refresh") {
                    Task { await model.refreshCron() }
                }
            }

            Table(model.cronJobs) {
                TableColumn("Name") { Text($0.name) }
                TableColumn("Type") { Text($0.type) }
                TableColumn("Enabled") { Text($0.enabled ? "Yes" : "No") }
                TableColumn("Schedule") { Text($0.schedule) }
                TableColumn("Next Run") { Text($0.nextRun.formatted()) }
                TableColumn("Last Run") { Text($0.lastRun.formatted()) }
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
