import AppKit
import Carbon
import CoreGraphics
import Foundation
import OSLog

/// Controller for programmatic keyboard operations.
/// Used by computer use agents to type text and send key commands.
@MainActor
final class KeyboardController {
    static let shared = KeyboardController()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "keyboard")

    /// Type a string of text
    func type(_ text: String, delayBetweenKeys: TimeInterval = 0.02) async {
        for character in text {
            await typeCharacter(character)
            if delayBetweenKeys > 0 {
                try? await Task.sleep(nanoseconds: UInt64(delayBetweenKeys * 1_000_000_000))
            }
        }
        logger.debug("keyboard typed \(text.count) characters")
    }

    /// Type a single character
    func typeCharacter(_ character: Character) async {
        let string = String(character)

        // Use CGEvent for reliable text input
        guard let source = CGEventSource(stateID: .hidSystemState) else { return }

        if let keyDown = CGEvent(keyboardEventSource: source, virtualKey: 0, keyDown: true) {
            var unicodeString = Array(string.utf16)
            keyDown.keyboardSetUnicodeString(stringLength: unicodeString.count, unicodeString: &unicodeString)
            keyDown.post(tap: .cghidEventTap)
        }

        if let keyUp = CGEvent(keyboardEventSource: source, virtualKey: 0, keyDown: false) {
            keyUp.post(tap: .cghidEventTap)
        }
    }

    /// Press a specific key with optional modifiers
    func pressKey(_ key: KeyCode, modifiers: CGEventFlags = []) async {
        guard let source = CGEventSource(stateID: .hidSystemState) else { return }

        // Key down
        if let keyDown = CGEvent(keyboardEventSource: source, virtualKey: key.rawValue, keyDown: true) {
            if !modifiers.isEmpty {
                keyDown.flags = modifiers
            }
            keyDown.post(tap: .cghidEventTap)
        }

        // Small delay
        try? await Task.sleep(nanoseconds: 10_000_000)

        // Key up
        if let keyUp = CGEvent(keyboardEventSource: source, virtualKey: key.rawValue, keyDown: false) {
            if !modifiers.isEmpty {
                keyUp.flags = modifiers
            }
            keyUp.post(tap: .cghidEventTap)
        }

        logger.debug("keyboard pressed key=\(key.rawValue) modifiers=\(modifiers.rawValue)")
    }

    /// Execute a key binding (e.g., Cmd+C)
    func shortcut(_ key: KeyCode, command: Bool = false, shift: Bool = false, option: Bool = false, control: Bool = false) async {
        var modifiers: CGEventFlags = []
        if command { modifiers.insert(.maskCommand) }
        if shift { modifiers.insert(.maskShift) }
        if option { modifiers.insert(.maskAlternate) }
        if control { modifiers.insert(.maskControl) }

        await pressKey(key, modifiers: modifiers)
    }

    /// Common shortcuts
    func copy() async {
        await shortcut(.c, command: true)
    }

    func paste() async {
        await shortcut(.v, command: true)
    }

    func cut() async {
        await shortcut(.x, command: true)
    }

    func selectAll() async {
        await shortcut(.a, command: true)
    }

    func undo() async {
        await shortcut(.z, command: true)
    }

    func redo() async {
        await shortcut(.z, command: true, shift: true)
    }

    func save() async {
        await shortcut(.s, command: true)
    }

    func tab() async {
        await pressKey(.tab)
    }

    func enter() async {
        await pressKey(.returnKey)
    }

    func escape() async {
        await pressKey(.escape)
    }

    func backspace() async {
        await pressKey(.delete)
    }
}

/// Virtual key codes for common keys
enum KeyCode: UInt16 {
    case a = 0x00
    case s = 0x01
    case d = 0x02
    case f = 0x03
    case h = 0x04
    case g = 0x05
    case z = 0x06
    case x = 0x07
    case c = 0x08
    case v = 0x09
    case b = 0x0B
    case q = 0x0C
    case w = 0x0D
    case e = 0x0E
    case r = 0x0F
    case y = 0x10
    case t = 0x11
    case one = 0x12
    case two = 0x13
    case three = 0x14
    case four = 0x15
    case six = 0x16
    case five = 0x17
    case equal = 0x18
    case nine = 0x19
    case seven = 0x1A
    case minus = 0x1B
    case eight = 0x1C
    case zero = 0x1D
    case rightBracket = 0x1E
    case o = 0x1F
    case u = 0x20
    case leftBracket = 0x21
    case i = 0x22
    case p = 0x23
    case returnKey = 0x24
    case l = 0x25
    case j = 0x26
    case quote = 0x27
    case k = 0x28
    case semicolon = 0x29
    case backslash = 0x2A
    case comma = 0x2B
    case slash = 0x2C
    case n = 0x2D
    case m = 0x2E
    case period = 0x2F
    case tab = 0x30
    case space = 0x31
    case grave = 0x32
    case delete = 0x33
    case escape = 0x35
    case command = 0x37
    case shift = 0x38
    case capsLock = 0x39
    case option = 0x3A
    case control = 0x3B
    case rightShift = 0x3C
    case rightOption = 0x3D
    case rightControl = 0x3E
    case function = 0x3F
    case f17 = 0x40
    case volumeUp = 0x48
    case volumeDown = 0x49
    case mute = 0x4A
    case f18 = 0x4F
    case f19 = 0x50
    case f20 = 0x5A
    case f5 = 0x60
    case f6 = 0x61
    case f7 = 0x62
    case f3 = 0x63
    case f8 = 0x64
    case f9 = 0x65
    case f11 = 0x67
    case f13 = 0x69
    case f16 = 0x6A
    case f14 = 0x6B
    case f10 = 0x6D
    case f12 = 0x6F
    case f15 = 0x71
    case home = 0x73
    case pageUp = 0x74
    case forwardDelete = 0x75
    case f4 = 0x76
    case end = 0x77
    case f2 = 0x78
    case pageDown = 0x79
    case f1 = 0x7A
    case leftArrow = 0x7B
    case rightArrow = 0x7C
    case downArrow = 0x7D
    case upArrow = 0x7E
}
