import AppKit
import ApplicationServices
import Foundation
import OSLog

/// Bridge to macOS Accessibility APIs for enhanced context gathering.
/// Provides access to UI elements and text content from applications.
@MainActor
final class AccessibilityBridge {
    static let shared = AccessibilityBridge()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "accessibility")

    private(set) var hasPermission = false

    struct UIElement {
        let role: String
        let title: String?
        let value: String?
        let description: String?
        let frame: CGRect
        let children: [UIElement]
    }

    // MARK: - Permission

    /// Check accessibility permission status
    func checkPermission() -> Bool {
        let trusted = AXIsProcessTrusted()
        hasPermission = trusted
        logger.debug("accessibility permission=\(trusted)")
        return trusted
    }

    /// Request accessibility permission
    func requestPermission() {
        let options = [kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: true] as CFDictionary
        _ = AXIsProcessTrustedWithOptions(options)
        logger.info("accessibility permission requested")
    }

    // MARK: - Element Access

    /// Get the focused UI element
    func getFocusedElement() -> UIElement? {
        guard hasPermission else { return nil }

        guard let systemWide = AXUIElementCreateSystemWide() as AXUIElement? else {
            return nil
        }

        var focusedElement: AnyObject?
        let result = AXUIElementCopyAttributeValue(systemWide, kAXFocusedUIElementAttribute as CFString, &focusedElement)

        guard result == .success,
              let element = focusedElement as! AXUIElement? else {
            return nil
        }

        return parseElement(element)
    }

    /// Get the frontmost application's window element
    func getFrontmostWindowElement() -> UIElement? {
        guard hasPermission else { return nil }

        guard let frontApp = NSWorkspace.shared.frontmostApplication else {
            return nil
        }

        let appElement = AXUIElementCreateApplication(frontApp.processIdentifier)

        var focusedWindow: AnyObject?
        let result = AXUIElementCopyAttributeValue(appElement, kAXFocusedWindowAttribute as CFString, &focusedWindow)

        guard result == .success,
              let window = focusedWindow as! AXUIElement? else {
            return nil
        }

        return parseElement(window)
    }

    /// Get selected text from the focused application
    func getSelectedText() -> String? {
        guard hasPermission else { return nil }

        guard let systemWide = AXUIElementCreateSystemWide() as AXUIElement? else {
            return nil
        }

        var focusedElement: AnyObject?
        let result = AXUIElementCopyAttributeValue(systemWide, kAXFocusedUIElementAttribute as CFString, &focusedElement)

        guard result == .success,
              let element = focusedElement as! AXUIElement? else {
            return nil
        }

        var selectedText: AnyObject?
        let textResult = AXUIElementCopyAttributeValue(element, kAXSelectedTextAttribute as CFString, &selectedText)

        guard textResult == .success,
              let text = selectedText as? String else {
            return nil
        }

        return text
    }

    /// Get all text content from an element
    func getTextContent(from element: AXUIElement) -> String? {
        var value: AnyObject?
        let result = AXUIElementCopyAttributeValue(element, kAXValueAttribute as CFString, &value)

        if result == .success, let text = value as? String {
            return text
        }

        return nil
    }

    // MARK: - Element Actions

    /// Perform an action on a UI element
    func performAction(_ action: String, on element: AXUIElement) -> Bool {
        let result = AXUIElementPerformAction(element, action as CFString)
        return result == .success
    }

    /// Set value on a UI element
    func setValue(_ value: String, on element: AXUIElement) -> Bool {
        let result = AXUIElementSetAttributeValue(element, kAXValueAttribute as CFString, value as CFTypeRef)
        return result == .success
    }

    // MARK: - Element Navigation

    /// Find elements by role
    func findElements(withRole role: String, in rootElement: AXUIElement) -> [AXUIElement] {
        var result: [AXUIElement] = []

        var children: AnyObject?
        let childResult = AXUIElementCopyAttributeValue(rootElement, kAXChildrenAttribute as CFString, &children)

        guard childResult == .success,
              let childArray = children as? [AXUIElement] else {
            return result
        }

        for child in childArray {
            var childRole: AnyObject?
            AXUIElementCopyAttributeValue(child, kAXRoleAttribute as CFString, &childRole)

            if let r = childRole as? String, r == role {
                result.append(child)
            }

            // Recursively search children
            result.append(contentsOf: findElements(withRole: role, in: child))
        }

        return result
    }

    /// Find element at point
    func elementAt(point: CGPoint, in app: AXUIElement) -> AXUIElement? {
        var element: AXUIElement?
        let result = AXUIElementCopyElementAtPosition(app, Float(point.x), Float(point.y), &element)

        guard result == .success else {
            return nil
        }

        return element
    }

    // MARK: - Private

    private func parseElement(_ element: AXUIElement, depth: Int = 0) -> UIElement {
        var role: AnyObject?
        AXUIElementCopyAttributeValue(element, kAXRoleAttribute as CFString, &role)

        var title: AnyObject?
        AXUIElementCopyAttributeValue(element, kAXTitleAttribute as CFString, &title)

        var value: AnyObject?
        AXUIElementCopyAttributeValue(element, kAXValueAttribute as CFString, &value)

        var description: AnyObject?
        AXUIElementCopyAttributeValue(element, kAXDescriptionAttribute as CFString, &description)

        var position: AnyObject?
        AXUIElementCopyAttributeValue(element, kAXPositionAttribute as CFString, &position)

        var size: AnyObject?
        AXUIElementCopyAttributeValue(element, kAXSizeAttribute as CFString, &size)

        var frame = CGRect.zero
        if let positionValue = position {
            var point = CGPoint.zero
            AXValueGetValue(positionValue as! AXValue, .cgPoint, &point)
            frame.origin = point
        }
        if let sizeValue = size {
            var sizeVal = CGSize.zero
            AXValueGetValue(sizeValue as! AXValue, .cgSize, &sizeVal)
            frame.size = sizeVal
        }

        var children: [UIElement] = []
        if depth < 3 { // Limit depth for performance
            var childrenValue: AnyObject?
            let result = AXUIElementCopyAttributeValue(element, kAXChildrenAttribute as CFString, &childrenValue)

            if result == .success, let childArray = childrenValue as? [AXUIElement] {
                children = childArray.prefix(10).map { parseElement($0, depth: depth + 1) }
            }
        }

        return UIElement(
            role: role as? String ?? "Unknown",
            title: title as? String,
            value: (value as? String).flatMap { $0.count < 1000 ? $0 : String($0.prefix(1000)) },
            description: description as? String,
            frame: frame,
            children: children
        )
    }
}
