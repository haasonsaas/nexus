import SwiftUI

// MARK: - Loading State View

/// Displays a loading state with optional message and skeleton shimmer effect.
struct LoadingStateView: View {
    let message: String
    var showSkeleton: Bool = false

    var body: some View {
        VStack(spacing: 16) {
            if showSkeleton {
                SkeletonView()
            } else {
                ProgressView()
                    .controlSize(.large)
            }

            Text(message)
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .transition(.opacity.combined(with: .scale(scale: 0.95)))
    }
}

// MARK: - Empty State View

/// Displays an empty state with icon, title, description, and optional action.
struct EmptyStateView: View {
    let icon: String
    let title: String
    let description: String
    var actionTitle: String?
    var action: (() -> Void)?

    var body: some View {
        VStack(spacing: 16) {
            Image(systemName: icon)
                .font(.system(size: 48))
                .foregroundStyle(.tertiary)
                .symbolEffect(.pulse, options: .repeating.speed(0.5))

            VStack(spacing: 8) {
                Text(title)
                    .font(.headline)

                Text(description)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                    .frame(maxWidth: 300)
            }

            if let actionTitle, let action {
                Button(action: action) {
                    Text(actionTitle)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.regular)
            }
        }
        .padding(32)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .transition(.opacity.combined(with: .scale(scale: 0.95)))
    }
}

// MARK: - Error State View

/// Displays an error state with icon, message, and retry action.
struct ErrorStateView: View {
    let message: String
    var details: String?
    var severity: ErrorSeverity = .error
    var onRetry: (() -> Void)?
    var onDismiss: (() -> Void)?

    enum ErrorSeverity {
        case warning
        case error
        case network

        var icon: String {
            switch self {
            case .warning: return "exclamationmark.triangle.fill"
            case .error: return "xmark.circle.fill"
            case .network: return "wifi.slash"
            }
        }

        var color: Color {
            switch self {
            case .warning: return .orange
            case .error: return .red
            case .network: return .blue
            }
        }
    }

    var body: some View {
        VStack(spacing: 16) {
            Image(systemName: severity.icon)
                .font(.system(size: 40))
                .foregroundStyle(severity.color)

            VStack(spacing: 8) {
                Text(message)
                    .font(.headline)

                if let details {
                    Text(details)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                        .frame(maxWidth: 300)
                }
            }

            HStack(spacing: 12) {
                if let onDismiss {
                    Button("Dismiss", action: onDismiss)
                        .buttonStyle(.bordered)
                }

                if let onRetry {
                    Button("Try Again", action: onRetry)
                        .buttonStyle(.borderedProminent)
                }
            }
        }
        .padding(24)
        .background(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .fill(Color(NSColor.controlBackgroundColor))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .strokeBorder(severity.color.opacity(0.3), lineWidth: 1)
        )
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .transition(.opacity.combined(with: .scale(scale: 0.9)))
    }
}

// MARK: - Inline Error Banner

/// Displays an inline error banner at the top of a view.
struct ErrorBanner: View {
    let message: String
    var severity: ErrorStateView.ErrorSeverity = .error
    var onDismiss: (() -> Void)?

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: severity.icon)
                .foregroundStyle(severity.color)

            Text(message)
                .font(.subheadline)
                .lineLimit(2)

            Spacer()

            if let onDismiss {
                Button {
                    withAnimation(.easeOut(duration: 0.2)) {
                        onDismiss()
                    }
                } label: {
                    Image(systemName: "xmark")
                        .font(.caption.weight(.semibold))
                }
                .buttonStyle(.plain)
                .foregroundStyle(.secondary)
            }
        }
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .fill(severity.color.opacity(0.1))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .strokeBorder(severity.color.opacity(0.3), lineWidth: 1)
        )
        .transition(.move(edge: .top).combined(with: .opacity))
    }
}

// MARK: - Skeleton View

/// Animated skeleton placeholder for loading content.
struct SkeletonView: View {
    @State private var isAnimating = false

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            ForEach(0..<3, id: \.self) { index in
                SkeletonRow(width: index == 2 ? 0.6 : 1.0)
            }
        }
        .padding()
        .onAppear {
            withAnimation(.easeInOut(duration: 1.2).repeatForever(autoreverses: true)) {
                isAnimating = true
            }
        }
    }
}

struct SkeletonRow: View {
    let width: CGFloat
    @State private var isAnimating = false

    var body: some View {
        HStack(spacing: 12) {
            Circle()
                .fill(shimmerGradient)
                .frame(width: 32, height: 32)

            VStack(alignment: .leading, spacing: 6) {
                RoundedRectangle(cornerRadius: 4)
                    .fill(shimmerGradient)
                    .frame(height: 12)
                    .frame(maxWidth: .infinity)

                RoundedRectangle(cornerRadius: 4)
                    .fill(shimmerGradient)
                    .frame(height: 10)
                    .frame(width: 120 * width)
            }
        }
        .onAppear {
            withAnimation(.easeInOut(duration: 1.2).repeatForever(autoreverses: true)) {
                isAnimating = true
            }
        }
    }

    private var shimmerGradient: LinearGradient {
        LinearGradient(
            colors: [
                Color.secondary.opacity(isAnimating ? 0.15 : 0.25),
                Color.secondary.opacity(isAnimating ? 0.25 : 0.15),
            ],
            startPoint: .leading,
            endPoint: .trailing
        )
    }
}

// MARK: - Status Badge

/// Unified status badge component with multiple variants.
struct StatusBadge: View {
    let status: Status
    var variant: Variant = .minimal

    enum Status {
        case online
        case offline
        case connecting
        case warning
        case error

        var color: Color {
            switch self {
            case .online: return .green
            case .offline: return .secondary
            case .connecting: return .blue
            case .warning: return .orange
            case .error: return .red
            }
        }

        var label: String {
            switch self {
            case .online: return "Online"
            case .offline: return "Offline"
            case .connecting: return "Connecting"
            case .warning: return "Warning"
            case .error: return "Error"
            }
        }
    }

    enum Variant {
        case minimal      // Just colored circle
        case badge        // Circle + text label
        case animated     // Circle + pulsing animation
        case detailed     // Icon + label + background
    }

    var body: some View {
        switch variant {
        case .minimal:
            minimalView
        case .badge:
            badgeView
        case .animated:
            animatedView
        case .detailed:
            detailedView
        }
    }

    private var minimalView: some View {
        Circle()
            .fill(status.color)
            .frame(width: 8, height: 8)
    }

    @ViewBuilder
    private var badgeView: some View {
        HStack(spacing: 6) {
            Circle()
                .fill(status.color)
                .frame(width: 6, height: 6)
            Text(status.label)
                .font(.caption)
        }
    }

    @ViewBuilder
    private var animatedView: some View {
        Circle()
            .fill(status.color)
            .frame(width: 8, height: 8)
            .overlay(
                Circle()
                    .stroke(status.color.opacity(0.5), lineWidth: 2)
                    .scaleEffect(status == .connecting || status == .online ? 1.5 : 1)
                    .opacity(status == .connecting || status == .online ? 0 : 1)
                    .animation(
                        status == .connecting
                            ? .easeOut(duration: 1).repeatForever(autoreverses: false)
                            : .default,
                        value: status
                    )
            )
    }

    @ViewBuilder
    private var detailedView: some View {
        HStack(spacing: 6) {
            Circle()
                .fill(status.color)
                .frame(width: 8, height: 8)
            Text(status.label)
                .font(.caption.weight(.medium))
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 4)
        .background(
            Capsule()
                .fill(status.color.opacity(0.15))
        )
    }
}

// MARK: - Loading Overlay

/// Full-screen loading overlay.
struct LoadingOverlay: View {
    let message: String
    var isPresented: Bool

    var body: some View {
        if isPresented {
            ZStack {
                Color.black.opacity(0.3)
                    .ignoresSafeArea()

                VStack(spacing: 16) {
                    ProgressView()
                        .controlSize(.large)
                    Text(message)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                .padding(24)
                .background(
                    RoundedRectangle(cornerRadius: 12, style: .continuous)
                        .fill(.ultraThinMaterial)
                )
            }
            .transition(.opacity)
        }
    }
}

// MARK: - Animated Transitions

extension AnyTransition {
    /// Smooth fade with scale for content changes
    static var smoothAppear: AnyTransition {
        .asymmetric(
            insertion: .opacity.combined(with: .scale(scale: 0.95)).animation(.spring(response: 0.3, dampingFraction: 0.8)),
            removal: .opacity.animation(.easeOut(duration: 0.15))
        )
    }

    /// Slide up from bottom
    static var slideUp: AnyTransition {
        .asymmetric(
            insertion: .move(edge: .bottom).combined(with: .opacity),
            removal: .opacity
        )
    }
}

// MARK: - Previews

#Preview("Loading State") {
    LoadingStateView(message: "Loading sessions...")
}

#Preview("Empty State") {
    EmptyStateView(
        icon: "tray",
        title: "No Sessions",
        description: "You don't have any active sessions yet. Start a new chat to begin.",
        actionTitle: "New Chat"
    ) {}
}

#Preview("Error State") {
    ErrorStateView(
        message: "Connection Failed",
        details: "Unable to connect to the gateway. Please check your network connection.",
        severity: .network,
        onRetry: {}
    )
}

#Preview("Status Badges") {
    HStack(spacing: 20) {
        StatusBadge(status: .online, variant: .minimal)
        StatusBadge(status: .connecting, variant: .animated)
        StatusBadge(status: .warning, variant: .badge)
        StatusBadge(status: .error, variant: .detailed)
    }
    .padding()
}
