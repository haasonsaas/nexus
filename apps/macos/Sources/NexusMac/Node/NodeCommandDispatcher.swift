import AppKit
import Foundation
import OSLog

/// Command types supported by node mode
enum NodeCommand: String, CaseIterable, Sendable {
    case systemRun = "system.run"
    case screenCapture = "screen.capture"
    case canvasOpen = "canvas.open"
    case canvasClose = "canvas.close"
    case canvasEval = "canvas.eval"
    case canvasSnapshot = "canvas.snapshot"
    case cameraCapture = "camera.capture"
    case locationGet = "location.get"
    case clipboardRead = "clipboard.read"
    case clipboardWrite = "clipboard.write"
    case fileRead = "file.read"
    case fileWrite = "file.write"
    case fileList = "file.list"
    case notify = "notify"
}

/// Result from command execution
struct NodeCommandResult: Codable, Sendable {
    let success: Bool
    let data: [String: AnyCodable]?
    let error: String?

    static func ok(_ data: [String: AnyCodable]? = nil) -> NodeCommandResult {
        NodeCommandResult(success: true, data: data, error: nil)
    }

    static func fail(_ error: String) -> NodeCommandResult {
        NodeCommandResult(success: false, data: nil, error: error)
    }
}

/// Dispatches commands from gateway to appropriate handlers
@MainActor
@Observable
final class NodeCommandDispatcher {
    static let shared = NodeCommandDispatcher()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "node-dispatch")
    private static let iso8601Formatter: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter
    }()

    // Capabilities
    private(set) var capabilities: Set<NodeCommand> = []

    // Handlers
    private var handlers: [NodeCommand: @Sendable ([String: AnyCodable]) async throws -> NodeCommandResult] = [:]

    init() {
        registerDefaultHandlers()
        updateCapabilities()
    }

    // MARK: - Capability Advertisement

    func currentCapabilities() -> [String] {
        capabilities.map { $0.rawValue }
    }

    func updateCapabilities() {
        var caps: Set<NodeCommand> = []

        // Always available
        caps.insert(.systemRun)
        caps.insert(.notify)
        caps.insert(.clipboardRead)
        caps.insert(.clipboardWrite)

        // Check screen recording permission
        if PermissionManager.shared.status(for: .screenRecording) {
            caps.insert(.screenCapture)
        }

        // Check camera permission
        if PermissionManager.shared.status(for: .camera) {
            caps.insert(.cameraCapture)
        }

        // Canvas always available
        caps.insert(.canvasOpen)
        caps.insert(.canvasClose)
        caps.insert(.canvasEval)
        caps.insert(.canvasSnapshot)

        // File operations
        caps.insert(.fileRead)
        caps.insert(.fileWrite)
        caps.insert(.fileList)

        // Location if node mode is enabled (proxy for location being configured)
        if AppStateStore.shared.nodeModeEnabled {
            caps.insert(.locationGet)
        }

        capabilities = caps
        logger.info("Updated capabilities: \(caps.map { $0.rawValue }.joined(separator: ", "))")
    }

    // MARK: - Handler Registration

    func register(_ command: NodeCommand, handler: @escaping @Sendable ([String: AnyCodable]) async throws -> NodeCommandResult) {
        handlers[command] = handler
        logger.debug("Registered handler for \(command.rawValue)")
    }

    private func registerDefaultHandlers() {
        // System run
        register(.systemRun) { [weak self] params in
            guard let self else { return .fail("Dispatcher deallocated") }
            return await self.handleSystemRun(params)
        }

        // Screen capture
        register(.screenCapture) { _ in
            let result = try await MainActor.run {
                ScreenCaptureService.shared
            }.capture()
            return .ok(["image": AnyCodable(result.data.base64EncodedString())])
        }

        // Canvas operations
        register(.canvasOpen) { params in
            guard let urlString = params["url"]?.value as? String,
                  let url = URL(string: urlString) else {
                return .fail("Missing or invalid url parameter")
            }
            let title = params["title"]?.value as? String
            let width = params["width"]?.value as? CGFloat ?? 800
            let height = params["height"]?.value as? CGFloat ?? 600
            let size = CGSize(width: width, height: height)
            let sessionId = params["session_id"]?.value as? String ?? UUID().uuidString

            await MainActor.run {
                CanvasManager.shared.openURL(sessionId: sessionId, url: url, title: title, size: size)
            }
            return .ok(["session_id": AnyCodable(sessionId)])
        }

        register(.canvasClose) { params in
            let sessionId = params["session_id"]?.value as? String ?? "default"
            await MainActor.run {
                CanvasManager.shared.close(sessionId: sessionId)
            }
            return .ok()
        }

        register(.canvasEval) { params in
            guard let js = params["script"]?.value as? String else {
                return .fail("Missing script parameter")
            }
            let sessionId = params["session_id"]?.value as? String ?? "default"
            let result = try await MainActor.run {
                try await CanvasManager.shared.executeJS(sessionId: sessionId, script: js)
            }
            if let result {
                return .ok(["result": AnyCodable(result)])
            }
            return .ok()
        }

        register(.canvasSnapshot) { params in
            let sessionId = params["session_id"]?.value as? String ?? "default"
            let image = try await MainActor.run {
                try await CanvasManager.shared.snapshot(sessionId: sessionId)
            }
            guard let image,
                  let tiffData = image.tiffRepresentation,
                  let bitmap = NSBitmapImageRep(data: tiffData),
                  let pngData = bitmap.representation(using: .png, properties: [:]) else {
                return .fail("Snapshot failed")
            }
            return .ok(["image": AnyCodable(pngData.base64EncodedString())])
        }

        // Camera capture
        register(.cameraCapture) { _ in
            let result = try await MainActor.run {
                try await CameraCaptureService.shared.captureFrame()
            }
            return .ok(["image": AnyCodable(result.data.base64EncodedString())])
        }

        // Location
        register(.locationGet) { [weak self] _ in
            guard let self else { return .fail("Dispatcher deallocated") }
            do {
                let location = try await PermissionGuard.shared.execute(requiring: .location) {
                    try await LocationService.shared.requestLocation()
                }
                var payload: [String: AnyCodable] = [
                    "latitude": AnyCodable(location.coordinate.latitude),
                    "longitude": AnyCodable(location.coordinate.longitude),
                    "accuracy": AnyCodable(location.horizontalAccuracy),
                    "timestamp": AnyCodable(Self.iso8601Formatter.string(from: location.timestamp)),
                ]
                if location.verticalAccuracy >= 0 {
                    payload["altitude"] = AnyCodable(location.altitude)
                }
                if location.speed >= 0 {
                    payload["speed"] = AnyCodable(location.speed)
                }
                if location.course >= 0 {
                    payload["course"] = AnyCodable(location.course)
                }
                return .ok(payload)
            } catch let error as PermissionError {
                return .fail(error.localizedDescription)
            } catch {
                self.logger.error("location request failed: \(error.localizedDescription)")
                return .fail("Location request failed: \(error.localizedDescription)")
            }
        }

        // Clipboard
        register(.clipboardRead) { _ in
            let pasteboard = NSPasteboard.general
            if let text = pasteboard.string(forType: .string) {
                return .ok(["content": AnyCodable(text), "type": AnyCodable("text")])
            }
            if let urls = pasteboard.readObjects(forClasses: [NSURL.self]) as? [URL], !urls.isEmpty {
                let paths = urls.map { $0.path }
                return .ok(["content": AnyCodable(paths), "type": AnyCodable("files")])
            }
            return .ok(["content": AnyCodable(""), "type": AnyCodable("empty")])
        }

        register(.clipboardWrite) { params in
            guard let content = params["content"]?.value as? String else {
                return .fail("Missing content parameter")
            }
            let pasteboard = NSPasteboard.general
            pasteboard.clearContents()
            pasteboard.setString(content, forType: .string)
            return .ok()
        }

        // File operations
        register(.fileRead) { params in
            guard let path = params["path"]?.value as? String else {
                return .fail("Missing path parameter")
            }
            let url = URL(fileURLWithPath: path)
            let data = try Data(contentsOf: url)
            // Try to decode as text, otherwise return base64
            if let text = String(data: data, encoding: .utf8) {
                return .ok([
                    "content": AnyCodable(text),
                    "encoding": AnyCodable("utf8"),
                ])
            }
            return .ok([
                "content": AnyCodable(data.base64EncodedString()),
                "encoding": AnyCodable("base64"),
            ])
        }

        register(.fileWrite) { params in
            guard let path = params["path"]?.value as? String else {
                return .fail("Missing path parameter")
            }
            guard let content = params["content"]?.value as? String else {
                return .fail("Missing content parameter")
            }
            let url = URL(fileURLWithPath: path)
            let encoding = params["encoding"]?.value as? String ?? "utf8"

            if encoding == "base64" {
                guard let data = Data(base64Encoded: content) else {
                    return .fail("Invalid base64 content")
                }
                try data.write(to: url, options: .atomic)
            } else {
                try content.write(to: url, atomically: true, encoding: .utf8)
            }
            return .ok()
        }

        register(.fileList) { params in
            guard let path = params["path"]?.value as? String else {
                return .fail("Missing path parameter")
            }
            let url = URL(fileURLWithPath: path)
            let contents = try FileManager.default.contentsOfDirectory(
                at: url,
                includingPropertiesForKeys: [.isDirectoryKey, .fileSizeKey, .contentModificationDateKey]
            )
            let items: [[String: Any]] = try contents.map { itemURL in
                let resourceValues = try itemURL.resourceValues(forKeys: [.isDirectoryKey, .fileSizeKey, .contentModificationDateKey])
                return [
                    "name": itemURL.lastPathComponent,
                    "isDirectory": resourceValues.isDirectory ?? false,
                    "size": resourceValues.fileSize ?? 0,
                    "modifiedAt": (resourceValues.contentModificationDate?.timeIntervalSince1970 ?? 0) * 1000,
                ]
            }
            return .ok(["items": AnyCodable(items)])
        }

        // Notify
        register(.notify) { params in
            let title = params["title"]?.value as? String ?? "Nexus"
            let body = params["body"]?.value as? String ?? ""
            try await MainActor.run {
                try await NotificationBridge.shared.send(title: title, body: body)
            }
            return .ok()
        }
    }

    // MARK: - System Run Handler

    private func handleSystemRun(_ params: [String: AnyCodable]) async -> NodeCommandResult {
        guard let command = params["command"]?.value as? String else {
            return .fail("Missing command parameter")
        }

        let args = (params["args"]?.value as? [String]) ?? []
        let workingDir = params["working_directory"]?.value as? String
        let env = params["environment"]?.value as? [String: String]
        let skipApproval = params["skip_approval"]?.value as? Bool ?? false

        // Build full command for approval check
        let fullCommand = ([command] + args).joined(separator: " ")

        // Check exec approvals unless skipped
        if !skipApproval {
            let decision = await checkExecApproval(command: fullCommand, cwd: workingDir)
            switch decision {
            case .allowOnce, .allowAlways:
                break
            case .deny:
                return .fail("Command execution denied by user")
            }
        }

        // Execute
        let process = Process()
        process.executableURL = URL(fileURLWithPath: command)
        process.arguments = args

        if let wd = workingDir {
            process.currentDirectoryURL = URL(fileURLWithPath: wd)
        }

        if let env {
            process.environment = ProcessInfo.processInfo.environment.merging(env) { _, new in new }
        }

        let stdout = Pipe()
        let stderr = Pipe()
        process.standardOutput = stdout
        process.standardError = stderr

        do {
            try process.run()
            process.waitUntilExit()

            let outData = stdout.fileHandleForReading.readDataToEndOfFile()
            let errData = stderr.fileHandleForReading.readDataToEndOfFile()

            return .ok([
                "exit_code": AnyCodable(Int(process.terminationStatus)),
                "stdout": AnyCodable(String(data: outData, encoding: .utf8) ?? ""),
                "stderr": AnyCodable(String(data: errData, encoding: .utf8) ?? ""),
            ])
        } catch {
            return .fail("Execution failed: \(error.localizedDescription)")
        }
    }

    private func checkExecApproval(command: String, cwd: String?) async -> ExecApprovalDecision {
        // Use the ExecApprovalsPromptPresenter for approval
        let request = ExecApprovalPromptRequest(
            command: command,
            cwd: cwd,
            host: Host.current().localizedName,
            security: nil,
            ask: nil,
            agentId: nil,
            resolvedPath: nil,
            sessionKey: nil
        )
        return await ExecApprovalsPromptPresenter.prompt(request)
    }

    // MARK: - Dispatch

    func dispatch(method: String, params: [String: AnyCodable]) async -> NodeCommandResult {
        guard let command = NodeCommand(rawValue: method) else {
            logger.warning("Unknown command: \(method)")
            return .fail("Unknown command: \(method)")
        }

        guard capabilities.contains(command) else {
            logger.warning("Command not available: \(method)")
            return .fail("Command not available: \(method)")
        }

        guard let handler = handlers[command] else {
            logger.error("No handler for command: \(method)")
            return .fail("No handler for command: \(method)")
        }

        do {
            logger.info("Dispatching: \(method)")
            return try await handler(params)
        } catch {
            logger.error("Command failed: \(error.localizedDescription)")
            return .fail(error.localizedDescription)
        }
    }

    // MARK: - Gateway Integration

    func handleInvoke(event: String, payload: Data?) async {
        guard let payload,
              let json = try? JSONSerialization.jsonObject(with: payload) as? [String: Any],
              let method = json["method"] as? String,
              let invokeId = json["invoke_id"] as? String else {
            logger.error("Invalid invoke frame")
            return
        }

        // Parse params
        var params: [String: AnyCodable] = [:]
        if let paramsDict = json["params"] as? [String: Any] {
            params = paramsDict.mapValues { AnyCodable($0) }
        }

        let result = await dispatch(method: method, params: params)

        // Send result back
        do {
            var resultParams: [String: AnyHashable] = [
                "invoke_id": invokeId,
                "success": result.success,
            ]
            if let data = result.data {
                resultParams["data"] = data.mapValues { $0.value as? AnyHashable ?? "" }
            }
            if let error = result.error {
                resultParams["error"] = error
            }

            _ = try await ControlChannel.shared.request(
                method: "node.invoke_result",
                params: resultParams
            )
        } catch {
            logger.error("Failed to send invoke result: \(error.localizedDescription)")
        }
    }
}

// MARK: - Gateway Event Integration

extension NodeCommandDispatcher {
    /// Subscribe to gateway events and handle node.invoke events
    func startListening() {
        Task {
            let stream = await GatewayConnection.shared.subscribe()
            for await push in stream {
                if case .event(let evt) = push, evt.event == "node.invoke" {
                    await handleInvoke(event: evt.event, payload: evt.payload)
                }
            }
        }
        logger.info("Started listening for node.invoke events")
    }
}
