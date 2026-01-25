import AppKit
import Carbon
import OSLog

@MainActor
final class GlobalHotkeyManager {
    static let shared = GlobalHotkeyManager()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "hotkeys")
    private var registeredHotkeys: [UInt32: HotkeyBinding] = [:]
    private var nextId: UInt32 = 1

    struct HotkeyBinding {
        let id: UInt32
        let keyCode: UInt32
        let modifiers: UInt32
        let action: () -> Void
        var eventHotKey: EventHotKeyRef?
    }

    /// Register a global hotkey
    func register(keyCode: UInt32, modifiers: UInt32, action: @escaping () -> Void) -> UInt32? {
        let id = nextId
        nextId += 1

        var hotKeyID = EventHotKeyID(signature: fourCharCode("NXUS"), id: id)
        var hotKeyRef: EventHotKeyRef?

        let status = RegisterEventHotKey(
            keyCode,
            modifiers,
            hotKeyID,
            GetApplicationEventTarget(),
            0,
            &hotKeyRef
        )

        guard status == noErr, let ref = hotKeyRef else {
            logger.error("Failed to register hotkey: \(status)")
            return nil
        }

        registeredHotkeys[id] = HotkeyBinding(
            id: id,
            keyCode: keyCode,
            modifiers: modifiers,
            action: action,
            eventHotKey: ref
        )

        return id
    }

    /// Unregister a hotkey by ID
    func unregister(id: UInt32) {
        guard let binding = registeredHotkeys.removeValue(forKey: id),
              let ref = binding.eventHotKey else { return }
        UnregisterEventHotKey(ref)
    }

    /// Handle hotkey event
    func handleHotKeyEvent(id: UInt32) {
        registeredHotkeys[id]?.action()
    }

    private func fourCharCode(_ string: String) -> OSType {
        var result: OSType = 0
        for char in string.utf8.prefix(4) {
            result = (result << 8) + OSType(char)
        }
        return result
    }
}
