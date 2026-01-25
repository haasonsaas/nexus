import SwiftUI

// MARK: - Context Card Container View

/// Container view that displays all context cards in the menu bar.
struct ContextMenuCardsView: View {
    @State private var cardManager = ContextCardManager.shared
    @State private var expandedCardId: String?

    var body: some View {
        VStack(spacing: 6) {
            ForEach(cardManager.cards, id: \.id) { card in
                ContextMenuCardView(
                    card: card,
                    isExpanded: expandedCardId == card.id,
                    onToggleExpand: {
                        withAnimation(.spring(response: 0.3, dampingFraction: 0.8)) {
                            expandedCardId = expandedCardId == card.id ? nil : card.id
                        }
                    }
                )
                .transition(.asymmetric(
                    insertion: .opacity.combined(with: .scale(scale: 0.95)),
                    removal: .opacity
                ))
            }
        }
        .animation(.spring(response: 0.3, dampingFraction: 0.8), value: cardManager.cards.map(\.id))
    }
}

// MARK: - Generic Card View

/// Generic view for displaying any context card with consistent styling.
struct ContextMenuCardView: View {
    let card: AnyContextCard
    let isExpanded: Bool
    let onToggleExpand: () -> Void

    @State private var isHovered = false
    @State private var hoveredActionId: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Main card content
            mainContent

            // Expanded actions
            if isExpanded && !card.actions.isEmpty {
                expandedActions
            }
        }
        .background(cardBackground)
        .overlay(cardBorder)
        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
        .shadow(color: .black.opacity(isHovered ? 0.12 : 0.06), radius: isHovered ? 8 : 4, y: 2)
        .scaleEffect(isHovered ? 1.01 : 1.0)
        .animation(.spring(response: 0.25, dampingFraction: 0.8), value: isHovered)
        .animation(.spring(response: 0.3, dampingFraction: 0.8), value: isExpanded)
        .onHover { hovering in
            isHovered = hovering
        }
    }

    // MARK: - Main Content

    private var mainContent: some View {
        Button(action: onToggleExpand) {
            HStack(spacing: 12) {
                // Icon with priority indicator
                iconView

                // Title and subtitle
                VStack(alignment: .leading, spacing: 2) {
                    Text(card.title)
                        .font(.system(size: 13, weight: .medium))
                        .foregroundStyle(.primary)
                        .lineLimit(1)

                    if let subtitle = card.subtitle {
                        Text(subtitle)
                            .font(.system(size: 11))
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                }

                Spacer()

                // Quick action or expand indicator
                if isHovered && !card.actions.isEmpty {
                    Image(systemName: isExpanded ? "chevron.up" : "chevron.down")
                        .font(.system(size: 10, weight: .semibold))
                        .foregroundStyle(.tertiary)
                        .transition(.opacity)
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    // MARK: - Icon View

    private var iconView: some View {
        ZStack {
            Circle()
                .fill(iconBackgroundColor.opacity(0.15))
                .frame(width: 32, height: 32)

            Image(systemName: card.icon)
                .font(.system(size: 14))
                .foregroundStyle(iconBackgroundColor)
                .symbolEffect(.pulse, isActive: card.priority == .critical)
        }
    }

    private var iconBackgroundColor: Color {
        switch card.priority {
        case .critical: return .red
        case .high: return .orange
        case .medium: return .blue
        case .low: return .secondary
        }
    }

    // MARK: - Expanded Actions

    private var expandedActions: some View {
        VStack(spacing: 0) {
            Divider()
                .padding(.horizontal, 12)

            HStack(spacing: 8) {
                ForEach(card.actions) { action in
                    CardActionButton(
                        action: action,
                        isHovered: hoveredActionId == action.id,
                        onHover: { hovering in
                            hoveredActionId = hovering ? action.id : nil
                        }
                    )
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
        }
        .transition(.opacity.combined(with: .move(edge: .top)))
    }

    // MARK: - Styling

    private var cardBackground: some View {
        RoundedRectangle(cornerRadius: 10, style: .continuous)
            .fill(Color(NSColor.controlBackgroundColor))
    }

    private var cardBorder: some View {
        RoundedRectangle(cornerRadius: 10, style: .continuous)
            .strokeBorder(
                isHovered ? Color.accentColor.opacity(0.3) : Color.gray.opacity(0.15),
                lineWidth: 1
            )
    }
}

// MARK: - Card Action Button

struct CardActionButton: View {
    let action: CardAction
    let isHovered: Bool
    let onHover: (Bool) -> Void

    var body: some View {
        Button {
            action.handler()
        } label: {
            HStack(spacing: 6) {
                if let icon = action.icon {
                    Image(systemName: icon)
                        .font(.system(size: 11))
                }

                Text(action.title)
                    .font(.system(size: 11, weight: .medium))

                if let shortcut = action.shortcut {
                    Text(shortcut)
                        .font(.system(size: 9))
                        .foregroundStyle(.tertiary)
                        .padding(.horizontal, 4)
                        .padding(.vertical, 2)
                        .background(
                            RoundedRectangle(cornerRadius: 3, style: .continuous)
                                .fill(Color.gray.opacity(0.15))
                        )
                }
            }
            .foregroundStyle(action.isDestructive ? .red : .primary)
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .background(
                RoundedRectangle(cornerRadius: 6, style: .continuous)
                    .fill(isHovered ? (action.isDestructive ? Color.red.opacity(0.1) : Color.accentColor.opacity(0.1)) : Color.clear)
            )
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering in
            onHover(hovering)
        }
    }
}

// MARK: - Specialized Card Views

/// View for active session cards with status indicator.
struct ActiveSessionCardView: View {
    let card: ActiveSessionCard
    @State private var isHovered = false

    var body: some View {
        HStack(spacing: 12) {
            // Session type icon with status ring
            ZStack {
                Circle()
                    .stroke(statusColor.opacity(0.3), lineWidth: 2)
                    .frame(width: 36, height: 36)

                Circle()
                    .fill(statusColor.opacity(0.15))
                    .frame(width: 32, height: 32)

                Image(systemName: card.icon)
                    .font(.system(size: 14))
                    .foregroundStyle(statusColor)
            }
            .overlay(alignment: .bottomTrailing) {
                if card.status == .processing {
                    ProgressView()
                        .controlSize(.mini)
                        .offset(x: 4, y: 4)
                }
            }

            VStack(alignment: .leading, spacing: 2) {
                Text(card.agentName)
                    .font(.system(size: 13, weight: .medium))

                HStack(spacing: 6) {
                    Image(systemName: card.status.icon)
                        .font(.system(size: 9))
                        .foregroundStyle(statusColor)

                    Text(card.subtitle ?? "")
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                }
            }

            Spacer()

            if isHovered {
                Button {
                    card.actions.first?.handler()
                } label: {
                    Image(systemName: "arrow.up.right.square")
                        .font(.system(size: 12))
                        .foregroundStyle(.secondary)
                }
                .buttonStyle(.plain)
                .transition(.opacity)
            }
        }
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .fill(Color(NSColor.controlBackgroundColor))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .strokeBorder(statusColor.opacity(isHovered ? 0.4 : 0.2), lineWidth: 1)
        )
        .onHover { hovering in
            withAnimation(.easeOut(duration: 0.15)) {
                isHovered = hovering
            }
        }
    }

    private var statusColor: Color {
        switch card.status {
        case .processing: return .blue
        case .waiting: return .orange
        case .idle: return .green
        case .error: return .red
        }
    }
}

/// View for system status card with compact indicators.
struct SystemStatusCardView: View {
    let card: SystemStatusCard
    @State private var isHovered = false

    var body: some View {
        HStack(spacing: 12) {
            // Status icon
            ZStack {
                Circle()
                    .fill(statusColor.opacity(0.15))
                    .frame(width: 32, height: 32)

                Image(systemName: card.icon)
                    .font(.system(size: 14))
                    .foregroundStyle(statusColor)
            }

            VStack(alignment: .leading, spacing: 4) {
                Text(card.title)
                    .font(.system(size: 13, weight: .medium))

                HStack(spacing: 12) {
                    StatusIndicator(
                        icon: card.isGatewayConnected ? "wifi" : "wifi.slash",
                        color: card.isGatewayConnected ? .green : .red,
                        label: card.isGatewayConnected ? "Connected" : "Offline"
                    )

                    if let memory = card.memoryUsageMB {
                        StatusIndicator(
                            icon: "memorychip",
                            color: .secondary,
                            label: "\(memory) MB"
                        )
                    }

                    if card.pendingApprovals > 0 {
                        StatusIndicator(
                            icon: "exclamationmark.circle",
                            color: .orange,
                            label: "\(card.pendingApprovals)"
                        )
                    }
                }
            }

            Spacer()
        }
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .fill(Color(NSColor.controlBackgroundColor))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .strokeBorder(Color.gray.opacity(0.15), lineWidth: 1)
        )
    }

    private var statusColor: Color {
        if card.pendingApprovals > 0 { return .orange }
        return card.isGatewayConnected ? .green : .red
    }
}

/// Compact status indicator for system status card.
struct StatusIndicator: View {
    let icon: String
    let color: Color
    let label: String

    var body: some View {
        HStack(spacing: 4) {
            Image(systemName: icon)
                .font(.system(size: 9))
                .foregroundStyle(color)

            Text(label)
                .font(.system(size: 10))
                .foregroundStyle(.secondary)
        }
    }
}

/// View for cost summary card with trend indicator.
struct CostSummaryCardView: View {
    let card: CostSummaryCard
    @State private var isHovered = false

    var body: some View {
        HStack(spacing: 12) {
            // Cost icon with trend
            ZStack {
                Circle()
                    .fill(Color.green.opacity(0.15))
                    .frame(width: 32, height: 32)

                Image(systemName: card.icon)
                    .font(.system(size: 14))
                    .foregroundStyle(.green)
            }

            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(formattedCost(card.todayCost))
                        .font(.system(size: 15, weight: .semibold, design: .rounded))

                    trendBadge
                }

                Text(card.subtitle ?? "")
                    .font(.system(size: 10))
                    .foregroundStyle(.secondary)
            }

            Spacer()
        }
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .fill(Color(NSColor.controlBackgroundColor))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .strokeBorder(Color.gray.opacity(0.15), lineWidth: 1)
        )
    }

    @ViewBuilder
    private var trendBadge: some View {
        switch card.trend {
        case .up(let pct):
            HStack(spacing: 2) {
                Image(systemName: "arrow.up.right")
                    .font(.system(size: 8, weight: .bold))
                Text("+\(Int(pct))%")
                    .font(.system(size: 9, weight: .medium))
            }
            .foregroundStyle(.red)
            .padding(.horizontal, 5)
            .padding(.vertical, 2)
            .background(
                Capsule().fill(Color.red.opacity(0.1))
            )

        case .down(let pct):
            HStack(spacing: 2) {
                Image(systemName: "arrow.down.right")
                    .font(.system(size: 8, weight: .bold))
                Text("-\(Int(pct))%")
                    .font(.system(size: 9, weight: .medium))
            }
            .foregroundStyle(.green)
            .padding(.horizontal, 5)
            .padding(.vertical, 2)
            .background(
                Capsule().fill(Color.green.opacity(0.1))
            )

        case .stable, .unknown:
            EmptyView()
        }
    }

    private func formattedCost(_ amount: Double) -> String {
        let formatter = NumberFormatter()
        formatter.numberStyle = .currency
        formatter.currencyCode = "USD"
        formatter.maximumFractionDigits = 2
        return formatter.string(from: NSNumber(value: amount)) ?? "$0.00"
    }
}

/// View for pending approval cards with urgent styling.
struct PendingApprovalCardView: View {
    let card: PendingApprovalCard
    @State private var isHovered = false
    @State private var pulseAnimation = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 10) {
                // Alert icon with pulse
                ZStack {
                    Circle()
                        .fill(Color.orange.opacity(pulseAnimation ? 0.3 : 0.15))
                        .frame(width: 36, height: 36)
                        .animation(.easeInOut(duration: 1).repeatForever(autoreverses: true), value: pulseAnimation)

                    Image(systemName: card.icon)
                        .font(.system(size: 16))
                        .foregroundStyle(.orange)
                }

                VStack(alignment: .leading, spacing: 2) {
                    Text(card.title)
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(.orange)

                    Text(card.subtitle ?? "")
                        .font(.system(size: 11, design: .monospaced))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }

                Spacer()
            }

            // Action buttons
            HStack(spacing: 8) {
                ForEach(card.actions) { action in
                    Button {
                        action.handler()
                    } label: {
                        HStack(spacing: 4) {
                            if let icon = action.icon {
                                Image(systemName: icon)
                                    .font(.system(size: 11))
                            }
                            Text(action.title)
                                .font(.system(size: 11, weight: .medium))

                            if let shortcut = action.shortcut {
                                Text(shortcut)
                                    .font(.system(size: 9))
                                    .padding(.horizontal, 4)
                                    .padding(.vertical, 1)
                                    .background(
                                        RoundedRectangle(cornerRadius: 3)
                                            .fill(Color.white.opacity(0.2))
                                    )
                            }
                        }
                        .foregroundStyle(action.isDestructive ? .white : .primary)
                        .padding(.horizontal, 12)
                        .padding(.vertical, 6)
                        .background(
                            RoundedRectangle(cornerRadius: 6, style: .continuous)
                                .fill(action.isDestructive ? Color.red : Color.green)
                        )
                    }
                    .buttonStyle(.plain)
                }
            }
        }
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .fill(Color(NSColor.controlBackgroundColor))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .strokeBorder(Color.orange.opacity(0.5), lineWidth: 2)
        )
        .onAppear {
            pulseAnimation = true
        }
    }
}

// MARK: - Quick Action Card View

struct QuickActionCardView: View {
    let card: QuickActionCard
    @State private var isHovered = false

    var body: some View {
        Button {
            card.actions.first?.handler()
        } label: {
            HStack(spacing: 12) {
                ZStack {
                    Circle()
                        .fill(Color.accentColor.opacity(0.15))
                        .frame(width: 32, height: 32)

                    Image(systemName: card.icon)
                        .font(.system(size: 14))
                        .foregroundStyle(.accentColor)
                }

                VStack(alignment: .leading, spacing: 2) {
                    Text(card.title)
                        .font(.system(size: 13, weight: .medium))

                    if let subtitle = card.subtitle {
                        Text(subtitle)
                            .font(.system(size: 10))
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                }

                Spacer()

                if let shortcut = card.action.hotkey {
                    Text(shortcut)
                        .font(.system(size: 10, weight: .medium))
                        .foregroundStyle(.tertiary)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 3)
                        .background(
                            RoundedRectangle(cornerRadius: 4, style: .continuous)
                                .fill(Color.gray.opacity(0.15))
                        )
                }
            }
            .padding(12)
            .background(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .fill(Color(NSColor.controlBackgroundColor))
            )
            .overlay(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .strokeBorder(
                        isHovered ? Color.accentColor.opacity(0.4) : Color.gray.opacity(0.15),
                        lineWidth: 1
                    )
            )
            .scaleEffect(isHovered ? 1.02 : 1.0)
            .animation(.spring(response: 0.25, dampingFraction: 0.8), value: isHovered)
        }
        .buttonStyle(.plain)
        .onHover { hovering in
            isHovered = hovering
        }
    }
}

// MARK: - Recent Conversation Card View

struct RecentConversationCardView: View {
    let card: RecentConversationCard
    @State private var isHovered = false

    var body: some View {
        Button {
            card.actions.first?.handler()
        } label: {
            HStack(spacing: 12) {
                ZStack {
                    Circle()
                        .fill(Color.purple.opacity(0.15))
                        .frame(width: 32, height: 32)

                    Image(systemName: card.icon)
                        .font(.system(size: 14))
                        .foregroundStyle(.purple)
                }

                VStack(alignment: .leading, spacing: 2) {
                    Text(card.title)
                        .font(.system(size: 13, weight: .medium))
                        .lineLimit(1)

                    if let subtitle = card.subtitle {
                        Text(subtitle)
                            .font(.system(size: 10))
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                }

                Spacer()

                Image(systemName: "arrow.right.circle")
                    .font(.system(size: 12))
                    .foregroundStyle(.tertiary)
                    .opacity(isHovered ? 1 : 0)
            }
            .padding(12)
            .background(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .fill(Color(NSColor.controlBackgroundColor))
            )
            .overlay(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .strokeBorder(
                        isHovered ? Color.purple.opacity(0.3) : Color.gray.opacity(0.15),
                        lineWidth: 1
                    )
            )
        }
        .buttonStyle(.plain)
        .onHover { hovering in
            withAnimation(.easeOut(duration: 0.15)) {
                isHovered = hovering
            }
        }
    }
}

// MARK: - Previews

#Preview("Context Cards") {
    ContextMenuCardsView()
        .frame(width: 320)
        .padding()
        .background(Color.gray.opacity(0.1))
}

#Preview("Active Session Card") {
    ActiveSessionCardView(card: ActiveSessionCard(
        id: "1",
        sessionId: "session_1",
        agentName: "Code Assistant",
        sessionType: .agent,
        status: .processing,
        startedAt: Date().addingTimeInterval(-300),
        messageCount: 5
    ))
    .frame(width: 300)
    .padding()
}

#Preview("System Status Card") {
    SystemStatusCardView(card: SystemStatusCard(
        id: "status",
        isGatewayConnected: true,
        memoryUsageMB: 256,
        pendingApprovals: 2,
        activeAgents: 1
    ))
    .frame(width: 300)
    .padding()
}

#Preview("Cost Summary Card") {
    CostSummaryCardView(card: CostSummaryCard(
        id: "cost",
        todayCost: 12.50,
        yesterdayCost: 10.00,
        weekTotal: 85.25,
        trend: .up(25)
    ))
    .frame(width: 300)
    .padding()
}
