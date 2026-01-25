import SwiftUI

/// Status item label shown in the menu bar.
/// Displays connection status and activity indicators.
struct StatusItemLabel: View {
    @Bindable var appState: AppStateStore

    @State private var isAnimating = false

    var body: some View {
        HStack(spacing: 2) {
            statusIcon
                .symbolRenderingMode(.hierarchical)
                .foregroundStyle(statusColor)
        }
        .animation(.easeInOut(duration: 0.3), value: appState.isPaused)
    }

    @ViewBuilder
    private var statusIcon: some View {
        if appState.isPaused {
            Image(systemName: "pause.circle.fill")
        } else if isAnimating {
            Image(systemName: "sparkle")
                .symbolEffect(.pulse)
        } else {
            Image(systemName: "circle.hexagongrid.fill")
        }
    }

    private var statusColor: Color {
        if appState.isPaused {
            return .secondary
        }

        switch appState.connectionMode {
        case .local, .remote:
            return .accentColor
        case .unconfigured:
            return .orange
        }
    }
}

#Preview {
    StatusItemLabel(appState: AppStateStore.shared)
}
