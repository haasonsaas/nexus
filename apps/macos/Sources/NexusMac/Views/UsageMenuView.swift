import SwiftUI

struct UsageMenuView: View {
    @ObservedObject var store: UsageStore

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("Usage")
                .font(.caption)
                .foregroundColor(.secondary)

            if let error = store.error {
                Text(error)
                    .font(.caption)
                    .foregroundColor(.red)
                    .lineLimit(2)
            } else if store.rows.isEmpty {
                if store.isLoading {
                    Text("Loading...")
                        .font(.caption)
                        .foregroundColor(.secondary)
                } else {
                    Text("No usage data")
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
            } else {
                ForEach(store.rows) { row in
                    UsageRowView(row: row)
                }
            }
        }
    }
}

struct UsageRowView: View {
    let row: UsageRow

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 4) {
                Text(row.titleText)
                    .font(.caption)
                    .lineLimit(1)
                Spacer()
                Text(row.detailText())
                    .font(.caption)
                    .foregroundColor(.secondary)
            }

            if let pct = row.usedPercent {
                UsageProgressBar(usedPercent: pct)
                    .frame(height: 4)
            }
        }
        .padding(.vertical, 2)
    }
}

struct UsageProgressBar: View {
    let usedPercent: Double

    private var fraction: Double {
        min(1, max(0, usedPercent / 100))
    }

    private var tint: Color {
        if usedPercent >= 95 { return .red }
        if usedPercent >= 80 { return .orange }
        if usedPercent >= 60 { return .yellow }
        return .green
    }

    var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                RoundedRectangle(cornerRadius: 2)
                    .fill(Color.gray.opacity(0.3))

                RoundedRectangle(cornerRadius: 2)
                    .fill(tint)
                    .frame(width: max(1, geo.size.width * fraction))
            }
        }
    }
}
