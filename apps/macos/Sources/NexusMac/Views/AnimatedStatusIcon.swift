import AppKit
import SwiftUI

/// Animated status icon for the menu bar.
/// Shows different states with subtle animations.
struct AnimatedStatusIcon: View {
    @Bindable var appState: AppStateStore
    @State private var pulseScale: CGFloat = 1.0
    @State private var rotationAngle: Double = 0
    @State private var glowOpacity: Double = 0

    var body: some View {
        ZStack {
            // Glow effect for active states
            if showGlow {
                Circle()
                    .fill(glowColor.opacity(glowOpacity * 0.3))
                    .frame(width: 24, height: 24)
                    .blur(radius: 4)
            }

            // Main icon
            iconView
                .scaleEffect(pulseScale)
        }
        .frame(width: 18, height: 18)
        .onAppear {
            startAnimations()
        }
        .onChange(of: appState.isPaused) { _, _ in
            startAnimations()
        }
    }

    @ViewBuilder
    private var iconView: some View {
        if appState.isPaused {
            // Paused state - static icon
            Image(systemName: "pause.circle.fill")
                .font(.system(size: 16))
                .foregroundStyle(.secondary)
        } else if isWorking {
            // Working state - rotating sparkle
            Image(systemName: "sparkle")
                .font(.system(size: 14, weight: .medium))
                .foregroundStyle(statusColor)
                .rotationEffect(.degrees(rotationAngle))
        } else if isListening {
            // Voice active - pulsing mic
            Image(systemName: "waveform.circle.fill")
                .font(.system(size: 16))
                .foregroundStyle(statusColor)
                .symbolEffect(.pulse, isActive: true)
        } else {
            // Default state - hexagon icon
            Image(systemName: "circle.hexagongrid.fill")
                .font(.system(size: 16))
                .foregroundStyle(statusColor)
        }
    }

    // MARK: - State

    private var isWorking: Bool {
        // Check if any session is actively processing
        SessionBridge.shared.activeSessions.contains { session in
            session.status == .processing
        }
    }

    private var isListening: Bool {
        appState.voiceWakeEnabled && VoiceWakeRuntime.shared.isListening
    }

    private var showGlow: Bool {
        isWorking || isListening
    }

    private var statusColor: Color {
        switch appState.connectionMode {
        case .local, .remote:
            return .accentColor
        case .unconfigured:
            return .orange
        }
    }

    private var glowColor: Color {
        if isListening {
            return .green
        } else if isWorking {
            return .accentColor
        } else {
            return .clear
        }
    }

    // MARK: - Animations

    private func startAnimations() {
        guard !appState.isPaused else {
            pulseScale = 1.0
            rotationAngle = 0
            glowOpacity = 0
            return
        }

        // Pulse animation
        withAnimation(.easeInOut(duration: 1.5).repeatForever(autoreverses: true)) {
            pulseScale = 1.05
        }

        // Rotation for working state
        if isWorking {
            withAnimation(.linear(duration: 3).repeatForever(autoreverses: false)) {
                rotationAngle = 360
            }
        }

        // Glow animation
        if showGlow {
            withAnimation(.easeInOut(duration: 1.2).repeatForever(autoreverses: true)) {
                glowOpacity = 1.0
            }
        }
    }
}

// MARK: - Icon State

enum IconState {
    case idle
    case working
    case listening
    case paused
    case error
    case unconfigured

    enum BadgeProminence {
        case primary
        case secondary
        case overridden
    }

    var badge: (symbol: String, prominence: BadgeProminence)? {
        switch self {
        case .working:
            return ("sparkle", .primary)
        case .listening:
            return ("mic.fill", .primary)
        case .error:
            return ("exclamationmark.triangle.fill", .primary)
        default:
            return nil
        }
    }
}

// MARK: - Static Icon Renderer

enum StatusIconRenderer {
    private static let size = NSSize(width: 18, height: 18)

    /// Render a static icon for menu bar
    static func makeIcon(state: IconState) -> NSImage {
        let symbolName: String
        switch state {
        case .idle:
            symbolName = "circle.hexagongrid.fill"
        case .working:
            symbolName = "sparkle"
        case .listening:
            symbolName = "waveform.circle.fill"
        case .paused:
            symbolName = "pause.circle.fill"
        case .error:
            symbolName = "exclamationmark.circle.fill"
        case .unconfigured:
            symbolName = "circle.hexagongrid"
        }

        let image = NSImage(systemSymbolName: symbolName, accessibilityDescription: nil) ?? NSImage()
        image.isTemplate = true
        return image
    }

    /// Render icon with badge
    static func makeIcon(state: IconState, withBadge badge: (symbol: String, prominence: IconState.BadgeProminence)?) -> NSImage {
        let baseIcon = makeIcon(state: state)

        guard let badge else {
            return baseIcon
        }

        // Create composite image with badge
        let composite = NSImage(size: size)
        composite.lockFocus()

        // Draw base icon
        baseIcon.draw(
            in: NSRect(x: 0, y: 0, width: size.width, height: size.height),
            from: .zero,
            operation: .sourceOver,
            fraction: 1.0
        )

        // Draw badge in bottom-right corner
        if let badgeImage = NSImage(systemSymbolName: badge.symbol, accessibilityDescription: nil) {
            let badgeSize: CGFloat = 8
            let badgeRect = NSRect(
                x: size.width - badgeSize - 1,
                y: 1,
                width: badgeSize,
                height: badgeSize
            )

            // Clear area for badge
            NSColor.clear.setFill()
            badgeRect.insetBy(dx: -1, dy: -1).fill()

            badgeImage.draw(
                in: badgeRect,
                from: .zero,
                operation: .sourceOver,
                fraction: badge.prominence == .primary ? 1.0 : 0.6
            )
        }

        composite.unlockFocus()
        composite.isTemplate = true
        return composite
    }
}

#Preview {
    HStack(spacing: 20) {
        AnimatedStatusIcon(appState: AppStateStore.shared)
    }
    .padding()
}
