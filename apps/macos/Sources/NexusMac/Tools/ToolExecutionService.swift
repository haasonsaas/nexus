import AppKit
import Foundation
import OSLog

/// Unified service for executing computer use tools.
/// Coordinates mouse, keyboard, screen capture, and MCP tools.
@MainActor
@Observable
final class ToolExecutionService {
    static let shared = ToolExecutionService()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "tools")

    private let mouse = MouseController.shared
    private let keyboard = KeyboardController.shared
    private let screen = ScreenCaptureService.shared
    private let mcp = MCPServerRegistry.shared

    private(set) var isExecuting = false
    private(set) var lastResult: ToolResult?

    struct ToolResult {
        let toolName: String
        let success: Bool
        let output: Any?
        let error: Error?
        let timestamp: Date
    }

    // MARK: - Computer Use Tools

    /// Execute a computer use action
    func executeComputerUse(_ action: ComputerUseAction) async throws -> ToolResult {
        isExecuting = true
        defer { isExecuting = false }

        logger.info("tool executing action=\(action.name)")

        let result: ToolResult
        do {
            let output = try await performAction(action)
            result = ToolResult(
                toolName: action.name,
                success: true,
                output: output,
                error: nil,
                timestamp: Date()
            )
        } catch {
            result = ToolResult(
                toolName: action.name,
                success: false,
                output: nil,
                error: error,
                timestamp: Date()
            )
            logger.error("tool execution failed: \(error.localizedDescription)")
            throw error
        }

        lastResult = result
        return result
    }

    private func performAction(_ action: ComputerUseAction) async throws -> Any? {
        switch action {
        case .screenshot(let options):
            let captureOptions = ScreenCaptureService.CaptureOptions(
                scale: options.scale ?? 1.0,
                includesCursor: options.includeCursor ?? true,
                format: .png
            )
            let result = try await screen.capture(options: captureOptions)
            return ["data": result.data.base64EncodedString(), "bounds": result.bounds]

        case .click(let x, let y, let button, let count):
            var options = MouseController.ClickOptions()
            options.button = button == "right" ? .right : .left
            options.clickCount = count ?? 1
            await mouse.clickAt(x: CGFloat(x), y: CGFloat(y), options: options)
            return nil

        case .doubleClick(let x, let y):
            await mouse.doubleClick(x: CGFloat(x), y: CGFloat(y))
            return nil

        case .rightClick(let x, let y):
            await mouse.rightClick(x: CGFloat(x), y: CGFloat(y))
            return nil

        case .moveMouse(let x, let y):
            mouse.moveTo(x: CGFloat(x), y: CGFloat(y))
            return nil

        case .drag(let fromX, let fromY, let toX, let toY, let duration):
            await mouse.drag(
                from: CGPoint(x: CGFloat(fromX), y: CGFloat(fromY)),
                to: CGPoint(x: CGFloat(toX), y: CGFloat(toY)),
                duration: duration ?? 0.3
            )
            return nil

        case .scroll(let deltaX, let deltaY):
            mouse.scroll(deltaX: Int32(deltaX ?? 0), deltaY: Int32(deltaY))
            return nil

        case .type(let text, let delay):
            await keyboard.type(text, delayBetweenKeys: delay ?? 0.02)
            return nil

        case .key(let keyName, let modifiers):
            guard let keyCode = KeyCode.from(name: keyName) else {
                throw ToolError.invalidKey(keyName)
            }
            var flags: CGEventFlags = []
            if modifiers?.contains("command") == true { flags.insert(.maskCommand) }
            if modifiers?.contains("shift") == true { flags.insert(.maskShift) }
            if modifiers?.contains("option") == true { flags.insert(.maskAlternate) }
            if modifiers?.contains("control") == true { flags.insert(.maskControl) }
            await keyboard.pressKey(keyCode, modifiers: flags)
            return nil

        case .shortcut(let shortcutName):
            try await executeShortcut(shortcutName)
            return nil

        case .listWindows:
            let windows = screen.listWindows()
            return windows.map { [
                "id": $0.windowID,
                "owner": $0.ownerName,
                "title": $0.title ?? "",
                "bounds": ["x": $0.bounds.origin.x, "y": $0.bounds.origin.y, "width": $0.bounds.width, "height": $0.bounds.height]
            ] }

        case .listDisplays:
            let displays = screen.listDisplays()
            return displays.map { [
                "id": $0.displayID,
                "name": $0.displayName,
                "isMain": $0.isMain,
                "bounds": ["x": $0.bounds.origin.x, "y": $0.bounds.origin.y, "width": $0.bounds.width, "height": $0.bounds.height]
            ] }

        case .captureWindow(let windowID):
            let result = try await screen.captureWindow(windowID: CGWindowID(windowID))
            return ["data": result.data.base64EncodedString(), "bounds": result.bounds]

        case .getMousePosition:
            let pos = mouse.currentPosition()
            return ["x": pos.x, "y": pos.y]

        case .clipboard(let operation, let content):
            return try await handleClipboard(operation, content: content)
        }
    }

    private func executeShortcut(_ name: String) async throws {
        switch name.lowercased() {
        case "copy": await keyboard.copy()
        case "paste": await keyboard.paste()
        case "cut": await keyboard.cut()
        case "selectall", "select_all": await keyboard.selectAll()
        case "undo": await keyboard.undo()
        case "redo": await keyboard.redo()
        case "save": await keyboard.save()
        case "tab": await keyboard.tab()
        case "enter", "return": await keyboard.enter()
        case "escape", "esc": await keyboard.escape()
        case "backspace", "delete": await keyboard.backspace()
        default:
            throw ToolError.unknownShortcut(name)
        }
    }

    private func handleClipboard(_ operation: String, content: String?) async throws -> Any? {
        switch operation {
        case "get", "read":
            return NSPasteboard.general.string(forType: .string)

        case "set", "write":
            guard let content else {
                throw ToolError.missingParameter("content")
            }
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(content, forType: .string)
            return nil

        case "clear":
            NSPasteboard.general.clearContents()
            return nil

        default:
            throw ToolError.unknownClipboardOperation(operation)
        }
    }

    // MARK: - MCP Tools

    /// Execute an MCP tool
    func executeMCPTool(serverId: String, toolName: String, arguments: [String: Any]) async throws -> ToolResult {
        isExecuting = true
        defer { isExecuting = false }

        logger.info("mcp tool executing server=\(serverId) tool=\(toolName)")

        let result: ToolResult
        do {
            let data = try await mcp.callTool(serverId: serverId, toolName: toolName, arguments: arguments)
            result = ToolResult(
                toolName: "\(serverId)/\(toolName)",
                success: true,
                output: data,
                error: nil,
                timestamp: Date()
            )
        } catch {
            result = ToolResult(
                toolName: "\(serverId)/\(toolName)",
                success: false,
                output: nil,
                error: error,
                timestamp: Date()
            )
            throw error
        }

        lastResult = result
        return result
    }
}

/// Computer use actions that can be executed
enum ComputerUseAction {
    case screenshot(ScreenshotOptions)
    case click(x: Int, y: Int, button: String?, count: Int?)
    case doubleClick(x: Int, y: Int)
    case rightClick(x: Int, y: Int)
    case moveMouse(x: Int, y: Int)
    case drag(fromX: Int, fromY: Int, toX: Int, toY: Int, duration: TimeInterval?)
    case scroll(deltaX: Int?, deltaY: Int)
    case type(text: String, delay: TimeInterval?)
    case key(name: String, modifiers: [String]?)
    case shortcut(name: String)
    case listWindows
    case listDisplays
    case captureWindow(windowID: UInt32)
    case getMousePosition
    case clipboard(operation: String, content: String?)

    var name: String {
        switch self {
        case .screenshot: return "screenshot"
        case .click: return "click"
        case .doubleClick: return "double_click"
        case .rightClick: return "right_click"
        case .moveMouse: return "move_mouse"
        case .drag: return "drag"
        case .scroll: return "scroll"
        case .type: return "type"
        case .key: return "key"
        case .shortcut: return "shortcut"
        case .listWindows: return "list_windows"
        case .listDisplays: return "list_displays"
        case .captureWindow: return "capture_window"
        case .getMousePosition: return "get_mouse_position"
        case .clipboard: return "clipboard"
        }
    }

    struct ScreenshotOptions {
        var scale: CGFloat?
        var includeCursor: Bool?
        var displayId: UInt32?
        var region: (x: Int, y: Int, width: Int, height: Int)?
    }
}

enum ToolError: LocalizedError {
    case invalidKey(String)
    case unknownShortcut(String)
    case unknownClipboardOperation(String)
    case missingParameter(String)

    var errorDescription: String? {
        switch self {
        case .invalidKey(let name):
            return "Invalid key: \(name)"
        case .unknownShortcut(let name):
            return "Unknown shortcut: \(name)"
        case .unknownClipboardOperation(let op):
            return "Unknown clipboard operation: \(op)"
        case .missingParameter(let name):
            return "Missing required parameter: \(name)"
        }
    }
}

// MARK: - KeyCode Extension

extension KeyCode {
    static func from(name: String) -> KeyCode? {
        switch name.lowercased() {
        case "a": return .a
        case "b": return .b
        case "c": return .c
        case "d": return .d
        case "e": return .e
        case "f": return .f
        case "g": return .g
        case "h": return .h
        case "i": return .i
        case "j": return .j
        case "k": return .k
        case "l": return .l
        case "m": return .m
        case "n": return .n
        case "o": return .o
        case "p": return .p
        case "q": return .q
        case "r": return .r
        case "s": return .s
        case "t": return .t
        case "u": return .u
        case "v": return .v
        case "w": return .w
        case "x": return .x
        case "y": return .y
        case "z": return .z
        case "0", "zero": return .zero
        case "1", "one": return .one
        case "2", "two": return .two
        case "3", "three": return .three
        case "4", "four": return .four
        case "5", "five": return .five
        case "6", "six": return .six
        case "7", "seven": return .seven
        case "8", "eight": return .eight
        case "9", "nine": return .nine
        case "return", "enter": return .returnKey
        case "tab": return .tab
        case "space": return .space
        case "delete", "backspace": return .delete
        case "escape", "esc": return .escape
        case "left", "leftarrow": return .leftArrow
        case "right", "rightarrow": return .rightArrow
        case "up", "uparrow": return .upArrow
        case "down", "downarrow": return .downArrow
        case "home": return .home
        case "end": return .end
        case "pageup": return .pageUp
        case "pagedown": return .pageDown
        case "f1": return .f1
        case "f2": return .f2
        case "f3": return .f3
        case "f4": return .f4
        case "f5": return .f5
        case "f6": return .f6
        case "f7": return .f7
        case "f8": return .f8
        case "f9": return .f9
        case "f10": return .f10
        case "f11": return .f11
        case "f12": return .f12
        default: return nil
        }
    }
}
