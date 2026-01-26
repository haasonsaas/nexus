import Charts
import SwiftUI

// MARK: - Data Models

/// Represents daily cost data for a single day.
struct DailyCostEntry: Identifiable, Hashable {
    let id = UUID()
    let date: Date
    let cost: Double
    let provider: String?

    var formattedCost: String {
        Self.currencyFormatter.string(from: NSNumber(value: cost)) ?? "$\(String(format: "%.2f", cost))"
    }

    static let currencyFormatter: NumberFormatter = {
        let formatter = NumberFormatter()
        formatter.numberStyle = .currency
        formatter.currencyCode = "USD"
        formatter.minimumFractionDigits = 2
        formatter.maximumFractionDigits = 2
        return formatter
    }()
}

struct CostUsageResponse: Decodable {
    let entries: [CostUsageEntry]
}

struct CostUsageEntry: Decodable {
    let date: Date
    let cost: Double
    let provider: String?
}

/// Store for managing cost/usage data.
@MainActor
final class CostUsageStore: ObservableObject {
    @Published private(set) var entries: [DailyCostEntry] = []
    @Published private(set) var isLoading = false
    @Published private(set) var error: String?
    @Published private(set) var isDemoData = false

    /// Number of days to display in the chart.
    var periodDays: Int = 7

    /// Total cost for the current period.
    var totalCost: Double {
        entries.reduce(0) { $0 + $1.cost }
    }

    /// Today's cost (if available).
    var todayCost: Double? {
        let calendar = Calendar.current
        let today = calendar.startOfDay(for: Date())
        return entries.first { calendar.isDate($0.date, inSameDayAs: today) }?.cost
    }

    /// Number of days with missing data in the period.
    var missingDaysCount: Int {
        let calendar = Calendar.current
        let today = calendar.startOfDay(for: Date())
        let expectedDates = (0..<periodDays).compactMap {
            calendar.date(byAdding: .day, value: -$0, to: today)
        }
        let existingDates = Set(entries.map { calendar.startOfDay(for: $0.date) })
        return expectedDates.filter { !existingDates.contains($0) }.count
    }

    /// Whether there are missing entries in the period.
    var hasMissingEntries: Bool {
        missingDaysCount > 0
    }

    /// Formatted total cost string.
    var formattedTotalCost: String {
        DailyCostEntry.currencyFormatter.string(from: NSNumber(value: totalCost)) ?? "$\(String(format: "%.2f", totalCost))"
    }

    /// Formatted today's cost string.
    var formattedTodayCost: String {
        guard let cost = todayCost else { return "--" }
        return DailyCostEntry.currencyFormatter.string(from: NSNumber(value: cost)) ?? "$\(String(format: "%.2f", cost))"
    }

    func refresh(api: NexusAPI) async {
        isLoading = true
        defer { isLoading = false }

        do {
            let result = try await api.fetchCostUsage(days: periodDays)
            entries = result.entries
                .sorted { $0.date < $1.date }
                .map { entry in
                    DailyCostEntry(date: entry.date, cost: entry.cost, provider: entry.provider)
                }
            error = nil
            isDemoData = false
        } catch {
            entries = generateSampleData()
            self.error = nil
            isDemoData = true
        }
    }

    private func generateSampleData() -> [DailyCostEntry] {
        let calendar = Calendar.current
        let today = calendar.startOfDay(for: Date())

        return (0..<periodDays).compactMap { dayOffset in
            guard let date = calendar.date(byAdding: .day, value: -dayOffset, to: today) else {
                return nil
            }
            // Skip some days randomly to demonstrate missing data indicator
            if dayOffset == 3 {
                return nil
            }
            let cost = Double.random(in: 0.50...15.00)
            return DailyCostEntry(date: date, cost: cost, provider: nil)
        }.reversed()
    }
}

// MARK: - Main Chart View

/// A Swift Charts-based view showing daily cost/usage data.
struct CostUsageChartView: View {
    @ObservedObject var store: CostUsageStore
    @State private var selectedEntry: DailyCostEntry?
    @State private var isHovered = false

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            // Header section
            headerSection

            // Chart section
            if store.isLoading && store.entries.isEmpty {
                CostChartSkeletonView()
            } else if let error = store.error {
                CostChartErrorView(message: error)
            } else if store.entries.isEmpty {
                CostChartEmptyView()
            } else {
                chartSection
            }

            // Missing data indicator
            if store.hasMissingEntries && !store.entries.isEmpty {
                missingDataIndicator
            }
        }
        .padding()
        .animation(.easeInOut(duration: 0.2), value: store.isLoading)
        .animation(.easeInOut(duration: 0.2), value: store.entries.count)
    }

    // MARK: - Header Section

    private var headerSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("Cost Overview")
                    .font(.title2.weight(.semibold))

                if store.isDemoData {
                    Text("Sample")
                        .font(.caption2.weight(.medium))
                        .foregroundStyle(.secondary)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(
                            Capsule()
                                .fill(Color.secondary.opacity(0.15))
                        )
                }

                Spacer()

                if store.isLoading {
                    ProgressView()
                        .controlSize(.small)
                }
            }

            HStack(spacing: 24) {
                // Today's cost
                VStack(alignment: .leading, spacing: 4) {
                    Text("Today")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Text(store.formattedTodayCost)
                        .font(.system(.title3, design: .rounded).weight(.semibold))
                        .foregroundStyle(store.todayCost != nil ? .primary : .tertiary)
                }

                Divider()
                    .frame(height: 36)

                // Period total
                VStack(alignment: .leading, spacing: 4) {
                    Text("Last \(store.periodDays) days")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Text(store.formattedTotalCost)
                        .font(.system(.title3, design: .rounded).weight(.semibold))
                }

                Spacer()
            }
        }
    }

    // MARK: - Chart Section

    private var chartSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Chart(store.entries) { entry in
                BarMark(
                    x: .value("Date", entry.date, unit: .day),
                    y: .value("Cost", entry.cost)
                )
                .foregroundStyle(
                    selectedEntry?.id == entry.id
                        ? Color.accentColor
                        : Color.accentColor.opacity(0.7)
                )
                .cornerRadius(4)
            }
            .chartXAxis {
                AxisMarks(values: .stride(by: .day)) { value in
                    AxisGridLine()
                    AxisValueLabel {
                        if let date = value.as(Date.self) {
                            Text(formatDateLabel(date))
                                .font(.caption2)
                        }
                    }
                }
            }
            .chartYAxis {
                AxisMarks(position: .leading) { value in
                    AxisGridLine()
                    AxisValueLabel {
                        if let cost = value.as(Double.self) {
                            Text(formatCurrencyLabel(cost))
                                .font(.caption2)
                        }
                    }
                }
            }
            .chartOverlay { proxy in
                GeometryReader { geometry in
                    Rectangle()
                        .fill(Color.clear)
                        .contentShape(Rectangle())
                        .gesture(
                            DragGesture(minimumDistance: 0)
                                .onChanged { value in
                                    handleChartInteraction(at: value.location, proxy: proxy, geometry: geometry)
                                }
                                .onEnded { _ in
                                    selectedEntry = nil
                                }
                        )
                }
            }
            .frame(height: 200)
            .padding(.top, 8)

            // Selection tooltip
            if let entry = selectedEntry {
                HStack(spacing: 8) {
                    Circle()
                        .fill(Color.accentColor)
                        .frame(width: 8, height: 8)
                    Text(formatFullDate(entry.date))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Text(entry.formattedCost)
                        .font(.caption.weight(.semibold))
                }
                .padding(.horizontal, 10)
                .padding(.vertical, 6)
                .background(
                    RoundedRectangle(cornerRadius: 6, style: .continuous)
                        .fill(Color(NSColor.controlBackgroundColor))
                )
                .transition(.opacity.combined(with: .scale(scale: 0.95)))
            }
        }
    }

    // MARK: - Missing Data Indicator

    private var missingDataIndicator: some View {
        HStack(spacing: 8) {
            Image(systemName: "exclamationmark.circle")
                .foregroundStyle(.orange)
                .font(.caption)

            Text("\(store.missingDaysCount) day\(store.missingDaysCount == 1 ? "" : "s") missing cost data")
                .font(.caption)
                .foregroundStyle(.secondary)

            Spacer()
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 8)
        .background(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .fill(Color.orange.opacity(0.1))
        )
    }

    // MARK: - Helper Functions

    private func handleChartInteraction(at location: CGPoint, proxy: ChartProxy, geometry: GeometryProxy) {
        let xPosition = location.x - geometry[proxy.plotFrame!].origin.x

        guard let date: Date = proxy.value(atX: xPosition) else { return }

        let calendar = Calendar.current
        selectedEntry = store.entries.first { entry in
            calendar.isDate(entry.date, inSameDayAs: date)
        }
    }

    private func formatDateLabel(_ date: Date) -> String {
        let formatter = DateFormatter()
        formatter.dateFormat = "E"
        return formatter.string(from: date)
    }

    private func formatFullDate(_ date: Date) -> String {
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        formatter.timeStyle = .none
        return formatter.string(from: date)
    }

    private func formatCurrencyLabel(_ value: Double) -> String {
        if value >= 1 {
            return "$\(Int(value))"
        }
        return "$\(String(format: "%.2f", value))"
    }
}

// MARK: - State Views

struct CostChartSkeletonView: View {
    @State private var isAnimating = false

    var body: some View {
        VStack(spacing: 12) {
            HStack {
                RoundedRectangle(cornerRadius: 4)
                    .fill(Color.gray.opacity(0.2))
                    .frame(width: 80, height: 16)

                Spacer()

                RoundedRectangle(cornerRadius: 4)
                    .fill(Color.gray.opacity(0.2))
                    .frame(width: 60, height: 16)
            }

            // Skeleton bars
            HStack(alignment: .bottom, spacing: 8) {
                ForEach(0..<7, id: \.self) { index in
                    RoundedRectangle(cornerRadius: 4)
                        .fill(Color.gray.opacity(0.15))
                        .frame(height: CGFloat.random(in: 40...160))
                }
            }
            .frame(height: 180)
        }
        .opacity(isAnimating ? 0.5 : 1)
        .animation(.easeInOut(duration: 1).repeatForever(autoreverses: true), value: isAnimating)
        .onAppear { isAnimating = true }
    }
}

struct CostChartErrorView: View {
    let message: String

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.orange)

            VStack(alignment: .leading, spacing: 2) {
                Text("Failed to load cost data")
                    .font(.subheadline.weight(.medium))
                Text(message)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }
        }
        .padding()
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .fill(Color.orange.opacity(0.1))
        )
    }
}

struct CostChartEmptyView: View {
    var body: some View {
        VStack(spacing: 12) {
            Image(systemName: "chart.bar")
                .font(.system(size: 32))
                .foregroundStyle(.tertiary)

            Text("No cost data available")
                .font(.subheadline)
                .foregroundStyle(.secondary)

            Text("Cost data will appear here once usage is recorded.")
                .font(.caption)
                .foregroundStyle(.tertiary)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity)
        .frame(height: 200)
        .background(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .fill(Color(NSColor.controlBackgroundColor))
        )
    }
}

// MARK: - Preview

#Preview {
    CostUsageChartView(store: {
        let store = CostUsageStore()
        return store
    }())
    .frame(width: 400, height: 400)
}
