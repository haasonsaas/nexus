import SwiftUI

// MARK: - WatchdogStatusView

/// Shows connection watchdog status and controls
struct WatchdogStatusView: View {
    @State private var watchdog = ConnectionWatchdog.shared
    @State private var health = HealthStore.shared

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            // Overall status header
            HStack {
                statusIcon
                VStack(alignment: .leading, spacing: 2) {
                    Text(statusTitle)
                        .font(.headline)
                    Text(statusSubtitle)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Spacer()
            }

            Divider()

            // Watchdog details
            Group {
                LabeledContent("Watchdog State", value: watchdog.state.rawValue.capitalized)

                if let lastTick = watchdog.lastTick {
                    LabeledContent("Last Tick") {
                        Text(lastTick, style: .relative)
                    }
                }

                if let lastHealth = watchdog.lastHealthCheck {
                    LabeledContent("Last Health Check") {
                        Text(lastHealth, style: .relative)
                    }
                }

                if watchdog.recoveryAttempts > 0 {
                    LabeledContent("Recovery Attempts") {
                        Text("\(watchdog.recoveryAttempts) / \(watchdog.maxRecoveryAttempts)")
                            .foregroundStyle(
                                watchdog.recoveryAttempts > watchdog.maxRecoveryAttempts / 2
                                    ? .orange
                                    : .secondary
                            )
                    }
                }

                if let nextRetry = watchdog.nextRetryAt, nextRetry > Date() {
                    LabeledContent("Next Retry") {
                        Text(nextRetry, style: .relative)
                            .foregroundStyle(.blue)
                    }
                }

                if let reason = watchdog.lastRecoveryReason {
                    LabeledContent("Last Recovery Reason") {
                        Text(reason)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
            }

            Divider()

            // Actions
            HStack(spacing: 8) {
                Button("Force Reconnect") {
                    Task {
                        await watchdog.forceReconnect()
                    }
                }
                .buttonStyle(.bordered)
                .disabled(watchdog.state == .recovering)

                Button("Reset Backoff") {
                    watchdog.resetBackoff()
                }
                .buttonStyle(.bordered)
                .disabled(watchdog.state == .idle)

                Spacer()

                if watchdog.isActive {
                    Button("Stop") {
                        watchdog.stop()
                    }
                    .buttonStyle(.bordered)
                    .tint(.red)
                } else {
                    Button("Start") {
                        watchdog.start()
                    }
                    .buttonStyle(.bordered)
                    .tint(.green)
                }
            }
        }
        .padding()
    }

    // MARK: - Status Icon

    private var statusIcon: some View {
        Group {
            switch health.state {
            case .ok:
                Image(systemName: "checkmark.circle.fill")
                    .foregroundStyle(.green)
            case .degraded:
                Image(systemName: "exclamationmark.triangle.fill")
                    .foregroundStyle(.orange)
            case .linkingNeeded:
                Image(systemName: "link.badge.plus")
                    .foregroundStyle(.blue)
            case .unknown:
                Image(systemName: "questionmark.circle.fill")
                    .foregroundStyle(.secondary)
            }
        }
        .font(.title)
    }

    // MARK: - Status Text

    private var statusTitle: String {
        switch health.state {
        case .ok:
            return "Connected"
        case .degraded(let message):
            return "Degraded: \(message)"
        case .linkingNeeded:
            return "Linking Required"
        case .unknown:
            return "Unknown"
        }
    }

    private var statusSubtitle: String {
        switch watchdog.state {
        case .idle:
            return "Watchdog not running"
        case .monitoring:
            return "Monitoring connection"
        case .recovering:
            return "Attempting recovery..."
        case .backingOff:
            if let timeUntilRetry = watchdog.timeUntilRetry {
                return "Backing off, retry in \(Int(timeUntilRetry))s"
            }
            return "Backing off, next retry soon"
        }
    }
}

// MARK: - CircuitBreakerStatusView

/// Shows circuit breaker status for gateway operations
struct CircuitBreakerStatusView: View {
    let breaker: CircuitBreaker

    var body: some View {
        HStack {
            Circle()
                .fill(stateColor)
                .frame(width: 8, height: 8)

            Text(breaker.name)
                .font(.caption)
                .fontWeight(.medium)

            Spacer()

            Text(breaker.statusSummary)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 2)
    }

    private var stateColor: Color {
        switch breaker.state {
        case .closed:
            return .green
        case .halfOpen:
            return .yellow
        case .open:
            return .red
        }
    }
}

// MARK: - CircuitBreakerListView

/// Shows all registered circuit breakers
struct CircuitBreakerListView: View {
    @State private var registry = CircuitBreakerRegistry.shared

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text("Circuit Breakers")
                    .font(.headline)
                Spacer()
                Button("Reset All") {
                    registry.resetAll()
                }
                .font(.caption)
                .buttonStyle(.borderless)
            }

            Divider()

            if registry.allBreakers.isEmpty {
                Text("No circuit breakers registered")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            } else {
                ForEach(registry.allBreakers, id: \.name) { breaker in
                    CircuitBreakerStatusView(breaker: breaker)
                }
            }
        }
        .padding()
    }
}

// MARK: - Combined Connection Health View

/// Combined view showing watchdog and circuit breaker status
struct ConnectionHealthView: View {
    var body: some View {
        ScrollView {
            VStack(spacing: 16) {
                WatchdogStatusView()
                    .background(Color(.windowBackgroundColor))
                    .clipShape(RoundedRectangle(cornerRadius: 8))

                CircuitBreakerListView()
                    .background(Color(.windowBackgroundColor))
                    .clipShape(RoundedRectangle(cornerRadius: 8))
            }
            .padding()
        }
    }
}

// MARK: - Compact Status Badge

/// Compact status indicator for menu bar or toolbar
struct WatchdogStatusBadge: View {
    @State private var watchdog = ConnectionWatchdog.shared
    @State private var health = HealthStore.shared

    var body: some View {
        HStack(spacing: 4) {
            Circle()
                .fill(statusColor)
                .frame(width: 6, height: 6)

            if watchdog.isRecovering {
                ProgressView()
                    .scaleEffect(0.5)
                    .frame(width: 10, height: 10)
            }
        }
        .help(watchdog.statusSummary)
    }

    private var statusColor: Color {
        switch (health.state, watchdog.state) {
        case (.ok, .monitoring):
            return .green
        case (_, .recovering):
            return .yellow
        case (_, .backingOff):
            return .orange
        case (.degraded, _):
            return .orange
        case (.linkingNeeded, _):
            return .blue
        default:
            return .gray
        }
    }
}

// MARK: - Previews

#Preview("Watchdog Status") {
    WatchdogStatusView()
        .frame(width: 350)
}

#Preview("Circuit Breaker List") {
    CircuitBreakerListView()
        .frame(width: 300)
        .onAppear {
            // Register some test breakers
            _ = CircuitBreakerRegistry.shared.healthCheck
            _ = CircuitBreakerRegistry.shared.gatewayRequest
        }
}

#Preview("Combined Health View") {
    ConnectionHealthView()
        .frame(width: 400, height: 500)
}

#Preview("Status Badge") {
    HStack {
        WatchdogStatusBadge()
        Text("Nexus")
    }
    .padding()
}
