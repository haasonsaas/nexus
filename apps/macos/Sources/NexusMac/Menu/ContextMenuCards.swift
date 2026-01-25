import Foundation
import OSLog

// MARK: - Context Card Protocol

/// Protocol defining the structure of a context menu card.
/// Each card represents a rich, information-dense UI element for the menu bar.
protocol ContextCard: Identifiable, Sendable {
    var id: String { get }
    var title: String { get }
    var subtitle: String? { get }
    var icon: String { get }
    var priority: CardPriority { get }
    var actions: [CardAction] { get }
}

/// Priority levels for card sorting in the menu.
enum CardPriority: Int, Comparable, Sendable {
    case critical = 100   // Pending approvals, errors
    case high = 80        // Active sessions, running agents
    case medium = 50      // Recent activity, quick actions
    case low = 20         // System status, informational

    static func < (lhs: CardPriority, rhs: CardPriority) -> Bool {
        lhs.rawValue < rhs.rawValue
    }
}

/// An action that can be performed from a card.
struct CardAction: Identifiable, Sendable {
    let id: String
    let title: String
    let icon: String?
    let shortcut: String?
    let isDestructive: Bool
    let handler: @MainActor @Sendable () -> Void

    init(
        id: String = UUID().uuidString,
        title: String,
        icon: String? = nil,
        shortcut: String? = nil,
        isDestructive: Bool = false,
        handler: @MainActor @escaping @Sendable () -> Void
    ) {
        self.id = id
        self.title = title
        self.icon = icon
        self.shortcut = shortcut
        self.isDestructive = isDestructive
        self.handler = handler
    }
}

// MARK: - Card Types

/// Card showing an active AI session with agent name, duration, and status.
struct ActiveSessionCard: ContextCard {
    let id: String
    let sessionId: String
    let agentName: String
    let sessionType: SessionBridge.Session.SessionType
    let status: SessionStatus
    let startedAt: Date
    let messageCount: Int

    enum SessionStatus: String, Sendable {
        case processing
        case waiting
        case idle
        case error

        var displayName: String {
            switch self {
            case .processing: return "Processing"
            case .waiting: return "Waiting for input"
            case .idle: return "Idle"
            case .error: return "Error"
            }
        }

        var icon: String {
            switch self {
            case .processing: return "arrow.triangle.2.circlepath"
            case .waiting: return "clock"
            case .idle: return "checkmark.circle"
            case .error: return "exclamationmark.triangle"
            }
        }
    }

    var title: String { agentName }

    var subtitle: String? {
        let duration = formatDuration(since: startedAt)
        return "\(status.displayName) - \(duration)"
    }

    var icon: String {
        switch sessionType {
        case .chat: return "message.fill"
        case .voice: return "mic.fill"
        case .agent: return "cpu.fill"
        case .computerUse: return "desktopcomputer"
        case .mcp: return "puzzlepiece.fill"
        }
    }

    var priority: CardPriority {
        switch status {
        case .error: return .critical
        case .processing, .waiting: return .high
        case .idle: return .medium
        }
    }

    var actions: [CardAction] {
        [
            CardAction(
                title: "Open",
                icon: "arrow.up.right.square",
                shortcut: nil
            ) {
                WebChatManager.shared.openChat(for: sessionId)
            },
            CardAction(
                title: "End Session",
                icon: "xmark.circle",
                isDestructive: true
            ) {
                SessionBridge.shared.endSession(id: sessionId, status: .completed)
            }
        ]
    }

    private func formatDuration(since date: Date) -> String {
        let interval = Date().timeIntervalSince(date)
        if interval < 60 {
            return "just now"
        } else if interval < 3600 {
            let minutes = Int(interval / 60)
            return "\(minutes)m"
        } else {
            let hours = Int(interval / 3600)
            let minutes = Int((interval.truncatingRemainder(dividingBy: 3600)) / 60)
            return "\(hours)h \(minutes)m"
        }
    }
}

/// Card for pinned quick actions with keyboard shortcuts.
struct QuickActionCard: ContextCard {
    let id: String
    let action: QuickActionManager.QuickAction

    var title: String { action.name }
    var subtitle: String? { action.description }
    var icon: String { action.icon ?? "bolt" }
    var priority: CardPriority { .medium }

    var actions: [CardAction] {
        [
            CardAction(
                title: "Run",
                icon: "play.fill",
                shortcut: action.hotkey
            ) {
                Task {
                    await QuickActionManager.shared.execute(action)
                }
            }
        ]
    }
}

/// Card showing a recent conversation with preview and resume action.
struct RecentConversationCard: ContextCard {
    let id: String
    let conversation: ConversationMemory.ConversationRecord

    var title: String { conversation.title ?? "Conversation" }

    var subtitle: String? {
        if let summary = conversation.summary {
            return String(summary.prefix(60))
        }
        if let lastMessage = conversation.messages.last {
            return String(lastMessage.content.prefix(60))
        }
        return formatRelativeDate(conversation.updatedAt)
    }

    var icon: String {
        conversation.metadata.starred ? "star.fill" : "message"
    }

    var priority: CardPriority { .low }

    var actions: [CardAction] {
        [
            CardAction(
                title: "Resume",
                icon: "arrow.right.circle",
                shortcut: nil
            ) {
                let session = SessionBridge.shared.createSession(type: .chat)
                WebChatManager.shared.openChat(for: session.id)
            },
            CardAction(
                title: conversation.metadata.starred ? "Unstar" : "Star",
                icon: conversation.metadata.starred ? "star.slash" : "star",
                shortcut: nil
            ) {
                ConversationMemory.shared.toggleStar(conversationId: conversation.id)
            }
        ]
    }

    private func formatRelativeDate(_ date: Date) -> String {
        let formatter = RelativeDateTimeFormatter()
        formatter.unitsStyle = .abbreviated
        return formatter.localizedString(for: date, relativeTo: Date())
    }
}

/// Card showing system status including gateway connection, memory usage, and pending approvals.
struct SystemStatusCard: ContextCard {
    let id: String
    let isGatewayConnected: Bool
    let memoryUsageMB: Int?
    let pendingApprovals: Int
    let activeAgents: Int

    var title: String { "System Status" }

    var subtitle: String? {
        var parts: [String] = []
        parts.append(isGatewayConnected ? "Connected" : "Disconnected")
        if let memory = memoryUsageMB {
            parts.append("\(memory) MB")
        }
        if pendingApprovals > 0 {
            parts.append("\(pendingApprovals) pending")
        }
        return parts.joined(separator: " - ")
    }

    var icon: String {
        if pendingApprovals > 0 {
            return "exclamationmark.circle.fill"
        }
        return isGatewayConnected ? "checkmark.shield.fill" : "shield.slash"
    }

    var priority: CardPriority {
        if pendingApprovals > 0 {
            return .critical
        }
        if !isGatewayConnected {
            return .high
        }
        return .low
    }

    var actions: [CardAction] {
        var cardActions: [CardAction] = []

        if !isGatewayConnected {
            cardActions.append(
                CardAction(
                    title: "Reconnect",
                    icon: "arrow.clockwise",
                    shortcut: nil
                ) {
                    Task {
                        try? await GatewayConnection.shared.refresh()
                    }
                }
            )
        }

        if pendingApprovals > 0 {
            cardActions.append(
                CardAction(
                    title: "View Approvals",
                    icon: "list.bullet.clipboard",
                    shortcut: nil
                ) {
                    NSApp.sendAction(Selector(("showSettingsWindow:")), to: nil, from: nil)
                }
            )
        }

        return cardActions
    }
}

/// Card showing today's cost and trend indicator.
struct CostSummaryCard: ContextCard {
    let id: String
    let todayCost: Double
    let yesterdayCost: Double?
    let weekTotal: Double
    let trend: CostTrend

    enum CostTrend: Sendable {
        case up(Double)     // Percentage increase
        case down(Double)   // Percentage decrease
        case stable
        case unknown

        var icon: String {
            switch self {
            case .up: return "arrow.up.right"
            case .down: return "arrow.down.right"
            case .stable: return "arrow.right"
            case .unknown: return "minus"
            }
        }
    }

    var title: String { "Cost Today" }

    var subtitle: String? {
        let formatter = NumberFormatter()
        formatter.numberStyle = .currency
        formatter.currencyCode = "USD"
        formatter.maximumFractionDigits = 2

        let todayFormatted = formatter.string(from: NSNumber(value: todayCost)) ?? "$0.00"
        let weekFormatted = formatter.string(from: NSNumber(value: weekTotal)) ?? "$0.00"

        return "\(todayFormatted) today - \(weekFormatted) this week"
    }

    var icon: String {
        switch trend {
        case .up(let pct) where pct > 50:
            return "chart.line.uptrend.xyaxis"
        case .down:
            return "chart.line.downtrend.xyaxis"
        default:
            return "dollarsign.circle"
        }
    }

    var priority: CardPriority { .low }

    var actions: [CardAction] {
        [
            CardAction(
                title: "View Details",
                icon: "chart.bar",
                shortcut: nil
            ) {
                NSApp.sendAction(Selector(("showSettingsWindow:")), to: nil, from: nil)
            }
        ]
    }
}

/// Card for pending command execution approvals requiring attention.
struct PendingApprovalCard: ContextCard {
    let id: String
    let approval: ExecApproval

    var title: String { "Approval Required" }

    var subtitle: String? {
        let command = approval.command
        if command.count > 50 {
            return String(command.prefix(47)) + "..."
        }
        return command
    }

    var icon: String { "exclamationmark.shield.fill" }
    var priority: CardPriority { .critical }

    var actions: [CardAction] {
        [
            CardAction(
                title: "Approve",
                icon: "checkmark.circle",
                shortcut: "A"
            ) {
                // Note: Need to access manager from appropriate context
                NotificationCenter.default.post(
                    name: .approveExecRequest,
                    object: nil,
                    userInfo: ["id": approval.id]
                )
            },
            CardAction(
                title: "Reject",
                icon: "xmark.circle",
                shortcut: "R",
                isDestructive: true
            ) {
                NotificationCenter.default.post(
                    name: .rejectExecRequest,
                    object: nil,
                    userInfo: ["id": approval.id]
                )
            }
        ]
    }
}

// MARK: - Notification Names

extension Notification.Name {
    static let approveExecRequest = Notification.Name("approveExecRequest")
    static let rejectExecRequest = Notification.Name("rejectExecRequest")
}

// MARK: - Type-Erased Card Wrapper

/// Type-erased wrapper for any ContextCard.
struct AnyContextCard: ContextCard {
    let id: String
    let title: String
    let subtitle: String?
    let icon: String
    let priority: CardPriority
    let actions: [CardAction]
    let cardType: CardType

    enum CardType: Sendable {
        case activeSession(ActiveSessionCard)
        case quickAction(QuickActionCard)
        case recentConversation(RecentConversationCard)
        case systemStatus(SystemStatusCard)
        case costSummary(CostSummaryCard)
        case pendingApproval(PendingApprovalCard)
    }

    init(_ card: ActiveSessionCard) {
        self.id = card.id
        self.title = card.title
        self.subtitle = card.subtitle
        self.icon = card.icon
        self.priority = card.priority
        self.actions = card.actions
        self.cardType = .activeSession(card)
    }

    init(_ card: QuickActionCard) {
        self.id = card.id
        self.title = card.title
        self.subtitle = card.subtitle
        self.icon = card.icon
        self.priority = card.priority
        self.actions = card.actions
        self.cardType = .quickAction(card)
    }

    init(_ card: RecentConversationCard) {
        self.id = card.id
        self.title = card.title
        self.subtitle = card.subtitle
        self.icon = card.icon
        self.priority = card.priority
        self.actions = card.actions
        self.cardType = .recentConversation(card)
    }

    init(_ card: SystemStatusCard) {
        self.id = card.id
        self.title = card.title
        self.subtitle = card.subtitle
        self.icon = card.icon
        self.priority = card.priority
        self.actions = card.actions
        self.cardType = .systemStatus(card)
    }

    init(_ card: CostSummaryCard) {
        self.id = card.id
        self.title = card.title
        self.subtitle = card.subtitle
        self.icon = card.icon
        self.priority = card.priority
        self.actions = card.actions
        self.cardType = .costSummary(card)
    }

    init(_ card: PendingApprovalCard) {
        self.id = card.id
        self.title = card.title
        self.subtitle = card.subtitle
        self.icon = card.icon
        self.priority = card.priority
        self.actions = card.actions
        self.cardType = .pendingApproval(card)
    }
}

// MARK: - Context Card Manager

/// Singleton manager that collects, sorts, and provides context cards for the menu bar.
@MainActor
@Observable
final class ContextCardManager {
    static let shared = ContextCardManager()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "contextcards")

    /// All available cards, sorted by priority
    private(set) var cards: [AnyContextCard] = []

    /// Maximum number of cards to display
    var maxCards: Int = 8

    /// Minimum number of cards to display
    var minCards: Int = 5

    private var updateTask: Task<Void, Never>?
    private var isRefreshing = false

    private init() {
        startObserving()
    }

    // MARK: - Public API

    /// Force refresh all cards
    func refresh() {
        guard !isRefreshing else { return }
        isRefreshing = true

        collectCards()

        isRefreshing = false
        logger.debug("cards refreshed count=\(self.cards.count)")
    }

    /// Get cards filtered by type
    func cards(ofType type: AnyContextCard.CardType.Type) -> [AnyContextCard] {
        cards.filter { card in
            switch card.cardType {
            case .activeSession: return true
            case .quickAction: return true
            case .recentConversation: return true
            case .systemStatus: return true
            case .costSummary: return true
            case .pendingApproval: return true
            }
        }
    }

    /// Get only high-priority cards (critical and high)
    func highPriorityCards() -> [AnyContextCard] {
        cards.filter { $0.priority >= .high }
    }

    // MARK: - Card Collection

    private func collectCards() {
        var collected: [AnyContextCard] = []

        // Collect active session cards
        collected.append(contentsOf: collectActiveSessionCards())

        // Collect pending approval cards
        collected.append(contentsOf: collectPendingApprovalCards())

        // Collect system status card
        if let statusCard = collectSystemStatusCard() {
            collected.append(statusCard)
        }

        // Collect cost summary card
        if let costCard = collectCostSummaryCard() {
            collected.append(costCard)
        }

        // Collect quick action cards
        collected.append(contentsOf: collectQuickActionCards())

        // Collect recent conversation cards
        collected.append(contentsOf: collectRecentConversationCards())

        // Sort by priority (descending) and limit count
        cards = collected
            .sorted { $0.priority > $1.priority }
            .prefix(maxCards)
            .map { $0 }

        // Ensure minimum cards if available
        while cards.count < minCards && cards.count < collected.count {
            let remaining = collected.filter { card in
                !cards.contains { $0.id == card.id }
            }
            if let next = remaining.first {
                cards.append(next)
            } else {
                break
            }
        }
    }

    private func collectActiveSessionCards() -> [AnyContextCard] {
        SessionBridge.shared.activeSessions
            .filter { $0.status == .active }
            .prefix(3)
            .map { session in
                let card = ActiveSessionCard(
                    id: "session_\(session.id)",
                    sessionId: session.id,
                    agentName: session.metadata.title ?? "Session",
                    sessionType: session.type,
                    status: mapSessionStatus(session),
                    startedAt: session.createdAt,
                    messageCount: session.metadata.messageCount ?? 0
                )
                return AnyContextCard(card)
            }
    }

    private func mapSessionStatus(_ session: SessionBridge.Session) -> ActiveSessionCard.SessionStatus {
        switch session.status {
        case .active: return .processing
        case .paused: return .idle
        case .completed: return .idle
        case .error: return .error
        }
    }

    private func collectPendingApprovalCards() -> [AnyContextCard] {
        // Access pending approvals through notification or shared state
        // For now, return empty - would need ExecApprovalsManager access
        return []
    }

    private func collectSystemStatusCard() -> AnyContextCard? {
        let card = SystemStatusCard(
            id: "system_status",
            isGatewayConnected: ControlChannel.shared.isConnected,
            memoryUsageMB: getMemoryUsage(),
            pendingApprovals: 0, // Would need ExecApprovalsManager access
            activeAgents: SessionBridge.shared.activeSessions.count
        )
        return AnyContextCard(card)
    }

    private func collectCostSummaryCard() -> AnyContextCard? {
        // Would need CostUsageStore access for actual data
        let card = CostSummaryCard(
            id: "cost_summary",
            todayCost: 0,
            yesterdayCost: nil,
            weekTotal: 0,
            trend: .unknown
        )
        return AnyContextCard(card)
    }

    private func collectQuickActionCards() -> [AnyContextCard] {
        QuickActionManager.shared.recentlyUsed
            .prefix(2)
            .compactMap { actionId in
                guard let action = QuickActionManager.shared.actions.first(where: { $0.id == actionId }) else {
                    return nil
                }
                let card = QuickActionCard(
                    id: "quickaction_\(action.id)",
                    action: action
                )
                return AnyContextCard(card)
            }
    }

    private func collectRecentConversationCards() -> [AnyContextCard] {
        ConversationMemory.shared.recentConversations(limit: 2)
            .map { conversation in
                let card = RecentConversationCard(
                    id: "conversation_\(conversation.id)",
                    conversation: conversation
                )
                return AnyContextCard(card)
            }
    }

    private func getMemoryUsage() -> Int? {
        var info = mach_task_basic_info()
        var count = mach_msg_type_number_t(MemoryLayout<mach_task_basic_info>.size) / 4
        let result = withUnsafeMutablePointer(to: &info) {
            $0.withMemoryRebound(to: integer_t.self, capacity: 1) {
                task_info(mach_task_self_, task_flavor_t(MACH_TASK_BASIC_INFO), $0, &count)
            }
        }
        guard result == KERN_SUCCESS else { return nil }
        return Int(info.resident_size / 1024 / 1024)
    }

    // MARK: - Observation

    private func startObserving() {
        // Initial refresh
        refresh()

        // Observe session changes
        updateTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(5))
                await self?.refresh()
            }
        }

        // Listen for specific events
        NotificationCenter.default.addObserver(
            forName: .sessionCreated,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            self?.refresh()
        }

        NotificationCenter.default.addObserver(
            forName: .sessionEnded,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            self?.refresh()
        }

        logger.debug("context card manager started observing")
    }

    deinit {
        updateTask?.cancel()
    }
}

// MARK: - Session Notification Names

extension Notification.Name {
    static let sessionCreated = Notification.Name("sessionCreated")
    static let sessionEnded = Notification.Name("sessionEnded")
}
