import AppKit
import Observation
import OSLog

// MARK: - Dock Icon State

/// Represents the visual state of the dock icon
enum DockIconState: Equatable {
    case idle
    case active
    case pending(count: Int)
    case warning
    case error
    case hidden

    var description: String {
        switch self {
        case .idle: return "idle"
        case .active: return "active"
        case .pending(let count): return "pending(\(count))"
        case .warning: return "warning"
        case .error: return "error"
        case .hidden: return "hidden"
        }
    }
}

// MARK: - Dock Icon Manager

/// Central manager for Dock icon appearance and state.
/// Dynamically updates the dock icon based on app state, connection status, and pending approvals.
@MainActor
@Observable
final class DockIconManager {
    static let shared = DockIconManager()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "dock-icon")

    // MARK: - State

    private(set) var currentState: DockIconState = .idle {
        didSet {
            guard oldValue != currentState else { return }
            logger.debug("dock icon state changed: \(oldValue.description) -> \(self.currentState.description)")
            applyState()
        }
    }

    private(set) var badgeText: String?
    private(set) var progress: Double?

    // MARK: - Animation State

    private var pulseTimer: Timer?
    private var pulsePhase: CGFloat = 0
    private var progressTimer: Timer?

    // MARK: - User Preference

    private static let showDockIconKey = "nexus.showDockIcon"
    private var userWantsDockIcon: Bool {
        UserDefaults.standard.bool(forKey: Self.showDockIconKey)
    }

    // MARK: - Observers

    private var controlChannelObserver: NSObjectProtocol?
    private var approvalsObserver: NSObjectProtocol?
    private var agentEventObserver: NSObjectProtocol?
    private var observationTask: Task<Void, Never>?

    // MARK: - Initialization

    private init() {
        logger.info("dock icon manager initializing")
        setupObservers()
        applyState()
    }

    deinit {
        Task { @MainActor [weak self] in
            guard let self else { return }
            self.pulseTimer?.invalidate()
            self.progressTimer?.invalidate()
            self.observationTask?.cancel()
            if let observer = self.controlChannelObserver {
                NotificationCenter.default.removeObserver(observer)
            }
            if let observer = self.approvalsObserver {
                NotificationCenter.default.removeObserver(observer)
            }
            if let observer = self.agentEventObserver {
                NotificationCenter.default.removeObserver(observer)
            }
        }
    }

    // MARK: - Public Methods

    /// Set the dock icon state explicitly
    func setState(_ state: DockIconState) {
        currentState = state
    }

    /// Set badge text on the dock icon
    func setBadge(_ text: String?) {
        badgeText = text
        NSApp?.dockTile.badgeLabel = text
        logger.debug("dock badge set: \(text ?? "nil")")
    }

    /// Show progress indicator on dock icon (0.0 to 1.0)
    func showProgress(_ progress: Double) {
        self.progress = max(0, min(1, progress))
        updateProgressView()
    }

    /// Reset dock icon to default state
    func reset() {
        stopAnimations()
        currentState = .idle
        badgeText = nil
        progress = nil
        NSApp?.dockTile.badgeLabel = nil
        NSApp?.dockTile.contentView = nil
        NSApp?.dockTile.display()
        logger.info("dock icon reset to default")
    }

    /// Update dock visibility based on window state and user preference
    func updateDockVisibility() {
        guard NSApp != nil else {
            logger.warning("NSApp not ready, skipping dock visibility update")
            return
        }

        if case .hidden = currentState {
            NSApp?.setActivationPolicy(.accessory)
            return
        }

        let visibleWindows = NSApp?.windows.filter { window in
            window.isVisible &&
                window.frame.width > 1 &&
                window.frame.height > 1 &&
                !window.isKind(of: NSPanel.self) &&
                "\(type(of: window))" != "NSPopupMenuWindow" &&
                window.contentViewController != nil
        } ?? []

        let hasVisibleWindows = !visibleWindows.isEmpty

        if userWantsDockIcon || hasVisibleWindows {
            NSApp?.setActivationPolicy(.regular)
        } else {
            NSApp?.setActivationPolicy(.accessory)
        }
    }

    /// Temporarily show dock icon (e.g., for notifications)
    func temporarilyShowDock() {
        guard NSApp != nil else {
            logger.warning("NSApp not ready, cannot show dock icon")
            return
        }
        NSApp.setActivationPolicy(.regular)
    }

    // MARK: - State Application

    private func applyState() {
        stopAnimations()

        switch currentState {
        case .idle:
            applyIdleState()

        case .active:
            applyActiveState()

        case .pending(let count):
            applyPendingState(count: count)

        case .warning:
            applyWarningState()

        case .error:
            applyErrorState()

        case .hidden:
            applyHiddenState()
        }

        updateDockVisibility()
    }

    private func applyIdleState() {
        NSApp?.dockTile.contentView = nil
        NSApp?.dockTile.badgeLabel = badgeText
        NSApp?.dockTile.display()
    }

    private func applyActiveState() {
        let contentView = DockTileContentView(state: .active)
        NSApp?.dockTile.contentView = contentView
        NSApp?.dockTile.badgeLabel = badgeText
        NSApp?.dockTile.display()

        startPulseAnimation()
    }

    private func applyPendingState(count: Int) {
        let contentView = DockTileContentView(state: .pending(count: count))
        NSApp?.dockTile.contentView = contentView
        NSApp?.dockTile.badgeLabel = count > 0 ? "\(count)" : nil
        NSApp?.dockTile.display()
    }

    private func applyWarningState() {
        let contentView = DockTileContentView(state: .warning)
        NSApp?.dockTile.contentView = contentView
        NSApp?.dockTile.badgeLabel = badgeText ?? "!"
        NSApp?.dockTile.display()
    }

    private func applyErrorState() {
        let contentView = DockTileContentView(state: .error)
        NSApp?.dockTile.contentView = contentView
        NSApp?.dockTile.badgeLabel = badgeText ?? "!"
        NSApp?.dockTile.display()
    }

    private func applyHiddenState() {
        NSApp?.setActivationPolicy(.accessory)
    }

    // MARK: - Animations

    private func startPulseAnimation() {
        pulsePhase = 0
        pulseTimer = Timer.scheduledTimer(withTimeInterval: 0.05, repeats: true) { [weak self] _ in
            Task { @MainActor in
                self?.updatePulse()
            }
        }
    }

    private func updatePulse() {
        pulsePhase += 0.1
        if pulsePhase > .pi * 2 {
            pulsePhase = 0
        }

        if let contentView = NSApp?.dockTile.contentView as? DockTileContentView {
            let scale = 1.0 + sin(pulsePhase) * 0.05
            contentView.pulseScale = scale
            contentView.needsDisplay = true
            NSApp?.dockTile.display()
        }
    }

    private func stopAnimations() {
        pulseTimer?.invalidate()
        pulseTimer = nil
        progressTimer?.invalidate()
        progressTimer = nil
    }

    private func updateProgressView() {
        guard let progress else {
            NSApp?.dockTile.contentView = nil
            return
        }

        let contentView = DockTileProgressView(progress: progress)
        NSApp?.dockTile.contentView = contentView
        NSApp?.dockTile.display()
    }

    // MARK: - Observers

    private func setupObservers() {
        // Observe control channel state changes
        observationTask = Task { [weak self] in
            await self?.observeControlChannel()
        }

        // Observe heartbeat events for agent activity
        agentEventObserver = NotificationCenter.default.addObserver(
            forName: .controlAgentEvent,
            object: nil,
            queue: .main
        ) { [weak self] notification in
            Task { @MainActor in
                self?.handleAgentEvent(notification)
            }
        }

        // Observe window changes for dock visibility
        NotificationCenter.default.addObserver(
            forName: NSWindow.didBecomeKeyNotification,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            Task { @MainActor in
                self?.updateDockVisibility()
            }
        }

        NotificationCenter.default.addObserver(
            forName: NSWindow.willCloseNotification,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            Task { @MainActor in
                try? await Task.sleep(nanoseconds: 50_000_000)
                self?.updateDockVisibility()
            }
        }

        NotificationCenter.default.addObserver(
            forName: UserDefaults.didChangeNotification,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            Task { @MainActor in
                self?.updateDockVisibility()
            }
        }

        logger.debug("observers configured")
    }

    private func observeControlChannel() async {
        // Use withObservationTracking to observe ControlChannel state changes
        while !Task.isCancelled {
            let channelState = await withCheckedContinuation { continuation in
                withObservationTracking {
                    _ = ControlChannel.shared.state
                } onChange: {
                    Task { @MainActor in
                        continuation.resume(returning: ControlChannel.shared.state)
                    }
                }
            }

            updateStateFromControlChannel(channelState)
        }
    }

    private func updateStateFromControlChannel(_ channelState: ControlChannel.ConnectionState) {
        // Don't override pending or active states with connection status
        switch currentState {
        case .active, .pending:
            return
        default:
            break
        }

        switch channelState {
        case .disconnected:
            currentState = .error

        case .connecting:
            currentState = .warning

        case .connected:
            currentState = .idle

        case .degraded:
            currentState = .warning
        }
    }

    private func handleAgentEvent(_ notification: Notification) {
        guard let event = notification.object as? ControlAgentEvent else { return }

        // Check for agent start/stop events
        if event.stream == "start" {
            currentState = .active
        } else if event.stream == "end" || event.stream == "error" {
            // Delay returning to idle to allow for rapid successive events
            Task {
                try? await Task.sleep(nanoseconds: 500_000_000)
                if case .active = currentState {
                    currentState = .idle
                }
            }
        }
    }

    /// Update state based on pending approvals count
    func updatePendingApprovals(count: Int) {
        if count > 0 {
            currentState = .pending(count: count)
            setBadge("\(count)")
        } else if case .pending = currentState {
            currentState = .idle
            setBadge(nil)
        }
    }
}

// MARK: - Dock Tile Content View

/// Custom NSView for rendering advanced dock tile states
private class DockTileContentView: NSView {
    let state: DockIconState
    var pulseScale: CGFloat = 1.0

    init(state: DockIconState) {
        self.state = state
        super.init(frame: NSRect(x: 0, y: 0, width: 128, height: 128))
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    override func draw(_ dirtyRect: NSRect) {
        super.draw(dirtyRect)

        guard let context = NSGraphicsContext.current?.cgContext else { return }

        let bounds = self.bounds
        let center = CGPoint(x: bounds.midX, y: bounds.midY)
        let iconSize: CGFloat = 100 * pulseScale

        // Draw base app icon
        if let appIcon = NSApp?.applicationIconImage {
            let iconRect = NSRect(
                x: center.x - iconSize / 2,
                y: center.y - iconSize / 2,
                width: iconSize,
                height: iconSize
            )
            appIcon.draw(in: iconRect)
        }

        // Draw state overlay
        switch state {
        case .active:
            drawActivityIndicator(context: context, center: center, bounds: bounds)

        case .pending(let count):
            drawPendingBadge(context: context, count: count, bounds: bounds)

        case .warning:
            drawOverlay(context: context, color: .orange, bounds: bounds)

        case .error:
            drawOverlay(context: context, color: .red, bounds: bounds)

        case .idle, .hidden:
            break
        }
    }

    private func drawActivityIndicator(context: CGContext, center: CGPoint, bounds: NSRect) {
        // Draw pulsing ring around icon
        let ringRadius: CGFloat = 55 * pulseScale
        let ringWidth: CGFloat = 3

        context.setStrokeColor(NSColor.systemBlue.withAlphaComponent(0.8).cgColor)
        context.setLineWidth(ringWidth)

        let ringPath = CGPath(
            ellipseIn: CGRect(
                x: center.x - ringRadius,
                y: center.y - ringRadius,
                width: ringRadius * 2,
                height: ringRadius * 2
            ),
            transform: nil
        )

        context.addPath(ringPath)
        context.strokePath()
    }

    private func drawPendingBadge(context: CGContext, count: Int, bounds: NSRect) {
        // Badge is handled by dockTile.badgeLabel
        // Draw attention indicator
        let indicatorSize: CGFloat = 20
        let indicatorRect = CGRect(
            x: bounds.maxX - indicatorSize - 10,
            y: bounds.maxY - indicatorSize - 10,
            width: indicatorSize,
            height: indicatorSize
        )

        context.setFillColor(NSColor.systemOrange.cgColor)
        context.fillEllipse(in: indicatorRect)
    }

    private func drawOverlay(context: CGContext, color: NSColor, bounds: NSRect) {
        // Draw semi-transparent color overlay on bottom portion
        let overlayHeight: CGFloat = 30
        let overlayRect = CGRect(
            x: 14,
            y: 14,
            width: bounds.width - 28,
            height: overlayHeight
        )

        context.setFillColor(color.withAlphaComponent(0.7).cgColor)

        let cornerRadius: CGFloat = 6
        let path = CGPath(
            roundedRect: overlayRect,
            cornerWidth: cornerRadius,
            cornerHeight: cornerRadius,
            transform: nil
        )

        context.addPath(path)
        context.fillPath()

        // Draw icon in overlay
        let iconName: String
        switch state {
        case .warning:
            iconName = "exclamationmark.triangle.fill"
        case .error:
            iconName = "xmark.circle.fill"
        default:
            return
        }

        if let symbolImage = NSImage(systemSymbolName: iconName, accessibilityDescription: nil) {
            let symbolConfig = NSImage.SymbolConfiguration(pointSize: 14, weight: .medium)
            let configuredImage = symbolImage.withSymbolConfiguration(symbolConfig)

            let symbolRect = CGRect(
                x: overlayRect.midX - 7,
                y: overlayRect.midY - 7,
                width: 14,
                height: 14
            )

            configuredImage?.draw(in: symbolRect, from: .zero, operation: .sourceOver, fraction: 1.0)
        }
    }
}

// MARK: - Dock Tile Progress View

/// NSView for rendering a progress indicator on the dock tile
private class DockTileProgressView: NSView {
    let progress: Double

    init(progress: Double) {
        self.progress = progress
        super.init(frame: NSRect(x: 0, y: 0, width: 128, height: 128))
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    override func draw(_ dirtyRect: NSRect) {
        super.draw(dirtyRect)

        guard let context = NSGraphicsContext.current?.cgContext else { return }

        let bounds = self.bounds
        let center = CGPoint(x: bounds.midX, y: bounds.midY)

        // Draw base app icon
        if let appIcon = NSApp?.applicationIconImage {
            let iconSize: CGFloat = 100
            let iconRect = NSRect(
                x: center.x - iconSize / 2,
                y: center.y - iconSize / 2,
                width: iconSize,
                height: iconSize
            )
            appIcon.draw(in: iconRect)
        }

        // Draw progress bar at bottom
        let barHeight: CGFloat = 8
        let barPadding: CGFloat = 20
        let barY: CGFloat = 16

        let backgroundRect = CGRect(
            x: barPadding,
            y: barY,
            width: bounds.width - barPadding * 2,
            height: barHeight
        )

        // Background
        context.setFillColor(NSColor.black.withAlphaComponent(0.5).cgColor)
        let backgroundPath = CGPath(
            roundedRect: backgroundRect,
            cornerWidth: barHeight / 2,
            cornerHeight: barHeight / 2,
            transform: nil
        )
        context.addPath(backgroundPath)
        context.fillPath()

        // Progress fill
        let progressWidth = (bounds.width - barPadding * 2) * CGFloat(progress)
        let progressRect = CGRect(
            x: barPadding,
            y: barY,
            width: progressWidth,
            height: barHeight
        )

        context.setFillColor(NSColor.systemBlue.cgColor)
        let progressPath = CGPath(
            roundedRect: progressRect,
            cornerWidth: barHeight / 2,
            cornerHeight: barHeight / 2,
            transform: nil
        )
        context.addPath(progressPath)
        context.fillPath()
    }
}

// MARK: - Dock Tile Plugin

/// Plugin for advanced dock tile customization
/// Note: To use this, add it to the Info.plist under NSDockTilePlugIn
final class NexusDockTilePlugIn: NSObject, NSDockTilePlugIn {
    var dockMenu: NSMenu?

    func setDockTile(_ dockTile: NSDockTile?) {
        // Configure dock tile if needed
        dockTile?.contentView = nil
        dockTile?.display()
    }
}
