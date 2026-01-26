import Charts
import SwiftUI

/// A compact menu bar view showing cost/usage summary with a small chart.
struct CostUsageMenuView: View {
    @ObservedObject var store: CostUsageStore
    @State private var isHovered = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            // Header
            HStack {
                Text("Costs")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)

                if store.isDemoData {
                    Text("Sample")
                        .font(.caption2.weight(.medium))
                        .foregroundStyle(.secondary)
                        .padding(.horizontal, 5)
                        .padding(.vertical, 1)
                        .background(
                            Capsule()
                                .fill(Color.secondary.opacity(0.12))
                        )
                }

                Spacer()

                if store.isLoading {
                    ProgressView()
                        .controlSize(.mini)
                }
            }

            // Content
            if let error = store.error {
                CostMenuErrorView(message: error)
            } else if store.entries.isEmpty {
                if store.isLoading {
                    CostMenuSkeletonView()
                } else {
                    CostMenuEmptyView()
                }
            } else {
                costContent
            }
        }
        .animation(.easeInOut(duration: 0.2), value: store.isLoading)
        .animation(.easeInOut(duration: 0.2), value: store.entries.count)
    }

    // MARK: - Cost Content

    private var costContent: some View {
        VStack(spacing: 8) {
            // Summary row
            HStack(spacing: 16) {
                // Today's cost
                VStack(alignment: .leading, spacing: 2) {
                    Text("Today")
                        .font(.system(size: 10))
                        .foregroundStyle(.tertiary)
                    Text(store.formattedTodayCost)
                        .font(.system(size: 14, weight: .semibold, design: .rounded))
                        .foregroundStyle(store.todayCost != nil ? .primary : .tertiary)
                }

                // Period total
                VStack(alignment: .leading, spacing: 2) {
                    Text("\(store.periodDays)d Total")
                        .font(.system(size: 10))
                        .foregroundStyle(.tertiary)
                    Text(store.formattedTotalCost)
                        .font(.system(size: 14, weight: .semibold, design: .rounded))
                }

                Spacer()

                // Missing data badge
                if store.hasMissingEntries {
                    HStack(spacing: 3) {
                        Image(systemName: "exclamationmark.circle")
                            .font(.system(size: 9))
                        Text("\(store.missingDaysCount)")
                            .font(.system(size: 10, weight: .medium))
                    }
                    .foregroundStyle(.orange)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 2)
                    .background(
                        Capsule()
                            .fill(Color.orange.opacity(0.15))
                    )
                    .help("\(store.missingDaysCount) day\(store.missingDaysCount == 1 ? "" : "s") missing data")
                }
            }

            // Mini chart
            miniChart
        }
        .padding(10)
        .background(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .fill(Color(NSColor.controlBackgroundColor))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .stroke(isHovered ? Color.accentColor.opacity(0.3) : Color.gray.opacity(0.1), lineWidth: 1)
        )
        .scaleEffect(isHovered ? 1.01 : 1.0)
        .animation(.spring(response: 0.3, dampingFraction: 0.8), value: isHovered)
        .onHover { hovering in
            isHovered = hovering
        }
    }

    // MARK: - Mini Chart

    private var miniChart: some View {
        Chart(store.entries) { entry in
            BarMark(
                x: .value("Date", entry.date, unit: .day),
                y: .value("Cost", entry.cost)
            )
            .foregroundStyle(
                LinearGradient(
                    colors: [Color.accentColor.opacity(0.6), Color.accentColor],
                    startPoint: .bottom,
                    endPoint: .top
                )
            )
            .cornerRadius(2)
        }
        .chartXAxis(.hidden)
        .chartYAxis(.hidden)
        .frame(height: 40)
    }
}

// MARK: - State Views

struct CostMenuErrorView: View {
    let message: String

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.orange)
                .font(.caption)

            Text(message)
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(2)
        }
        .padding(8)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            RoundedRectangle(cornerRadius: 6, style: .continuous)
                .fill(Color.orange.opacity(0.1))
        )
    }
}

struct CostMenuEmptyView: View {
    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "chart.bar")
                .foregroundStyle(.tertiary)
                .font(.caption)

            Text("No cost data available")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(8)
    }
}

struct CostMenuSkeletonView: View {
    @State private var isAnimating = false

    var body: some View {
        VStack(spacing: 8) {
            HStack(spacing: 16) {
                VStack(alignment: .leading, spacing: 4) {
                    RoundedRectangle(cornerRadius: 3)
                        .fill(Color.gray.opacity(0.2))
                        .frame(width: 40, height: 10)
                    RoundedRectangle(cornerRadius: 3)
                        .fill(Color.gray.opacity(0.2))
                        .frame(width: 50, height: 14)
                }

                VStack(alignment: .leading, spacing: 4) {
                    RoundedRectangle(cornerRadius: 3)
                        .fill(Color.gray.opacity(0.2))
                        .frame(width: 50, height: 10)
                    RoundedRectangle(cornerRadius: 3)
                        .fill(Color.gray.opacity(0.2))
                        .frame(width: 60, height: 14)
                }

                Spacer()
            }

            // Skeleton bars
            HStack(alignment: .bottom, spacing: 4) {
                ForEach(0..<7, id: \.self) { _ in
                    RoundedRectangle(cornerRadius: 2)
                        .fill(Color.gray.opacity(0.15))
                        .frame(height: CGFloat.random(in: 15...35))
                }
            }
            .frame(height: 40)
        }
        .padding(10)
        .background(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .fill(Color(NSColor.controlBackgroundColor))
        )
        .opacity(isAnimating ? 0.5 : 1)
        .animation(.easeInOut(duration: 1).repeatForever(autoreverses: true), value: isAnimating)
        .onAppear { isAnimating = true }
    }
}

// MARK: - Expanded Menu View

/// A larger version of the cost menu view for expanded display.
struct CostUsageExpandedMenuView: View {
    @ObservedObject var store: CostUsageStore

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            // Header
            HStack {
                Text("Cost Overview")
                    .font(.subheadline.weight(.semibold))

                if store.isDemoData {
                    Text("Sample")
                        .font(.caption2.weight(.medium))
                        .foregroundStyle(.secondary)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(
                            Capsule()
                                .fill(Color.secondary.opacity(0.12))
                        )
                }

                Spacer()

                if store.isLoading {
                    ProgressView()
                        .controlSize(.small)
                }
            }

            // Summary cards
            HStack(spacing: 12) {
                CostSummaryCard(
                    title: "Today",
                    value: store.formattedTodayCost,
                    icon: "calendar",
                    isAvailable: store.todayCost != nil
                )

                CostSummaryCard(
                    title: "Last \(store.periodDays) days",
                    value: store.formattedTotalCost,
                    icon: "chart.bar.fill",
                    isAvailable: true
                )
            }

            // Chart
            if !store.entries.isEmpty {
                expandedChart
            }

            // Missing data warning
            if store.hasMissingEntries {
                HStack(spacing: 6) {
                    Image(systemName: "exclamationmark.circle")
                        .font(.caption)
                        .foregroundStyle(.orange)

                    Text("\(store.missingDaysCount) day\(store.missingDaysCount == 1 ? "" : "s") missing data")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 6)
                .background(
                    RoundedRectangle(cornerRadius: 6, style: .continuous)
                        .fill(Color.orange.opacity(0.1))
                )
            }
        }
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .fill(Color(NSColor.windowBackgroundColor))
        )
    }

    private var expandedChart: some View {
        Chart(store.entries) { entry in
            BarMark(
                x: .value("Date", entry.date, unit: .day),
                y: .value("Cost", entry.cost)
            )
            .foregroundStyle(Color.accentColor.opacity(0.8))
            .cornerRadius(3)
        }
        .chartXAxis {
            AxisMarks(values: .stride(by: .day)) { value in
                AxisValueLabel {
                    if let date = value.as(Date.self) {
                        Text(formatShortDate(date))
                            .font(.system(size: 9))
                    }
                }
            }
        }
        .chartYAxis {
            AxisMarks(position: .leading) { value in
                AxisValueLabel {
                    if let cost = value.as(Double.self) {
                        Text("$\(Int(cost))")
                            .font(.system(size: 9))
                    }
                }
            }
        }
        .frame(height: 80)
    }

    private func formatShortDate(_ date: Date) -> String {
        let formatter = DateFormatter()
        formatter.dateFormat = "d"
        return formatter.string(from: date)
    }
}

// MARK: - Summary Card

struct CostSummaryCard: View {
    let title: String
    let value: String
    let icon: String
    let isAvailable: Bool

    @State private var isHovered = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 4) {
                Image(systemName: icon)
                    .font(.system(size: 10))
                    .foregroundStyle(.tertiary)
                Text(title)
                    .font(.system(size: 10))
                    .foregroundStyle(.secondary)
            }

            Text(value)
                .font(.system(size: 16, weight: .semibold, design: .rounded))
                .foregroundStyle(isAvailable ? .primary : .tertiary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(10)
        .background(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .fill(Color(NSColor.controlBackgroundColor))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .stroke(isHovered ? Color.accentColor.opacity(0.2) : Color.gray.opacity(0.1), lineWidth: 1)
        )
        .scaleEffect(isHovered ? 1.02 : 1.0)
        .animation(.spring(response: 0.3, dampingFraction: 0.8), value: isHovered)
        .onHover { hovering in
            isHovered = hovering
        }
    }
}

// MARK: - Preview

#Preview("Compact") {
    CostUsageMenuView(store: {
        let store = CostUsageStore()
        return store
    }())
    .frame(width: 280)
    .padding()
}

#Preview("Expanded") {
    CostUsageExpandedMenuView(store: {
        let store = CostUsageStore()
        return store
    }())
    .frame(width: 320)
    .padding()
}
