import SwiftUI

struct SkillsView: View {
    @EnvironmentObject var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("Skills")
                    .font(.title2)
                Spacer()
                Button("Refresh") {
                    Task { await model.refreshSkills() }
                }
                Button("Discover") {
                    Task { await model.triggerSkillsRefresh() }
                }
            }

            Table(model.skills) {
                TableColumn("Name") { Text($0.name) }
                TableColumn("Eligible") { Text($0.eligible ? "Yes" : "No") }
                TableColumn("Source") { Text($0.source) }
                TableColumn("Execution") { Text($0.execution ?? "-") }
                TableColumn("Reason") { Text($0.reason ?? "") }
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
