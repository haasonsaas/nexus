import SwiftUI


// MARK: - StatusIndicatorView

struct StatusIndicatorView: View {
    let label: String
    let color: Color

    var body: some View {
        HStack(spacing: 6) {
            Circle()
                .fill(color)
                .frame(width: 6, height: 6)
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }
}

// MARK: - ConnectionStatusView

struct ConnectionStatusView: View {
    let status: GatewayProcessManager.Status

    var body: some View {
        StatusIndicatorView(label: statusLabel, color: statusColor)
    }

    private var statusLabel: String {
        switch status {
        case .running:
            return "Running"
        case .attachedExisting:
            return "Running"
        case .stopped:
            return "Stopped"
        case .starting:
            return "Starting..."
        case .failed(let reason):
            return "Failed: \(reason)"
        }
    }

    private var statusColor: Color {
        switch status {
        case .running, .attachedExisting:
            return .green
        case .stopped:
            return .gray
        case .starting:
            return .yellow
        case .failed:
            return .red
        }
    }
}

// MARK: - HealthStatusView

struct HealthStatusView: View {
    let store: HealthStore

    var body: some View {
        StatusIndicatorView(label: statusLabel, color: statusColor)
    }

    private var statusLabel: String {
        if store.isRefreshing {
            return "Checking..."
        }

        switch store.state {
        case .ok:
            if let lastSuccess = store.lastSuccess {
                return "OK \(age(from: lastSuccess))"
            }
            return "OK"
        case .degraded(let message):
            if let lastSuccess = store.lastSuccess {
                return "\(message) \(age(from: lastSuccess))"
            }
            return message
        case .linkingNeeded:
            return "Linking needed"
        case .unknown:
            return "Unknown"
        }
    }

    private var statusColor: Color {
        if store.isRefreshing {
            return .yellow
        }
        return store.state.tint
    }
}

// MARK: - HeartbeatStatusView

struct HeartbeatStatusView: View {
    let lastEvent: ControlHeartbeatEvent?

    var body: some View {
        StatusIndicatorView(label: statusLabel, color: statusColor)
    }

    private var statusLabel: String {
        guard let event = lastEvent else {
            return "No heartbeat"
        }

        let timestamp = Date(timeIntervalSince1970: event.ts)
        let ageText = age(from: timestamp)

        switch event.status {
        case "sent":
            return "sent \(ageText)"
        case "ok":
            return "ok \(ageText)"
        case "skipped":
            return "skipped \(ageText)"
        case "failed":
            return "failed \(ageText)"
        default:
            return "\(event.status) \(ageText)"
        }
    }

    private var statusColor: Color {
        guard let event = lastEvent else {
            return .gray
        }

        switch event.status {
        case "ok":
            return .green
        case "sent":
            return .blue
        case "skipped":
            return .yellow
        case "failed":
            return .red
        default:
            return .gray
        }
    }
}
