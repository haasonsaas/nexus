import SwiftUI

// MARK: - NodeServiceView

/// A SwiftUI view for displaying and controlling the node service status.
struct NodeServiceView: View {
    @StateObject private var manager = NodeServiceManager()

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            // Header
            Text("Node Service")
                .font(.title2)
                .fontWeight(.semibold)

            // Status Section
            HStack(spacing: 12) {
                NodeServiceStatusIndicator(status: manager.currentStatus)

                Spacer()

                Button {
                    Task { await manager.refreshStatus() }
                } label: {
                    Label("Refresh", systemImage: "arrow.clockwise")
                }
                .disabled(manager.isLoading)
            }

            Divider()

            // Control Buttons
            HStack(spacing: 12) {
                Button {
                    Task { await manager.start() }
                } label: {
                    Label("Start", systemImage: "play.fill")
                }
                .disabled(manager.isLoading || manager.currentStatus == .running)

                Button {
                    Task { await manager.stop() }
                } label: {
                    Label("Stop", systemImage: "stop.fill")
                }
                .disabled(manager.isLoading || manager.currentStatus == .stopped || manager.currentStatus == .notLoaded)
            }
            .buttonStyle(.bordered)

            // Loading Indicator
            if manager.isLoading {
                HStack(spacing: 8) {
                    ProgressView()
                        .controlSize(.small)
                    Text("Processing...")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }

            // Error Message
            if let error = manager.lastError {
                GroupBox {
                    HStack(alignment: .top, spacing: 8) {
                        Image(systemName: "exclamationmark.triangle.fill")
                            .foregroundStyle(.yellow)

                        Text(error)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)

                        Spacer()

                        Button {
                            manager.clearError()
                        } label: {
                            Image(systemName: "xmark.circle.fill")
                                .foregroundStyle(.secondary)
                        }
                        .buttonStyle(.plain)
                    }
                }
            }

            Spacer()
        }
        .padding()
        .task {
            await manager.refreshStatus()
        }
    }
}

// MARK: - NodeServiceStatusIndicator

/// A visual indicator showing the current node service status.
struct NodeServiceStatusIndicator: View {
    let status: NodeServiceStatus

    var body: some View {
        HStack(spacing: 8) {
            Circle()
                .fill(statusColor)
                .frame(width: 10, height: 10)
                .overlay {
                    if isPulsing {
                        Circle()
                            .stroke(statusColor.opacity(0.5), lineWidth: 2)
                            .scaleEffect(1.5)
                    }
                }

            VStack(alignment: .leading, spacing: 2) {
                Text("Status")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)

                Text(status.displayName)
                    .font(.callout)
                    .fontWeight(.medium)
                    .foregroundStyle(textColor)
            }
        }
    }

    private var statusColor: Color {
        switch status {
        case .running:
            return .green
        case .stopped:
            return .gray
        case .notLoaded:
            return .orange
        case .error:
            return .red
        }
    }

    private var textColor: Color {
        switch status {
        case .running:
            return .primary
        case .stopped:
            return .secondary
        case .notLoaded:
            return .orange
        case .error:
            return .red
        }
    }

    private var isPulsing: Bool {
        status == .running
    }
}

// MARK: - Preview

#if DEBUG
struct NodeServiceView_Previews: PreviewProvider {
    static var previews: some View {
        NodeServiceView()
            .frame(width: 400, height: 300)
    }
}
#endif
