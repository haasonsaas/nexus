import SwiftUI

// MARK: - HeartbeatIndicatorView

/// A view that displays connection health status with a pulsing indicator dot.
/// Shows green/yellow/red based on connection quality with tooltip details.
struct HeartbeatIndicatorView: View {
    let store: HeartbeatStore

    @State private var isPulsing = false

    var body: some View {
        HStack(spacing: 6) {
            pulsingDot
            qualityLabel
        }
        .help(tooltipText)
        .onAppear {
            startPulseAnimation()
        }
    }

    // MARK: - Subviews

    private var pulsingDot: some View {
        ZStack {
            // Outer pulse ring (only when excellent/good)
            if store.connectionQuality == .excellent || store.connectionQuality == .good {
                Circle()
                    .fill(store.connectionQuality.color.opacity(0.3))
                    .frame(width: 12, height: 12)
                    .scaleEffect(isPulsing ? 1.5 : 1.0)
                    .opacity(isPulsing ? 0 : 0.6)
            }

            // Inner dot
            Circle()
                .fill(store.connectionQuality.color)
                .frame(width: 8, height: 8)
                .shadow(color: store.connectionQuality.color.opacity(0.5), radius: 2)
        }
        .frame(width: 14, height: 14)
    }

    private var qualityLabel: some View {
        Text(store.connectionQuality.rawValue)
            .font(.caption)
            .foregroundStyle(labelColor)
    }

    // MARK: - Computed Properties

    private var labelColor: Color {
        switch store.connectionQuality {
        case .excellent, .good:
            return .secondary
        case .fair:
            return .yellow
        case .poor:
            return .orange
        case .disconnected:
            return .red
        }
    }

    private var tooltipText: String {
        var lines: [String] = []

        // Connection quality
        lines.append("Connection: \(store.connectionQuality.rawValue)")

        // Last heartbeat time
        if let lastTime = store.lastHeartbeatTime {
            let formatter = DateFormatter()
            formatter.dateStyle = .none
            formatter.timeStyle = .medium
            lines.append("Last heartbeat: \(formatter.string(from: lastTime))")

            // Time since last heartbeat
            if let elapsed = store.timeSinceLastHeartbeat {
                lines.append("(\(formatElapsed(elapsed)) ago)")
            }
        } else {
            lines.append("Last heartbeat: Never")
        }

        // Average interval
        if store.heartbeatInterval > 0 {
            lines.append("Avg interval: \(String(format: "%.1f", store.heartbeatInterval))s")
        }

        // Missed heartbeats
        if store.missedHeartbeats > 0 {
            lines.append("Missed: \(store.missedHeartbeats)")
        }

        return lines.joined(separator: "\n")
    }

    // MARK: - Helpers

    private func startPulseAnimation() {
        guard store.connectionQuality == .excellent || store.connectionQuality == .good else {
            isPulsing = false
            return
        }

        withAnimation(
            .easeInOut(duration: 1.5)
            .repeatForever(autoreverses: false)
        ) {
            isPulsing = true
        }
    }

    private func formatElapsed(_ interval: TimeInterval) -> String {
        if interval < 60 {
            return "\(Int(interval))s"
        } else if interval < 3600 {
            return "\(Int(interval / 60))m"
        } else {
            return "\(Int(interval / 3600))h"
        }
    }
}

// MARK: - Compact Variant

/// A compact heartbeat indicator showing just the pulsing dot.
struct HeartbeatDotView: View {
    let store: HeartbeatStore

    @State private var isPulsing = false

    var body: some View {
        ZStack {
            // Outer pulse ring
            if store.isHealthy {
                Circle()
                    .fill(store.connectionQuality.color.opacity(0.3))
                    .frame(width: 10, height: 10)
                    .scaleEffect(isPulsing ? 1.8 : 1.0)
                    .opacity(isPulsing ? 0 : 0.5)
            }

            // Inner dot
            Circle()
                .fill(store.connectionQuality.color)
                .frame(width: 6, height: 6)
        }
        .frame(width: 12, height: 12)
        .help(store.lastHeartbeatFormatted)
        .onAppear {
            if store.isHealthy {
                withAnimation(
                    .easeInOut(duration: 1.2)
                    .repeatForever(autoreverses: false)
                ) {
                    isPulsing = true
                }
            }
        }
        .onChange(of: store.connectionQuality) { _, newValue in
            let healthy = newValue == .excellent || newValue == .good || newValue == .fair
            if healthy && !isPulsing {
                withAnimation(
                    .easeInOut(duration: 1.2)
                    .repeatForever(autoreverses: false)
                ) {
                    isPulsing = true
                }
            } else if !healthy {
                isPulsing = false
            }
        }
    }
}

// MARK: - Detailed Status View

/// A detailed heartbeat status view for settings or diagnostics panels.
struct HeartbeatDetailView: View {
    let store: HeartbeatStore

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            // Header with indicator
            HStack {
                HeartbeatIndicatorView(store: store)
                Spacer()
                if store.missedHeartbeats > 0 {
                    Label("\(store.missedHeartbeats) missed", systemImage: "exclamationmark.triangle")
                        .font(.caption)
                        .foregroundStyle(.orange)
                }
            }

            Divider()

            // Stats grid
            Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 4) {
                GridRow {
                    Text("Last Heartbeat")
                        .foregroundStyle(.secondary)
                    Text(store.lastHeartbeatFormatted)
                }

                GridRow {
                    Text("Avg Interval")
                        .foregroundStyle(.secondary)
                    Text(store.heartbeatInterval > 0 ? "\(String(format: "%.1f", store.heartbeatInterval))s" : "-")
                }

                GridRow {
                    Text("History")
                        .foregroundStyle(.secondary)
                    Text("\(store.history.count) events")
                }

                GridRow {
                    Text("Timeout")
                        .foregroundStyle(.secondary)
                    Text("\(Int(store.timeoutThreshold))s")
                }
            }
            .font(.caption)
        }
        .padding()
        .background(Color(nsColor: .controlBackgroundColor))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }
}

// MARK: - Preview

#if DEBUG
#Preview("Heartbeat Indicator") {
    VStack(spacing: 20) {
        HeartbeatIndicatorView(store: .shared)
        HeartbeatDotView(store: .shared)
        HeartbeatDetailView(store: .shared)
            .frame(width: 250)
    }
    .padding()
}
#endif
