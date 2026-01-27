import SwiftUI

struct UsageMenuView: View {
    @ObservedObject var store: UsageStore

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            // Header
            HStack {
                Text("Usage")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)

                Spacer()

                if store.isLoading {
                    ProgressView()
                        .controlSize(.mini)
                }
            }

            // Content
            if let error = store.error {
                UsageErrorView(message: error)
            } else if store.rows.isEmpty {
                if store.isLoading {
                    UsageSkeletonView()
                } else {
                    UsageEmptyView()
                }
            } else {
                VStack(spacing: 6) {
                    ForEach(store.rows) { row in
                        ImprovedUsageRowView(row: row)
                    }
                }
            }
        }
        .animation(.easeInOut(duration: 0.2), value: store.isLoading)
        .animation(.easeInOut(duration: 0.2), value: store.rows.count)
    }
}

// MARK: - Improved Usage Row

struct ImprovedUsageRowView: View {
    let row: UsageRow

    @State private var isHovered = false

    private var progressColor: Color {
        guard let pct = row.usedPercent else { return .gray }
        if pct >= 95 { return .red }
        if pct >= 80 { return .orange }
        if pct >= 60 { return .yellow }
        return .green
    }

    private var statusIcon: String {
        guard let pct = row.usedPercent else { return "questionmark.circle" }
        if pct >= 95 { return "exclamationmark.triangle.fill" }
        if pct >= 80 { return "exclamationmark.circle.fill" }
        return "checkmark.circle.fill"
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            // Provider header
            HStack(spacing: 8) {
                Image(systemName: providerIcon)
                    .font(.caption)
                    .foregroundStyle(progressColor)
                    .frame(width: 14)

                Text(row.titleText)
                    .font(.system(size: 12, weight: .medium))
                    .lineLimit(1)

                Spacer()

                // Remaining percentage badge
                if let remaining = row.remainingPercent {
                    HStack(spacing: 3) {
                        Image(systemName: statusIcon)
                            .font(.system(size: 9))
                        Text("\(remaining)%")
                            .font(.system(size: 10, weight: .semibold, design: .rounded))
                    }
                    .foregroundStyle(progressColor)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 2)
                    .background(
                        Capsule()
                            .fill(progressColor.opacity(0.15))
                    )
                }
            }

            // Progress bar
            if let pct = row.usedPercent {
                ImprovedUsageProgressBar(usedPercent: pct, color: progressColor)
                    .frame(height: 6)
            }

            // Detail text
            HStack(spacing: 8) {
                if let window = row.windowLabel, !window.isEmpty {
                    Label(window, systemImage: "clock")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }

                if let resetAt = row.resetAt {
                    let resetText = UsageRow.formatResetRemaining(target: resetAt, now: Date()) ?? ""
                    if !resetText.isEmpty {
                        Label("Resets \(resetText)", systemImage: "arrow.clockwise")
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                }

                Spacer()
            }
        }
        .padding(10)
        .background(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .fill(Color(NSColor.controlBackgroundColor))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .stroke(isHovered ? progressColor.opacity(0.3) : Color.gray.opacity(0.1), lineWidth: 1)
        )
        .scaleEffect(isHovered ? 1.01 : 1.0)
        .animation(.spring(response: 0.3, dampingFraction: 0.8), value: isHovered)
        .onHover { hovering in
            isHovered = hovering
        }
    }

    private var providerIcon: String {
        let provider = row.providerId.lowercased()
        if provider.contains("anthropic") { return "brain" }
        if provider.contains("openai") { return "sparkles" }
        if provider.contains("google") { return "g.circle" }
        if provider.contains("claude") { return "brain.head.profile" }
        return "cpu"
    }
}

// MARK: - Improved Progress Bar

struct ImprovedUsageProgressBar: View {
    let usedPercent: Double
    let color: Color

    private var fraction: Double {
        min(1, max(0, usedPercent / 100))
    }

    var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                // Background track
                RoundedRectangle(cornerRadius: 3)
                    .fill(Color.gray.opacity(0.15))

                // Progress fill with gradient
                RoundedRectangle(cornerRadius: 3)
                    .fill(
                        LinearGradient(
                            colors: [color.opacity(0.7), color],
                            startPoint: .leading,
                            endPoint: .trailing
                        )
                    )
                    .frame(width: max(4, geo.size.width * fraction))

                // Shimmer effect for high usage
                if usedPercent >= 80 {
                    RoundedRectangle(cornerRadius: 3)
                        .fill(
                            LinearGradient(
                                colors: [.clear, .white.opacity(0.3), .clear],
                                startPoint: .leading,
                                endPoint: .trailing
                            )
                        )
                        .frame(width: max(4, geo.size.width * fraction))
                }
            }
        }
    }
}

// MARK: - State Views

struct UsageErrorView: View {
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

struct UsageEmptyView: View {
    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "chart.bar")
                .foregroundStyle(.tertiary)
                .font(.caption)

            Text("No usage data available")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(8)
    }
}

struct UsageSkeletonView: View {
    @State private var isAnimating = false

    var body: some View {
        VStack(spacing: 8) {
            ForEach(0..<2, id: \.self) { _ in
                VStack(alignment: .leading, spacing: 6) {
                    HStack {
                        RoundedRectangle(cornerRadius: 4)
                            .fill(Color.gray.opacity(0.2))
                            .frame(width: 100, height: 12)

                        Spacer()

                        RoundedRectangle(cornerRadius: 8)
                            .fill(Color.gray.opacity(0.2))
                            .frame(width: 40, height: 16)
                    }

                    RoundedRectangle(cornerRadius: 3)
                        .fill(Color.gray.opacity(0.15))
                        .frame(height: 6)
                }
                .padding(10)
                .background(
                    RoundedRectangle(cornerRadius: 8)
                        .fill(Color(NSColor.controlBackgroundColor))
                )
            }
        }
        .opacity(isAnimating ? 0.5 : 1)
        .animation(.easeInOut(duration: 1).repeatForever(autoreverses: true), value: isAnimating)
        .onAppear { isAnimating = true }
    }
}

// MARK: - Compatibility Support

struct UsageRowView: View {
    let row: UsageRow

    var body: some View {
        ImprovedUsageRowView(row: row)
    }
}

struct UsageProgressBar: View {
    let usedPercent: Double

    private var tint: Color {
        if usedPercent >= 95 { return .red }
        if usedPercent >= 80 { return .orange }
        if usedPercent >= 60 { return .yellow }
        return .green
    }

    var body: some View {
        ImprovedUsageProgressBar(usedPercent: usedPercent, color: tint)
    }
}
