import AppKit
import CoreGraphics
import Foundation
import OSLog

/// Controller for programmatic mouse operations.
/// Used by computer use agents to interact with the UI.
@MainActor
final class MouseController {
    static let shared = MouseController()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "mouse")

    struct ClickOptions {
        var button: CGMouseButton = .left
        var clickCount: Int = 1
        var modifiers: CGEventFlags = []
        var delayBetweenClicks: TimeInterval = 0.05
    }

    /// Move mouse to a specific screen coordinate
    func moveTo(x: CGFloat, y: CGFloat) {
        let point = CGPoint(x: x, y: y)
        CGWarpMouseCursorPosition(point)
        logger.debug("mouse moved to (\(Int(x)), \(Int(y)))")
    }

    /// Get current mouse position
    func currentPosition() -> CGPoint {
        NSEvent.mouseLocation
    }

    /// Perform a click at the current position
    func click(options: ClickOptions = ClickOptions()) async {
        let position = currentPosition()
        await clickAt(x: position.x, y: position.y, options: options)
    }

    /// Perform a click at a specific position
    func clickAt(x: CGFloat, y: CGFloat, options: ClickOptions = ClickOptions()) async {
        let point = CGPoint(x: x, y: y)

        // First move to the position
        moveTo(x: x, y: y)

        // Small delay to ensure position is registered
        try? await Task.sleep(nanoseconds: 10_000_000) // 10ms

        let mouseDownType: CGEventType
        let mouseUpType: CGEventType

        switch options.button {
        case .left:
            mouseDownType = .leftMouseDown
            mouseUpType = .leftMouseUp
        case .right:
            mouseDownType = .rightMouseDown
            mouseUpType = .rightMouseUp
        case .center:
            mouseDownType = .otherMouseDown
            mouseUpType = .otherMouseUp
        @unknown default:
            mouseDownType = .leftMouseDown
            mouseUpType = .leftMouseUp
        }

        for clickIndex in 0..<options.clickCount {
            guard let mouseDown = CGEvent(
                mouseEventSource: nil,
                mouseType: mouseDownType,
                mouseCursorPosition: point,
                mouseButton: options.button
            ) else { continue }

            guard let mouseUp = CGEvent(
                mouseEventSource: nil,
                mouseType: mouseUpType,
                mouseCursorPosition: point,
                mouseButton: options.button
            ) else { continue }

            // Set click count for double/triple click detection
            mouseDown.setIntegerValueField(.mouseEventClickState, value: Int64(clickIndex + 1))
            mouseUp.setIntegerValueField(.mouseEventClickState, value: Int64(clickIndex + 1))

            // Apply modifiers
            if !options.modifiers.isEmpty {
                mouseDown.flags = options.modifiers
                mouseUp.flags = options.modifiers
            }

            mouseDown.post(tap: .cghidEventTap)
            mouseUp.post(tap: .cghidEventTap)

            if clickIndex < options.clickCount - 1 {
                try? await Task.sleep(nanoseconds: UInt64(options.delayBetweenClicks * 1_000_000_000))
            }
        }

        logger.debug("mouse clicked at (\(Int(x)), \(Int(y))) button=\(options.button.rawValue) count=\(options.clickCount)")
    }

    /// Perform a double click
    func doubleClick(x: CGFloat, y: CGFloat) async {
        await clickAt(x: x, y: y, options: ClickOptions(clickCount: 2))
    }

    /// Perform a right click
    func rightClick(x: CGFloat, y: CGFloat) async {
        await clickAt(x: x, y: y, options: ClickOptions(button: .right))
    }

    /// Drag from one point to another
    func drag(from: CGPoint, to: CGPoint, duration: TimeInterval = 0.3) async {
        moveTo(x: from.x, y: from.y)
        try? await Task.sleep(nanoseconds: 10_000_000)

        // Mouse down at start
        guard let mouseDown = CGEvent(
            mouseEventSource: nil,
            mouseType: .leftMouseDown,
            mouseCursorPosition: from,
            mouseButton: .left
        ) else { return }
        mouseDown.post(tap: .cghidEventTap)

        // Interpolate movement
        let steps = max(10, Int(duration * 60)) // ~60fps
        let dx = (to.x - from.x) / CGFloat(steps)
        let dy = (to.y - from.y) / CGFloat(steps)
        let stepDelay = UInt64(duration / Double(steps) * 1_000_000_000)

        for i in 1...steps {
            let x = from.x + dx * CGFloat(i)
            let y = from.y + dy * CGFloat(i)
            let point = CGPoint(x: x, y: y)

            guard let dragEvent = CGEvent(
                mouseEventSource: nil,
                mouseType: .leftMouseDragged,
                mouseCursorPosition: point,
                mouseButton: .left
            ) else { continue }
            dragEvent.post(tap: .cghidEventTap)

            try? await Task.sleep(nanoseconds: stepDelay)
        }

        // Mouse up at end
        guard let mouseUp = CGEvent(
            mouseEventSource: nil,
            mouseType: .leftMouseUp,
            mouseCursorPosition: to,
            mouseButton: .left
        ) else { return }
        mouseUp.post(tap: .cghidEventTap)

        logger.debug("mouse dragged from (\(Int(from.x)), \(Int(from.y))) to (\(Int(to.x)), \(Int(to.y)))")
    }

    /// Scroll at the current position
    func scroll(deltaX: Int32 = 0, deltaY: Int32) {
        guard let scrollEvent = CGEvent(
            scrollWheelEvent2Source: nil,
            units: .pixel,
            wheelCount: 2,
            wheel1: deltaY,
            wheel2: deltaX,
            wheel3: 0
        ) else { return }

        scrollEvent.post(tap: .cghidEventTap)
        logger.debug("mouse scrolled dx=\(deltaX) dy=\(deltaY)")
    }
}
