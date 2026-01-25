import AppKit
import OSLog
import SwiftUI

/// Manages the menu bar extra (status item) and popover.
/// Provides centralized control for the menu bar presence.
@MainActor
final class MenuBarManager {
    static let shared = MenuBarManager()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "menu-bar")

    private var statusItem: NSStatusItem?
    private var popover: NSPopover?
    private var eventMonitor: Any?
    private var healthObserver: NSObjectProtocol?

    // MARK: - Initialization

    private init() {}

    // MARK: - Setup

    /// Set up the menu bar status item and popover.
    func setup() {
        guard statusItem == nil else {
            logger.debug("Menu bar already set up")
            return
        }

        // Create status item
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)

        if let button = statusItem?.button {
            button.image = NSImage(
                systemSymbolName: "circle.hexagongrid.fill",
                accessibilityDescription: "Nexus"
            )
            button.image?.isTemplate = true
            button.action = #selector(togglePopover(_:))
            button.target = self
        }

        // Create popover
        popover = NSPopover()
        popover?.contentSize = NSSize(width: 300, height: 400)
        popover?.behavior = .transient
        popover?.animates = true
        popover?.contentViewController = NSHostingController(
            rootView: MenuContentViewV2(
                onOpenChat: { [weak self] in
                    self?.hidePopover()
                    let session = SessionBridge.shared.createSession(type: .chat)
                    WebChatManager.shared.openChat(for: session.id)
                },
                onOpenSettings: { [weak self] in
                    self?.hidePopover()
                    NSApp.sendAction(Selector(("showSettingsWindow:")), to: nil, from: nil)
                }
            )
        )

        // Set up event monitor to close popover on outside click
        eventMonitor = NSEvent.addGlobalMonitorForEvents(
            matching: [.leftMouseDown, .rightMouseDown]
        ) { [weak self] _ in
            if let popover = self?.popover, popover.isShown {
                popover.performClose(nil)
            }
        }

        // Observe health changes
        setupHealthObserver()

        logger.info("Menu bar setup complete")
    }

    /// Tear down the menu bar status item and popover.
    func teardown() {
        if let statusItem = statusItem {
            NSStatusBar.system.removeStatusItem(statusItem)
        }
        statusItem = nil

        popover?.close()
        popover = nil

        if let eventMonitor = eventMonitor {
            NSEvent.removeMonitor(eventMonitor)
        }
        eventMonitor = nil

        if let healthObserver = healthObserver {
            NotificationCenter.default.removeObserver(healthObserver)
        }
        healthObserver = nil

        logger.info("Menu bar torn down")
    }

    // MARK: - Popover Control

    @objc private func togglePopover(_ sender: AnyObject?) {
        if let popover = popover, popover.isShown {
            hidePopover()
        } else {
            showPopover()
        }
    }

    /// Show the popover from the status item.
    func showPopover() {
        guard let button = statusItem?.button, let popover = popover else { return }
        popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
        logger.debug("Popover shown")
    }

    /// Hide the popover.
    func hidePopover() {
        popover?.performClose(nil)
        logger.debug("Popover hidden")
    }

    // MARK: - Badge

    /// Set a notification badge on the status item.
    /// - Parameter count: The badge count (0 to remove badge).
    func setBadge(_ count: Int) {
        guard let button = statusItem?.button else { return }

        if count > 0 {
            button.image = createBadgedImage(count: count)
        } else {
            button.image = NSImage(
                systemSymbolName: "circle.hexagongrid.fill",
                accessibilityDescription: "Nexus"
            )
            button.image?.isTemplate = true
        }

        logger.debug("Badge set to \(count)")
    }

    private func createBadgedImage(count: Int) -> NSImage {
        let baseImage = NSImage(
            systemSymbolName: "circle.hexagongrid.fill",
            accessibilityDescription: "Nexus"
        )!
        let size = NSSize(width: 22, height: 22)

        let image = NSImage(size: size)
        image.lockFocus()

        // Draw base image
        baseImage.draw(in: NSRect(origin: .zero, size: size))

        // Draw badge circle
        let badgeRect = NSRect(x: 12, y: 0, width: 10, height: 10)
        NSColor.systemRed.setFill()
        NSBezierPath(ovalIn: badgeRect).fill()

        // Draw badge count
        let attrs: [NSAttributedString.Key: Any] = [
            .font: NSFont.systemFont(ofSize: 7, weight: .bold),
            .foregroundColor: NSColor.white
        ]
        let str = count > 9 ? "9+" : "\(count)"
        let strSize = str.size(withAttributes: attrs)
        str.draw(
            at: NSPoint(
                x: badgeRect.midX - strSize.width / 2,
                y: badgeRect.midY - strSize.height / 2
            ),
            withAttributes: attrs
        )

        image.unlockFocus()
        image.isTemplate = false  // Badge needs color, so not template
        return image
    }

    // MARK: - Status Updates

    /// Update the status item icon based on health state.
    /// - Parameter state: The current health state.
    func updateIcon(for state: HealthState) {
        guard let button = statusItem?.button else { return }

        let symbolName: String
        switch state {
        case .ok:
            symbolName = "circle.hexagongrid.fill"
        case .degraded:
            symbolName = "exclamationmark.circle.fill"
        case .linkingNeeded:
            symbolName = "link.badge.plus"
        case .unknown:
            symbolName = "questionmark.circle"
        }

        button.image = NSImage(
            systemSymbolName: symbolName,
            accessibilityDescription: "Nexus - \(state)"
        )
        button.image?.isTemplate = (state == .ok)

        logger.debug("Icon updated for state: \(String(describing: state))")
    }

    /// Update the status item with a connection state indicator.
    /// - Parameter state: The current connection state.
    func updateIcon(for state: ControlChannel.ConnectionState) {
        guard let button = statusItem?.button else { return }

        let symbolName: String
        var isTemplate = true

        switch state {
        case .connected:
            symbolName = "circle.hexagongrid.fill"
        case .connecting:
            symbolName = "arrow.triangle.2.circlepath"
        case .disconnected:
            symbolName = "circle.hexagongrid"
            isTemplate = false
        case .degraded:
            symbolName = "exclamationmark.circle.fill"
            isTemplate = false
        }

        button.image = NSImage(
            systemSymbolName: symbolName,
            accessibilityDescription: "Nexus"
        )
        button.image?.isTemplate = isTemplate
    }

    // MARK: - Health Observer

    private func setupHealthObserver() {
        // Use Task to observe HealthStore changes
        Task { [weak self] in
            let health = HealthStore.shared
            var lastState = health.state

            // Initial update
            self?.updateIcon(for: lastState)

            // Poll for changes (since we can't directly observe @Observable)
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 1_000_000_000)  // 1 second
                let currentState = health.state
                if currentState != lastState {
                    lastState = currentState
                    await MainActor.run {
                        self?.updateIcon(for: currentState)
                    }
                }
            }
        }
    }

    // MARK: - Quick Actions

    /// Show the voice input overlay.
    func showVoiceInput() {
        hidePopover()
        VoiceWakeOverlayController.shared.show(source: .pushToTalk)
    }

    /// Trigger a screenshot capture.
    func captureScreenshot() {
        hidePopover()
        Task {
            _ = try? await ScreenCaptureService.shared.capture()
        }
    }

    /// Open a new chat window.
    func newChat() {
        hidePopover()
        let session = SessionBridge.shared.createSession(type: .chat)
        WebChatManager.shared.openChat(for: session.id)
    }

    /// Open the settings window.
    func openSettings() {
        hidePopover()
        NSApp.sendAction(Selector(("showSettingsWindow:")), to: nil, from: nil)
    }
}

// MARK: - Menu Bar Controller

/// SwiftUI-compatible wrapper for MenuBarManager.
/// Enables easy integration with SwiftUI lifecycle.
@MainActor
@Observable
final class MenuBarController {
    static let shared = MenuBarController()

    private(set) var isSetup = false
    private(set) var badgeCount = 0
    private(set) var isPopoverShown = false

    private init() {}

    func setup() {
        guard !isSetup else { return }
        MenuBarManager.shared.setup()
        isSetup = true
    }

    func teardown() {
        guard isSetup else { return }
        MenuBarManager.shared.teardown()
        isSetup = false
    }

    func setBadge(_ count: Int) {
        badgeCount = count
        MenuBarManager.shared.setBadge(count)
    }

    func showPopover() {
        MenuBarManager.shared.showPopover()
        isPopoverShown = true
    }

    func hidePopover() {
        MenuBarManager.shared.hidePopover()
        isPopoverShown = false
    }

    func togglePopover() {
        if isPopoverShown {
            hidePopover()
        } else {
            showPopover()
        }
    }
}

// MARK: - Notification Badge Observer

/// Observes notifications that should update the menu bar badge.
@MainActor
final class MenuBarBadgeObserver {
    static let shared = MenuBarBadgeObserver()

    private var pendingApprovals = 0
    private var unreadNotifications = 0

    private init() {
        setupObservers()
    }

    private func setupObservers() {
        // Observe exec approval changes
        NotificationCenter.default.addObserver(
            forName: .execApprovalAdded,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            self?.pendingApprovals += 1
            self?.updateBadge()
        }

        NotificationCenter.default.addObserver(
            forName: .execApprovalResolved,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            self?.pendingApprovals = max(0, (self?.pendingApprovals ?? 0) - 1)
            self?.updateBadge()
        }
    }

    private func updateBadge() {
        let total = pendingApprovals + unreadNotifications
        MenuBarManager.shared.setBadge(total)
    }

    func clearAll() {
        pendingApprovals = 0
        unreadNotifications = 0
        updateBadge()
    }
}

// MARK: - Notification Names

extension Notification.Name {
    static let execApprovalAdded = Notification.Name("nexus.exec.approval.added")
    static let execApprovalResolved = Notification.Name("nexus.exec.approval.resolved")
}
