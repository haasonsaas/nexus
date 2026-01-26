import Carbon.HIToolbox
import AppKit
import Foundation

// MARK: - Hotkey Types

/// Represents a keyboard modifier combination
struct HotkeyModifiers: OptionSet, Codable, Hashable {
    let rawValue: UInt32

    static let command = HotkeyModifiers(rawValue: UInt32(cmdKey))
    static let shift = HotkeyModifiers(rawValue: UInt32(shiftKey))
    static let option = HotkeyModifiers(rawValue: UInt32(optionKey))
    static let control = HotkeyModifiers(rawValue: UInt32(controlKey))

    /// Convert to Carbon modifier flags
    var carbonModifiers: UInt32 { rawValue }

    /// Human-readable string representation
    var displayString: String {
        var parts: [String] = []
        if contains(.control) { parts.append("Ctrl") }
        if contains(.option) { parts.append("Opt") }
        if contains(.shift) { parts.append("Shift") }
        if contains(.command) { parts.append("Cmd") }
        return parts.joined(separator: "+")
    }
}

/// Represents a hotkey action that can be triggered
enum HotkeyAction: String, CaseIterable, Codable, Identifiable {
    case screenshot = "screenshot"
    case click = "click"
    case cursorPosition = "cursor_position"
    case typeClipboard = "type_clipboard"

    var id: String { rawValue }

    var displayName: String {
        switch self {
        case .screenshot: return "Take Screenshot"
        case .click: return "Click at Cursor"
        case .cursorPosition: return "Show Pointer Position"
        case .typeClipboard: return "Type Clipboard Contents"
        }
    }

    var description: String {
        switch self {
        case .screenshot: return "Capture a screenshot from the remote node"
        case .click: return "Perform a left-click at the current cursor position"
        case .cursorPosition: return "Display the current cursor coordinates"
        case .typeClipboard: return "Type the contents of the clipboard"
        }
    }

    /// Default key code for each action
    var defaultKeyCode: UInt32 {
        switch self {
        case .screenshot: return UInt32(kVK_ANSI_S)    // S
        case .click: return UInt32(kVK_ANSI_C)         // C (Note: This is used since we're using Cmd+Shift)
        case .cursorPosition: return UInt32(kVK_ANSI_P) // P
        case .typeClipboard: return UInt32(kVK_ANSI_T) // T
        }
    }

    /// Default modifiers for each action
    var defaultModifiers: HotkeyModifiers {
        [.command, .shift]
    }
}

/// Represents a configured hotkey binding
struct HotkeyBinding: Codable, Identifiable, Hashable {
    let action: HotkeyAction
    var keyCode: UInt32
    var modifiers: HotkeyModifiers
    var isEnabled: Bool

    var id: String { action.rawValue }

    /// Human-readable display string for the key
    var keyDisplayString: String {
        let keyName = Self.keyCodeToString(keyCode)
        if modifiers.rawValue == 0 {
            return keyName
        }
        return "\(modifiers.displayString)+\(keyName)"
    }

    /// Convert a key code to its string representation
    static func keyCodeToString(_ keyCode: UInt32) -> String {
        let keyMap: [UInt32: String] = [
            UInt32(kVK_ANSI_A): "A", UInt32(kVK_ANSI_S): "S", UInt32(kVK_ANSI_D): "D",
            UInt32(kVK_ANSI_F): "F", UInt32(kVK_ANSI_H): "H", UInt32(kVK_ANSI_G): "G",
            UInt32(kVK_ANSI_Z): "Z", UInt32(kVK_ANSI_X): "X", UInt32(kVK_ANSI_C): "C",
            UInt32(kVK_ANSI_V): "V", UInt32(kVK_ANSI_B): "B", UInt32(kVK_ANSI_Q): "Q",
            UInt32(kVK_ANSI_W): "W", UInt32(kVK_ANSI_E): "E", UInt32(kVK_ANSI_R): "R",
            UInt32(kVK_ANSI_Y): "Y", UInt32(kVK_ANSI_T): "T", UInt32(kVK_ANSI_1): "1",
            UInt32(kVK_ANSI_2): "2", UInt32(kVK_ANSI_3): "3", UInt32(kVK_ANSI_4): "4",
            UInt32(kVK_ANSI_6): "6", UInt32(kVK_ANSI_5): "5", UInt32(kVK_ANSI_Equal): "=",
            UInt32(kVK_ANSI_9): "9", UInt32(kVK_ANSI_7): "7", UInt32(kVK_ANSI_Minus): "-",
            UInt32(kVK_ANSI_8): "8", UInt32(kVK_ANSI_0): "0", UInt32(kVK_ANSI_RightBracket): "]",
            UInt32(kVK_ANSI_O): "O", UInt32(kVK_ANSI_U): "U", UInt32(kVK_ANSI_LeftBracket): "[",
            UInt32(kVK_ANSI_I): "I", UInt32(kVK_ANSI_P): "P", UInt32(kVK_ANSI_L): "L",
            UInt32(kVK_ANSI_J): "J", UInt32(kVK_ANSI_Quote): "'", UInt32(kVK_ANSI_K): "K",
            UInt32(kVK_ANSI_Semicolon): ";", UInt32(kVK_ANSI_Backslash): "\\",
            UInt32(kVK_ANSI_Comma): ",", UInt32(kVK_ANSI_Slash): "/", UInt32(kVK_ANSI_N): "N",
            UInt32(kVK_ANSI_M): "M", UInt32(kVK_ANSI_Period): ".", UInt32(kVK_ANSI_Grave): "`",
            UInt32(kVK_Space): "Space", UInt32(kVK_Return): "Return", UInt32(kVK_Tab): "Tab",
            UInt32(kVK_Delete): "Delete", UInt32(kVK_Escape): "Escape",
            UInt32(kVK_F1): "F1", UInt32(kVK_F2): "F2", UInt32(kVK_F3): "F3",
            UInt32(kVK_F4): "F4", UInt32(kVK_F5): "F5", UInt32(kVK_F6): "F6",
            UInt32(kVK_F7): "F7", UInt32(kVK_F8): "F8", UInt32(kVK_F9): "F9",
            UInt32(kVK_F10): "F10", UInt32(kVK_F11): "F11", UInt32(kVK_F12): "F12",
        ]
        return keyMap[keyCode] ?? "Key\(keyCode)"
    }

    /// Create default binding for an action
    static func defaultBinding(for action: HotkeyAction) -> HotkeyBinding {
        HotkeyBinding(
            action: action,
            keyCode: action.defaultKeyCode,
            modifiers: action.defaultModifiers,
            isEnabled: true
        )
    }
}

// MARK: - Hotkey Event Handler

/// Global callback for Carbon hotkey events
private var hotkeyEventHandler: EventHandlerRef?
private var hotkeyService: HotkeyService?

/// Carbon event handler callback
private func hotkeyCallback(
    nextHandler: EventHandlerCallRef?,
    event: EventRef?,
    userData: UnsafeMutableRawPointer?
) -> OSStatus {
    guard let event = event else { return OSStatus(eventNotHandledErr) }

    var hotkeyID = EventHotKeyID()
    let status = GetEventParameter(
        event,
        EventParamName(kEventParamDirectObject),
        EventParamType(typeEventHotKeyID),
        nil,
        MemoryLayout<EventHotKeyID>.size,
        nil,
        &hotkeyID
    )

    if status == noErr {
        Task { @MainActor in
            hotkeyService?.handleHotkeyEvent(id: hotkeyID.id)
        }
    }

    return noErr
}

// MARK: - HotkeyService

/// Service for managing global keyboard hotkeys using Carbon APIs
@MainActor
final class HotkeyService: ObservableObject {
    static let shared = HotkeyService()

    /// Published bindings that can be observed by UI
    @Published private(set) var bindings: [HotkeyBinding] = []

    /// Whether global hotkeys are enabled
    @Published var globalHotkeysEnabled: Bool = true {
        didSet {
            UserDefaults.standard.set(globalHotkeysEnabled, forKey: "HotkeyGlobalEnabled")
            if globalHotkeysEnabled {
                registerAllHotkeys()
            } else {
                unregisterAllHotkeys()
            }
        }
    }

    /// Last triggered action (for UI feedback)
    @Published private(set) var lastTriggeredAction: HotkeyAction?
    @Published private(set) var lastTriggeredAt: Date?

    /// Callback for when a hotkey is triggered
    var onHotkeyTriggered: ((HotkeyAction) -> Void)?

    /// Registered hotkey references
    private var hotkeyRefs: [UInt32: EventHotKeyRef] = [:]

    /// Signature for hotkey IDs (4-char code)
    private let hotkeySignature: OSType = {
        // 'NXHK' as a FourCharCode
        let chars: [UInt8] = [0x4E, 0x58, 0x48, 0x4B] // NXHK
        return OSType(chars[0]) << 24 | OSType(chars[1]) << 16 | OSType(chars[2]) << 8 | OSType(chars[3])
    }()

    private init() {
        hotkeyService = self
        loadBindings()
        globalHotkeysEnabled = UserDefaults.standard.object(forKey: "HotkeyGlobalEnabled") as? Bool ?? true

        if globalHotkeysEnabled {
            installEventHandler()
            registerAllHotkeys()
        }
    }

    @MainActor
    deinit {
        unregisterAllHotkeys()
        if let handler = hotkeyEventHandler {
            RemoveEventHandler(handler)
        }
    }

    // MARK: - Binding Management

    /// Load bindings from UserDefaults or create defaults
    private func loadBindings() {
        if let data = UserDefaults.standard.data(forKey: "HotkeyBindings"),
           let decoded = try? JSONDecoder().decode([HotkeyBinding].self, from: data) {
            bindings = decoded
            // Ensure all actions have bindings
            for action in HotkeyAction.allCases {
                if !bindings.contains(where: { $0.action == action }) {
                    bindings.append(HotkeyBinding.defaultBinding(for: action))
                }
            }
        } else {
            // Create default bindings for all actions
            bindings = HotkeyAction.allCases.map { HotkeyBinding.defaultBinding(for: $0) }
        }
        saveBindings()
    }

    /// Save bindings to UserDefaults
    private func saveBindings() {
        if let data = try? JSONEncoder().encode(bindings) {
            UserDefaults.standard.set(data, forKey: "HotkeyBindings")
        }
    }

    /// Update a binding
    func updateBinding(_ binding: HotkeyBinding) {
        if let index = bindings.firstIndex(where: { $0.action == binding.action }) {
            // Unregister old hotkey
            unregisterHotkey(for: bindings[index])

            // Update binding
            bindings[index] = binding
            saveBindings()

            // Register new hotkey if enabled
            if binding.isEnabled && globalHotkeysEnabled {
                registerHotkey(for: binding)
            }
        }
    }

    /// Enable or disable a specific binding
    func setEnabled(_ enabled: Bool, for action: HotkeyAction) {
        if let index = bindings.firstIndex(where: { $0.action == action }) {
            var binding = bindings[index]
            binding.isEnabled = enabled
            updateBinding(binding)
        }
    }

    /// Get binding for a specific action
    func binding(for action: HotkeyAction) -> HotkeyBinding? {
        bindings.first { $0.action == action }
    }

    /// Reset all bindings to defaults
    func resetToDefaults() {
        unregisterAllHotkeys()
        bindings = HotkeyAction.allCases.map { HotkeyBinding.defaultBinding(for: $0) }
        saveBindings()
        if globalHotkeysEnabled {
            registerAllHotkeys()
        }
    }

    // MARK: - Carbon Event Handler

    /// Install the Carbon event handler for hotkeys
    private func installEventHandler() {
        var eventSpec = EventTypeSpec(
            eventClass: OSType(kEventClassKeyboard),
            eventKind: UInt32(kEventHotKeyPressed)
        )

        let status = InstallEventHandler(
            GetApplicationEventTarget(),
            hotkeyCallback,
            1,
            &eventSpec,
            nil,
            &hotkeyEventHandler
        )

        if status != noErr {
            print("Failed to install hotkey event handler: \(status)")
        }
    }

    /// Handle a hotkey event by ID
    func handleHotkeyEvent(id: UInt32) {
        guard globalHotkeysEnabled else { return }

        if let binding = bindings.first(where: { actionID(for: $0.action) == id }),
           binding.isEnabled {
            lastTriggeredAction = binding.action
            lastTriggeredAt = Date()
            onHotkeyTriggered?(binding.action)
        }
    }

    // MARK: - Hotkey Registration

    /// Register all enabled hotkeys
    private func registerAllHotkeys() {
        for binding in bindings where binding.isEnabled {
            registerHotkey(for: binding)
        }
    }

    /// Unregister all hotkeys
    private func unregisterAllHotkeys() {
        for binding in bindings {
            unregisterHotkey(for: binding)
        }
    }

    /// Register a single hotkey
    private func registerHotkey(for binding: HotkeyBinding) {
        let id = actionID(for: binding.action)

        // Skip if already registered
        if hotkeyRefs[id] != nil {
            return
        }

        var hotkeyID = EventHotKeyID(signature: hotkeySignature, id: id)
        var hotkeyRef: EventHotKeyRef?

        let status = RegisterEventHotKey(
            binding.keyCode,
            binding.modifiers.carbonModifiers,
            hotkeyID,
            GetApplicationEventTarget(),
            0,
            &hotkeyRef
        )

        if status == noErr, let ref = hotkeyRef {
            hotkeyRefs[id] = ref
        } else {
            print("Failed to register hotkey for \(binding.action): \(status)")
        }
    }

    /// Unregister a single hotkey
    private func unregisterHotkey(for binding: HotkeyBinding) {
        let id = actionID(for: binding.action)

        if let ref = hotkeyRefs[id] {
            UnregisterEventHotKey(ref)
            hotkeyRefs.removeValue(forKey: id)
        }
    }

    /// Generate a unique ID for each action
    private func actionID(for action: HotkeyAction) -> UInt32 {
        switch action {
        case .screenshot: return 1
        case .click: return 2
        case .cursorPosition: return 3
        case .typeClipboard: return 4
        }
    }

    // MARK: - Accessibility Check

    /// Check if accessibility permissions are granted (required for global hotkeys)
    func checkAccessibilityPermission() -> Bool {
        let options = [kAXTrustedCheckOptionPrompt.takeUnretainedValue(): true] as CFDictionary
        return AXIsProcessTrustedWithOptions(options)
    }

    /// Check accessibility without prompting
    func isAccessibilityGranted() -> Bool {
        return AXIsProcessTrusted()
    }
}
