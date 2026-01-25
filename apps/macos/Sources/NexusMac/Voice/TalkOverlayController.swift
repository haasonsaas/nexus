import AppKit
import SwiftUI
import OSLog

// MARK: - Overlay Position

/// Position options for the talk overlay
enum VoiceTalkOverlayPosition: Sendable {
    case topLeft
    case topRight
    case bottomLeft
    case bottomRight
    case center
}

// MARK: - Voice Talk Overlay Controller

/// Controls the talk mode overlay window
@MainActor
final class VoiceTalkOverlayController {
    static let shared = VoiceTalkOverlayController()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "voice-talk-overlay")

    private var panel: NSPanel?
    private var isDragging = false
    private var dragOffset: NSPoint = .zero

    private init() {}

    // MARK: - Presentation

    func show() {
        guard panel == nil else { return }

        let view = VoiceTalkOverlayView()
        let hostingView = NSHostingView(rootView: view)

        // Create panel
        panel = NSPanel(
            contentRect: NSRect(x: 0, y: 0, width: 120, height: 120),
            styleMask: [.nonactivatingPanel, .borderless],
            backing: .buffered,
            defer: false
        )

        guard let panel = panel else { return }

        panel.contentView = hostingView
        panel.backgroundColor = .clear
        panel.isOpaque = false
        panel.hasShadow = true
        panel.level = .floating
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
        panel.isMovableByWindowBackground = true

        // Position in bottom right
        positionPanel()

        panel.orderFront(nil)
        logger.info("Talk overlay shown")
    }

    func hide() {
        panel?.close()
        panel = nil
        logger.info("Talk overlay hidden")
    }

    var isShowing: Bool {
        panel != nil
    }

    // MARK: - Positioning

    private func positionPanel() {
        guard let panel = panel,
              let screen = NSScreen.main else { return }

        let screenFrame = screen.visibleFrame
        let panelSize = panel.frame.size

        // Bottom right with padding
        let x = screenFrame.maxX - panelSize.width - 20
        let y = screenFrame.minY + 20

        panel.setFrameOrigin(NSPoint(x: x, y: y))
    }

    func moveToPosition(_ position: VoiceTalkOverlayPosition) {
        guard let panel = panel,
              let screen = NSScreen.main else { return }

        let screenFrame = screen.visibleFrame
        let panelSize = panel.frame.size
        let padding: CGFloat = 20

        let point: NSPoint
        switch position {
        case .topLeft:
            point = NSPoint(x: screenFrame.minX + padding, y: screenFrame.maxY - panelSize.height - padding)
        case .topRight:
            point = NSPoint(x: screenFrame.maxX - panelSize.width - padding, y: screenFrame.maxY - panelSize.height - padding)
        case .bottomLeft:
            point = NSPoint(x: screenFrame.minX + padding, y: screenFrame.minY + padding)
        case .bottomRight:
            point = NSPoint(x: screenFrame.maxX - panelSize.width - padding, y: screenFrame.minY + padding)
        case .center:
            point = NSPoint(
                x: screenFrame.midX - panelSize.width / 2,
                y: screenFrame.midY - panelSize.height / 2
            )
        }

        NSAnimationContext.runAnimationGroup { context in
            context.duration = 0.3
            context.timingFunction = CAMediaTimingFunction(name: .easeInEaseOut)
            panel.animator().setFrameOrigin(point)
        }
    }

    // MARK: - Audio Level Updates

    func updateAudioLevel(_ level: Float) {
        // The view observes VoiceTalkModeRuntime directly
    }
}
