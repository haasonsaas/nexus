import Foundation

// MARK: - A2UI Action

/// Defines all actions that can be sent from an agent to the UI (A2UI) for canvas control.
/// These actions allow agents to control canvas windows programmatically.
enum A2UIAction: Sendable, Equatable {
    /// Navigate the canvas to a specific URL
    case navigate(url: URL)

    /// Reload the current canvas content
    case reload

    /// Close the canvas window
    case close

    /// Resize the canvas window
    case resize(width: Int, height: Int)

    /// Move the canvas window to a specific position
    case move(x: Int, y: Int)

    /// Bring the canvas window to front and focus it
    case focus

    /// Send the canvas window to back (unfocus)
    case blur

    /// Toggle fullscreen mode
    case fullscreen(enabled: Bool)

    /// Take a screenshot of the canvas
    case screenshot

    /// Execute JavaScript in the canvas WebView
    case executeJS(script: String)

    /// Post a message to the canvas JavaScript context
    case postMessage(message: String, data: [String: Any]?)

    /// Set the canvas window title
    case setTitle(title: String)

    /// Show developer tools (WebView inspector)
    case showDevTools

    /// Print the canvas content
    case print

    // MARK: - Action Type

    /// The string identifier for this action type
    var actionType: String {
        switch self {
        case .navigate: return "navigate"
        case .reload: return "reload"
        case .close: return "close"
        case .resize: return "resize"
        case .move: return "move"
        case .focus: return "focus"
        case .blur: return "blur"
        case .fullscreen: return "fullscreen"
        case .screenshot: return "screenshot"
        case .executeJS: return "executeJS"
        case .postMessage: return "postMessage"
        case .setTitle: return "setTitle"
        case .showDevTools: return "showDevTools"
        case .print: return "print"
        }
    }

    // MARK: - Equatable

    static func == (lhs: A2UIAction, rhs: A2UIAction) -> Bool {
        switch (lhs, rhs) {
        case (.navigate(let lUrl), .navigate(let rUrl)):
            return lUrl == rUrl
        case (.reload, .reload):
            return true
        case (.close, .close):
            return true
        case (.resize(let lw, let lh), .resize(let rw, let rh)):
            return lw == rw && lh == rh
        case (.move(let lx, let ly), .move(let rx, let ry)):
            return lx == rx && ly == ry
        case (.focus, .focus):
            return true
        case (.blur, .blur):
            return true
        case (.fullscreen(let lEnabled), .fullscreen(let rEnabled)):
            return lEnabled == rEnabled
        case (.screenshot, .screenshot):
            return true
        case (.executeJS(let lScript), .executeJS(let rScript)):
            return lScript == rScript
        case (.postMessage(let lMsg, _), .postMessage(let rMsg, _)):
            // Note: We only compare message, not data (which contains Any)
            return lMsg == rMsg
        case (.setTitle(let lTitle), .setTitle(let rTitle)):
            return lTitle == rTitle
        case (.showDevTools, .showDevTools):
            return true
        case (.print, .print):
            return true
        default:
            return false
        }
    }
}

// MARK: - JSON Serialization

extension A2UIAction {
    /// Serialize the action to a JSON-compatible dictionary
    func toJSON() -> [String: Any] {
        var json: [String: Any] = ["action": actionType]

        switch self {
        case .navigate(let url):
            json["url"] = url.absoluteString
        case .resize(let width, let height):
            json["width"] = width
            json["height"] = height
        case .move(let x, let y):
            json["x"] = x
            json["y"] = y
        case .fullscreen(let enabled):
            json["enabled"] = enabled
        case .executeJS(let script):
            json["script"] = script
        case .postMessage(let message, let data):
            json["message"] = message
            if let data {
                json["data"] = data
            }
        case .setTitle(let title):
            json["title"] = title
        case .reload, .close, .focus, .blur, .screenshot, .showDevTools, .print:
            break
        }

        return json
    }

    /// Serialize the action to JSON data
    func toJSONData() throws -> Data {
        try JSONSerialization.data(withJSONObject: toJSON(), options: [])
    }

    /// Create an action from a JSON dictionary
    static func fromJSON(_ json: [String: Any]) -> A2UIAction? {
        guard let action = json["action"] as? String else { return nil }

        switch action {
        case "navigate":
            guard let urlString = json["url"] as? String,
                  let url = URL(string: urlString) else { return nil }
            return .navigate(url: url)

        case "reload":
            return .reload

        case "close":
            return .close

        case "resize":
            guard let width = json["width"] as? Int,
                  let height = json["height"] as? Int else { return nil }
            return .resize(width: width, height: height)

        case "move":
            guard let x = json["x"] as? Int,
                  let y = json["y"] as? Int else { return nil }
            return .move(x: x, y: y)

        case "focus":
            return .focus

        case "blur":
            return .blur

        case "fullscreen":
            guard let enabled = json["enabled"] as? Bool else { return nil }
            return .fullscreen(enabled: enabled)

        case "screenshot":
            return .screenshot

        case "executeJS":
            guard let script = json["script"] as? String else { return nil }
            return .executeJS(script: script)

        case "postMessage":
            guard let message = json["message"] as? String else { return nil }
            let data = json["data"] as? [String: Any]
            return .postMessage(message: message, data: data)

        case "setTitle":
            guard let title = json["title"] as? String else { return nil }
            return .setTitle(title: title)

        case "showDevTools":
            return .showDevTools

        case "print":
            return .print

        default:
            return nil
        }
    }

    /// Create an action from JSON data
    static func fromJSONData(_ data: Data) -> A2UIAction? {
        guard let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            return nil
        }
        return fromJSON(json)
    }
}

// MARK: - Validation

extension A2UIAction {
    /// Validation error types
    enum ValidationError: LocalizedError {
        case invalidURL(String)
        case invalidDimensions(String)
        case invalidPosition(String)
        case emptyScript
        case emptyMessage
        case emptyTitle
        case scriptTooLong(Int)

        var errorDescription: String? {
            switch self {
            case .invalidURL(let url):
                return "Invalid URL: \(url)"
            case .invalidDimensions(let reason):
                return "Invalid dimensions: \(reason)"
            case .invalidPosition(let reason):
                return "Invalid position: \(reason)"
            case .emptyScript:
                return "JavaScript script cannot be empty"
            case .emptyMessage:
                return "Message cannot be empty"
            case .emptyTitle:
                return "Title cannot be empty"
            case .scriptTooLong(let length):
                return "JavaScript script exceeds maximum length (got \(length), max 1000000)"
            }
        }
    }

    /// Maximum allowed script length (1MB)
    private static let maxScriptLength = 1_000_000

    /// Validate the action and return any errors
    func validate() -> ValidationError? {
        switch self {
        case .navigate(let url):
            // Ensure URL has a valid scheme
            guard let scheme = url.scheme,
                  ["http", "https", "file", "data", "about", "canvas"].contains(scheme.lowercased()) else {
                return .invalidURL("Unsupported URL scheme: \(url.scheme ?? "none")")
            }
            return nil

        case .resize(let width, let height):
            if width < 100 {
                return .invalidDimensions("Width must be at least 100 (got \(width))")
            }
            if height < 100 {
                return .invalidDimensions("Height must be at least 100 (got \(height))")
            }
            if width > 10000 {
                return .invalidDimensions("Width must be at most 10000 (got \(width))")
            }
            if height > 10000 {
                return .invalidDimensions("Height must be at most 10000 (got \(height))")
            }
            return nil

        case .move(let x, let y):
            if x < -10000 || x > 10000 {
                return .invalidPosition("X position must be between -10000 and 10000 (got \(x))")
            }
            if y < -10000 || y > 10000 {
                return .invalidPosition("Y position must be between -10000 and 10000 (got \(y))")
            }
            return nil

        case .executeJS(let script):
            if script.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                return .emptyScript
            }
            if script.count > Self.maxScriptLength {
                return .scriptTooLong(script.count)
            }
            return nil

        case .postMessage(let message, _):
            if message.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                return .emptyMessage
            }
            return nil

        case .setTitle(let title):
            if title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                return .emptyTitle
            }
            return nil

        case .reload, .close, .focus, .blur, .fullscreen, .screenshot, .showDevTools, .print:
            return nil
        }
    }

    /// Check if the action is valid
    var isValid: Bool {
        validate() == nil
    }
}

// MARK: - Action Result

/// Result of an A2UI action execution
struct A2UIActionResult: Sendable {
    /// Whether the action succeeded
    let success: Bool

    /// Optional error message if the action failed
    let error: String?

    /// Optional result data (e.g., screenshot data, JS evaluation result)
    let data: [String: AnyCodable]?

    /// Timestamp when the action completed
    let timestamp: Date

    /// The action that was executed
    let action: A2UIAction

    init(success: Bool, error: String? = nil, data: [String: AnyCodable]? = nil, action: A2UIAction) {
        self.success = success
        self.error = error
        self.data = data
        self.action = action
        self.timestamp = Date()
    }

    /// Create a success result
    static func success(action: A2UIAction, data: [String: AnyCodable]? = nil) -> A2UIActionResult {
        A2UIActionResult(success: true, data: data, action: action)
    }

    /// Create a failure result
    static func failure(action: A2UIAction, error: String) -> A2UIActionResult {
        A2UIActionResult(success: false, error: error, action: action)
    }

    /// Convert to JSON for transmission
    func toJSON() -> [String: Any] {
        var json: [String: Any] = [
            "success": success,
            "timestamp": timestamp.timeIntervalSince1970,
            "action": action.toJSON()
        ]
        if let error {
            json["error"] = error
        }
        if let data {
            json["data"] = data.mapValues { $0.value }
        }
        return json
    }
}

// MARK: - Batch Action

/// A batch of A2UI actions to execute sequentially
struct A2UIActionBatch: Sendable {
    /// Unique identifier for this batch
    let id: String

    /// Actions to execute in order
    let actions: [A2UIAction]

    /// Whether to stop on first error
    let stopOnError: Bool

    /// Target session ID
    let sessionId: String

    init(id: String = UUID().uuidString, actions: [A2UIAction], sessionId: String, stopOnError: Bool = true) {
        self.id = id
        self.actions = actions
        self.sessionId = sessionId
        self.stopOnError = stopOnError
    }

    /// Create from JSON
    static func fromJSON(_ json: [String: Any]) -> A2UIActionBatch? {
        guard let sessionId = json["sessionId"] as? String,
              let actionsJSON = json["actions"] as? [[String: Any]] else {
            return nil
        }

        let actions = actionsJSON.compactMap { A2UIAction.fromJSON($0) }
        guard !actions.isEmpty else { return nil }

        let id = json["id"] as? String ?? UUID().uuidString
        let stopOnError = json["stopOnError"] as? Bool ?? true

        return A2UIActionBatch(id: id, actions: actions, sessionId: sessionId, stopOnError: stopOnError)
    }
}
