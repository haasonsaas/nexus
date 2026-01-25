import AppKit
import OSLog

/// Provides screen region selection for screenshot capture.
@MainActor
final class ScreenRegionSelector {
    static let shared = ScreenRegionSelector()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "region")

    private var overlayWindows: [NSWindow] = []
    private var selectionWindow: SelectionOverlayWindow?
    private var completion: ((CGRect?) -> Void)?

    private init() {}

    /// Start region selection with callback
    func startSelection(completion: @escaping (CGRect?) -> Void) {
        self.completion = completion

        // Create overlay windows for all screens
        for screen in NSScreen.screens {
            let overlayWindow = createOverlayWindow(for: screen)
            overlayWindows.append(overlayWindow)
            overlayWindow.orderFront(nil)
        }

        // Create selection window on main screen
        if let mainScreen = NSScreen.main {
            selectionWindow = SelectionOverlayWindow(screen: mainScreen)
            selectionWindow?.selectionComplete = { [weak self] rect in
                self?.finishSelection(rect: rect)
            }
            selectionWindow?.orderFront(nil)
            selectionWindow?.makeKeyAndOrderFront(nil)
        }

        logger.debug("region selection started")
    }

    /// Cancel selection
    func cancel() {
        finishSelection(rect: nil)
    }

    private func createOverlayWindow(for screen: NSScreen) -> NSWindow {
        let window = NSWindow(
            contentRect: screen.frame,
            styleMask: [.borderless],
            backing: .buffered,
            defer: false
        )
        window.level = .screenSaver
        window.backgroundColor = NSColor.black.withAlphaComponent(0.3)
        window.isOpaque = false
        window.ignoresMouseEvents = true
        window.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]

        return window
    }

    private func finishSelection(rect: CGRect?) {
        // Close all windows
        for window in overlayWindows {
            window.orderOut(nil)
        }
        overlayWindows.removeAll()

        selectionWindow?.orderOut(nil)
        selectionWindow = nil

        logger.debug("region selection finished: \(rect != nil ? "selected" : "cancelled")")

        completion?(rect)
        completion = nil
    }
}

// MARK: - Selection Overlay Window

private class SelectionOverlayWindow: NSWindow {
    var selectionComplete: ((CGRect?) -> Void)?

    private var selectionView: SelectionView!
    private var startPoint: NSPoint?
    private var currentRect: NSRect = .zero

    init(screen: NSScreen) {
        super.init(
            contentRect: screen.frame,
            styleMask: [.borderless],
            backing: .buffered,
            defer: false
        )

        level = .screenSaver + 1
        backgroundColor = .clear
        isOpaque = false
        hasShadow = false
        collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]

        selectionView = SelectionView(frame: screen.frame)
        contentView = selectionView

        // Set cursor
        NSCursor.crosshair.set()
    }

    override var canBecomeKey: Bool { true }

    override func mouseDown(with event: NSEvent) {
        startPoint = event.locationInWindow
        currentRect = .zero
        selectionView.selectionRect = currentRect
    }

    override func mouseDragged(with event: NSEvent) {
        guard let start = startPoint else { return }

        let current = event.locationInWindow
        currentRect = NSRect(
            x: min(start.x, current.x),
            y: min(start.y, current.y),
            width: abs(current.x - start.x),
            height: abs(current.y - start.y)
        )

        selectionView.selectionRect = currentRect
    }

    override func mouseUp(with event: NSEvent) {
        NSCursor.arrow.set()

        if currentRect.width > 10 && currentRect.height > 10 {
            // Convert to screen coordinates
            let screenRect = convertToScreen(currentRect)
            selectionComplete?(screenRect)
        } else {
            selectionComplete?(nil)
        }
    }

    override func keyDown(with event: NSEvent) {
        // Escape to cancel
        if event.keyCode == 53 {
            NSCursor.arrow.set()
            selectionComplete?(nil)
        }
    }

    private func convertToScreen(_ rect: NSRect) -> CGRect {
        guard let screen = screen else { return rect }

        // Convert from window coordinates to screen coordinates
        let screenHeight = screen.frame.height
        return CGRect(
            x: rect.minX,
            y: screenHeight - rect.maxY,
            width: rect.width,
            height: rect.height
        )
    }
}

// MARK: - Selection View

private class SelectionView: NSView {
    var selectionRect: NSRect = .zero {
        didSet { needsDisplay = true }
    }

    override func draw(_ dirtyRect: NSRect) {
        super.draw(dirtyRect)

        // Draw semi-transparent overlay
        NSColor.black.withAlphaComponent(0.01).setFill()
        dirtyRect.fill()

        guard selectionRect != .zero else { return }

        // Clear selection area
        NSColor.clear.setFill()
        let path = NSBezierPath(rect: selectionRect)
        path.fill()

        // Draw selection border
        NSColor.white.setStroke()
        path.lineWidth = 2
        path.stroke()

        // Draw dashed inner border
        NSColor.systemBlue.setStroke()
        let dashedPath = NSBezierPath(rect: selectionRect.insetBy(dx: 1, dy: 1))
        dashedPath.lineWidth = 1
        dashedPath.setLineDash([4, 4], count: 2, phase: 0)
        dashedPath.stroke()

        // Draw size label
        let sizeText = "\(Int(selectionRect.width)) x \(Int(selectionRect.height))"
        let attributes: [NSAttributedString.Key: Any] = [
            .font: NSFont.monospacedSystemFont(ofSize: 12, weight: .medium),
            .foregroundColor: NSColor.white,
            .backgroundColor: NSColor.black.withAlphaComponent(0.7)
        ]
        let textSize = sizeText.size(withAttributes: attributes)
        let textRect = NSRect(
            x: selectionRect.midX - textSize.width / 2,
            y: selectionRect.minY - textSize.height - 8,
            width: textSize.width + 8,
            height: textSize.height + 4
        )

        // Background for text
        NSColor.black.withAlphaComponent(0.7).setFill()
        NSBezierPath(roundedRect: textRect, xRadius: 4, yRadius: 4).fill()

        // Draw text
        sizeText.draw(
            at: NSPoint(x: textRect.minX + 4, y: textRect.minY + 2),
            withAttributes: attributes
        )
    }
}
