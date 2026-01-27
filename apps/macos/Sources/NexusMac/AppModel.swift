import AppKit
import Foundation

@MainActor
final class AppModel: ObservableObject {
    enum EdgeServiceStatus: String {
        case notInstalled = "Not Installed"
        case stopped = "Stopped"
        case running = "Running"
        case unknown = "Unknown"
    }

    @Published var baseURL: String
    @Published var apiKey: String
    @Published var status: SystemStatus?
    @Published var nodes: [NodeSummary] = []
    @Published var nodeTools: [String: [NodeToolSummary]] = [:]
    @Published var artifacts: [ArtifactSummary] = []
    @Published var sessions: [SessionSummary] = []
    @Published var sessionMessages: [SessionMessage] = []
    @Published var sessionHasMore: Bool = false
    @Published var providers: [ProviderStatus] = []
    @Published var providerQRImages: [String: NSImage] = [:]
    @Published var skills: [SkillSummary] = []
    @Published var cronEnabled: Bool = false
    @Published var cronJobs: [CronJobSummary] = []
    @Published var edgeServiceStatus: EdgeServiceStatus = .unknown
    @Published var configText: String = ""
    @Published var configPath: String
    @Published var logText: String = ""
    @Published var lastError: String?

    // WebSocket real-time connection
    @Published var webSocketService: WebSocketService?
    @Published var isWebSocketConnected: Bool = false
    @Published var webSocketError: String?
    @Published var lastServerEvent: ServerEvent?
    @Published var serverUptimeMs: Int64 = 0
    @Published var activeToolCalls: [ToolCallEvent] = []
    @Published var recentSessionEvents: [SessionEventPayload] = []

    private let keychain = KeychainStore()
    private let edgeBinary: String
    private let notificationService = NotificationService.shared
    private var defaultsObserver: NSObjectProtocol?

    // Track previous states for change detection
    private var previousGatewayConnected: Bool?
    private var previousNodeStatuses: [String: String] = [:]
    private var webSocketObservation: Task<Void, Never>?

    init() {
        let defaultPort = GatewayEnvironment.gatewayPort()
        let rawBaseURL = UserDefaults.standard.string(forKey: "NexusBaseURL") ?? "http://localhost:\(defaultPort)"
        baseURL = Self.normalizeBaseURL(rawBaseURL)
        apiKey = keychain.read() ?? ""
        configPath = AppModel.defaultConfigPath()
        edgeBinary = ProcessInfo.processInfo.environment["NEXUS_EDGE_BIN"]
            ?? BundledBinaryLocator.path(for: "nexus-edge")
            ?? "nexus-edge"

        // Request notification permission on first launch
        notificationService.requestPermission()

        refreshEdgeServiceStatus()
        loadConfig()
        loadLogs()

        // Initialize WebSocket service
        setupWebSocketService()
        startDefaultsObservation()

        Task {
            await refreshAll()
        }
    }

    deinit {
        if let observer = defaultsObserver {
            NotificationCenter.default.removeObserver(observer)
        }
    }

    // MARK: - WebSocket Management

    private func setupWebSocketService() {
        guard !baseURL.isEmpty && !apiKey.isEmpty else { return }

        let service = WebSocketService(baseURL: baseURL, apiKey: apiKey)
        webSocketService = service

        // Set up event handler
        service.onEvent = { [weak self] event in
            Task { @MainActor in
                self?.handleWebSocketEvent(event)
            }
        }

        // Start observing published properties
        startWebSocketObservation()

        // Connect automatically
        service.connect()
    }

    private func startDefaultsObservation() {
        defaultsObserver = NotificationCenter.default.addObserver(
            forName: UserDefaults.didChangeNotification,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            Task { @MainActor in
                self?.syncDefaults()
            }
        }
    }

    private func syncDefaults() {
        let defaultPort = GatewayEnvironment.gatewayPort()
        let rawBaseURL = UserDefaults.standard.string(forKey: "NexusBaseURL") ?? "http://localhost:\(defaultPort)"
        let normalized = Self.normalizeBaseURL(rawBaseURL)
        let storedKey = keychain.read() ?? ""

        let shouldReconnect = normalized != baseURL || storedKey != apiKey
        baseURL = normalized
        apiKey = storedKey

        if shouldReconnect {
            reconnectWebSocket()
        }
    }

    private func startWebSocketObservation() {
        webSocketObservation?.cancel()
        webSocketObservation = Task { [weak self] in
            guard let self = self else { return }
            while !Task.isCancelled {
                self.syncWebSocketState()
                try? await Task.sleep(nanoseconds: 500_000_000) // 0.5 seconds
            }
        }
    }

    private func syncWebSocketState() {
        guard let service = webSocketService else { return }
        isWebSocketConnected = service.isConnected
        webSocketError = service.connectionError
        activeToolCalls = service.activeToolCalls
        recentSessionEvents = service.recentSessionEvents
        if let snapshot = service.healthSnapshot {
            serverUptimeMs = snapshot.uptimeMs
        }
    }

    private func handleWebSocketEvent(_ event: ServerEvent) {
        lastServerEvent = event

        switch event {
        case .connected(let hello):
            isWebSocketConnected = true
            webSocketError = nil
            if let snapshot = hello.healthSnapshot {
                serverUptimeMs = snapshot.uptimeMs
            }
            notificationService.notifyGatewayConnected()

        case .disconnected(let reason):
            isWebSocketConnected = false
            webSocketError = reason
            notificationService.notifyGatewayDisconnected(reason: reason)

        case .healthUpdate(let snapshot):
            serverUptimeMs = snapshot.uptimeMs

        case .tick:
            // Periodic health check - could trigger light refresh
            break

        case .chatComplete(let complete):
            // Refresh sessions when a chat completes
            Task { await refreshSessions() }
            _ = complete // silence unused warning

        case .toolCall(let toolCall):
            activeToolCalls.append(toolCall)
            if activeToolCalls.count > 10 {
                activeToolCalls.removeFirst()
            }

        case .sessionEvent(let sessionEvent):
            recentSessionEvents.append(sessionEvent)
            if recentSessionEvents.count > 20 {
                recentSessionEvents.removeFirst()
            }
            // Refresh sessions on session events
            Task { await refreshSessions() }

        case .error(let errorEvent):
            notificationService.notifyError(operation: "WebSocket", message: errorEvent.message)

        case .chatChunk, .pong:
            // These are handled internally or don't require action
            break
        }
    }

    func connectWebSocket() {
        if webSocketService == nil {
            setupWebSocketService()
        } else {
            webSocketService?.connect()
        }
    }

    func disconnectWebSocket() {
        webSocketService?.disconnect()
    }

    func reconnectWebSocket() {
        disconnectWebSocket()
        // Recreate the service with current credentials
        webSocketService = nil
        setupWebSocketService()
    }

    func saveSettings() {
        baseURL = Self.normalizeBaseURL(baseURL)
        UserDefaults.standard.set(baseURL, forKey: "NexusBaseURL")
        _ = keychain.write(apiKey)

        // Reconnect WebSocket with new settings
        reconnectWebSocket()
    }

    func refreshAll() async {
        await refreshStatus()
        await refreshNodes()
        await refreshArtifacts()
        await refreshSessions()
        await refreshProviders()
        await refreshSkills()
        await refreshCron()
    }

    func refreshStatus() async {
        guard let api = makeAPI() else { return }
        do {
            status = try await api.fetchStatus()
            // Check for gateway connection status change
            let isConnected = status != nil
            if let previousConnected = previousGatewayConnected {
                if isConnected && !previousConnected {
                    notificationService.notifyGatewayConnected()
                } else if !isConnected && previousConnected {
                    notificationService.notifyGatewayDisconnected()
                }
            }
            previousGatewayConnected = isConnected
            lastError = nil
        } catch {
            // Gateway disconnected or error
            if previousGatewayConnected == true {
                notificationService.notifyGatewayDisconnected(reason: error.localizedDescription)
            }
            previousGatewayConnected = false
            lastError = error.localizedDescription
        }
    }

    func refreshNodes() async {
        guard let api = makeAPI() else { return }
        do {
            let newNodes = try await api.fetchNodes()

            // Check for node status changes
            for node in newNodes {
                let previousStatus = previousNodeStatuses[node.edgeId]
                let currentStatus = node.status

                if let prevStatus = previousStatus {
                    // Node went online
                    if prevStatus != "online" && currentStatus == "online" {
                        notificationService.notifyNodeOnline(nodeName: node.name)
                    }
                    // Node went offline
                    else if prevStatus == "online" && currentStatus != "online" {
                        notificationService.notifyNodeOffline(nodeName: node.name)
                    }
                }
                previousNodeStatuses[node.edgeId] = currentStatus
            }

            // Check for nodes that disappeared (went offline)
            let currentNodeIds = Set(newNodes.map { $0.edgeId })
            for (edgeId, previousStatus) in previousNodeStatuses {
                if !currentNodeIds.contains(edgeId) && previousStatus == "online" {
                    // Node was removed from the list, consider it offline
                    if let nodeName = nodes.first(where: { $0.edgeId == edgeId })?.name {
                        notificationService.notifyNodeOffline(nodeName: nodeName)
                    }
                    previousNodeStatuses.removeValue(forKey: edgeId)
                }
            }

            nodes = newNodes
            lastError = nil
        } catch {
            lastError = error.localizedDescription
        }
    }

    func refreshArtifacts() async {
        guard let api = makeAPI() else { return }
        do {
            artifacts = try await api.fetchArtifacts()
            lastError = nil
        } catch {
            lastError = error.localizedDescription
        }
    }

    func refreshSessions() async {
        guard let api = makeAPI() else { return }
        do {
            let response = try await api.fetchSessions(page: 1, size: 50)
            sessions = response.sessions
            lastError = nil
        } catch {
            lastError = error.localizedDescription
        }
    }

    func loadSessionMessages(sessionID: String, page: Int = 1, size: Int = 50) async {
        guard let api = makeAPI() else { return }
        do {
            let response = try await api.fetchSessionMessages(sessionID: sessionID, page: page, size: size)
            if page == 1 {
                sessionMessages = response.messages
            } else {
                sessionMessages.append(contentsOf: response.messages)
            }
            sessionHasMore = response.hasMore
            lastError = nil
        } catch {
            lastError = error.localizedDescription
        }
    }

    func refreshProviders() async {
        guard let api = makeAPI() else { return }
        do {
            providers = try await api.fetchProviders()
            lastError = nil
        } catch {
            lastError = error.localizedDescription
        }
    }

    func loadProviderQR(name: String) async {
        guard let api = makeAPI() else { return }
        do {
            let data = try await api.fetchProviderQR(name: name)
            if let image = NSImage(data: data) {
                providerQRImages[name] = image
            }
            lastError = nil
        } catch {
            lastError = error.localizedDescription
        }
    }

    func refreshSkills() async {
        guard let api = makeAPI() else { return }
        do {
            skills = try await api.fetchSkills()
            lastError = nil
        } catch {
            lastError = error.localizedDescription
        }
    }

    func triggerSkillsRefresh() async {
        guard let api = makeAPI() else { return }
        do {
            try await api.refreshSkills()
            skills = try await api.fetchSkills()
            lastError = nil
        } catch {
            lastError = error.localizedDescription
        }
    }

    func refreshCron() async {
        guard let api = makeAPI() else { return }
        do {
            let response = try await api.fetchCron()
            cronEnabled = response.enabled
            cronJobs = response.jobs
            lastError = nil
        } catch {
            lastError = error.localizedDescription
        }
    }

    func loadTools(for node: NodeSummary) async {
        guard let api = makeAPI() else { return }
        do {
            let tools = try await api.fetchNodeTools(edgeID: node.edgeId)
            nodeTools[node.edgeId] = tools
            lastError = nil
        } catch {
            lastError = error.localizedDescription
        }
    }

    func invokeTool(edgeID: String, toolName: String, input: String, approved: Bool) async -> ToolInvocationResult? {
        guard let api = makeAPI() else { return nil }
        do {
            let result = try await api.invokeTool(edgeID: edgeID, toolName: toolName, input: input, approved: approved)
            lastError = nil
            notificationService.notifyToolCompleted(
                toolName: toolName,
                success: !result.isError,
                durationMs: result.durationMs
            )
            return result
        } catch {
            lastError = error.localizedDescription
            notificationService.notifyError(operation: "Tool Invocation", message: error.localizedDescription)
            return nil
        }
    }

    func invokeTool(edgeID: String, toolName: String, payload: [String: Any], approved: Bool) async -> ToolInvocationResult? {
        guard let data = try? JSONSerialization.data(withJSONObject: payload, options: []),
              let input = String(data: data, encoding: .utf8) else {
            lastError = "Failed to encode tool payload"
            return nil
        }
        return await invokeTool(edgeID: edgeID, toolName: toolName, input: input, approved: approved)
    }

    func images(from result: ToolInvocationResult) -> [NSImage] {
        guard let artifacts = result.artifacts else { return [] }
        return artifacts.compactMap { imageFromArtifact($0) }
    }

    func imageFromArtifact(_ artifact: ToolInvocationArtifact) -> NSImage? {
        let isImage = artifact.mimeType.lowercased().hasPrefix("image/") || artifact.type == "screenshot"
        guard isImage else { return nil }
        if let data = artifact.data, let image = NSImage(data: data) {
            return image
        }
        if let ref = artifact.reference, let data = decodeDataURL(ref), let image = NSImage(data: data) {
            return image
        }
        return nil
    }

    func refreshEdgeServiceStatus() {
        lastError = nil
        if !FileManager.default.fileExists(atPath: plistPath) {
            edgeServiceStatus = .notInstalled
            return
        }

        let result = runCommand("/bin/launchctl", ["list"])
        if result.exitCode != 0 {
            edgeServiceStatus = .unknown
            lastError = result.output.trimmingCharacters(in: .whitespacesAndNewlines)
            return
        }
        if result.output.contains(launchdLabel) {
            edgeServiceStatus = .running
        } else {
            edgeServiceStatus = .stopped
        }
    }

    func installService() {
        lastError = nil
        let result = runCommand("/usr/bin/env", [edgeBinary, "install", "--config", configPath, "--init-config", "--start"])
        if result.exitCode != 0 {
            lastError = result.output.trimmingCharacters(in: .whitespacesAndNewlines)
            notificationService.notifyError(operation: "Install Service", message: lastError ?? "Unknown error")
        } else {
            notificationService.notifyEdgeServiceInstalled()
        }
        refreshEdgeServiceStatus()
    }

    func uninstallService() {
        lastError = nil
        let result = runCommand("/usr/bin/env", [edgeBinary, "uninstall", "--keep-config"])
        if result.exitCode != 0 {
            lastError = result.output.trimmingCharacters(in: .whitespacesAndNewlines)
            notificationService.notifyError(operation: "Uninstall Service", message: lastError ?? "Unknown error")
        } else {
            notificationService.notifyEdgeServiceUninstalled()
        }
        refreshEdgeServiceStatus()
    }

    func startService() {
        lastError = nil
        let result = runCommand("/bin/launchctl", ["load", "-w", plistPath])
        if result.exitCode != 0 {
            lastError = result.output.trimmingCharacters(in: .whitespacesAndNewlines)
            notificationService.notifyError(operation: "Start Service", message: lastError ?? "Unknown error")
        } else {
            notificationService.notifyEdgeServiceStarted()
        }
        refreshEdgeServiceStatus()
    }

    func stopService() {
        lastError = nil
        let result = runCommand("/bin/launchctl", ["unload", plistPath])
        if result.exitCode != 0 {
            lastError = result.output.trimmingCharacters(in: .whitespacesAndNewlines)
            notificationService.notifyError(operation: "Stop Service", message: lastError ?? "Unknown error")
        } else {
            notificationService.notifyEdgeServiceStopped()
        }
        refreshEdgeServiceStatus()
    }

    func openConfigFolder() {
        let url = URL(fileURLWithPath: (configPath as NSString).deletingLastPathComponent)
        NSWorkspace.shared.open(url)
    }

    func openLogs() {
        let url = URL(fileURLWithPath: logsPath)
        NSWorkspace.shared.open(url)
    }

    func openArtifact(_ artifact: ArtifactSummary) {
        Task { await downloadArtifact(artifact) }
    }

    func downloadArtifact(_ artifact: ArtifactSummary) async {
        guard let api = makeAPI() else { return }
        let base = api.baseURL.hasSuffix("/") ? String(api.baseURL.dropLast()) : api.baseURL
        guard let url = URL(string: base + "/api/artifacts/\(artifact.id)?raw=1") else { return }

        var request = URLRequest(url: url)
        request.setValue(api.apiKey, forHTTPHeaderField: "X-API-Key")

        do {
            let (data, response) = try await URLSession.shared.data(for: request)
            if let http = response as? HTTPURLResponse, !(200...299).contains(http.statusCode) {
                lastError = "Artifact download failed: HTTP \(http.statusCode)"
                return
            }
            let filename = artifact.filename.isEmpty ? "artifact-\(artifact.id)" : artifact.filename
            let tempURL = FileManager.default.temporaryDirectory.appendingPathComponent(filename)
            try data.write(to: tempURL)
            NSWorkspace.shared.open(tempURL)
        } catch {
            lastError = error.localizedDescription
        }
    }

    func loadConfig() {
        do {
            configText = try String(contentsOfFile: configPath, encoding: .utf8)
        } catch {
            configText = ""
        }
    }

    func saveConfig() {
        do {
            try configText.write(toFile: configPath, atomically: true, encoding: .utf8)
        } catch {
            lastError = error.localizedDescription
        }
    }

    func loadLogs() {
        guard FileManager.default.fileExists(atPath: logsPath) else {
            logText = ""
            return
        }
        do {
            let data = try String(contentsOfFile: logsPath, encoding: .utf8)
            logText = tail(data, lines: 400)
        } catch {
            logText = ""
        }
    }

    private func makeAPI() -> NexusAPI? {
        let trimmedURL = Self.normalizeBaseURL(baseURL)
        if trimmedURL.isEmpty || apiKey.isEmpty {
            lastError = "Set base URL and API key in Settings"
            return nil
        }
        return NexusAPI(baseURL: trimmedURL, apiKey: apiKey)
    }

    private func decodeDataURL(_ raw: String) -> Data? {
        guard raw.hasPrefix("data:") else { return nil }
        let parts = raw.split(separator: ",", maxSplits: 1).map(String.init)
        guard parts.count == 2 else { return nil }
        let meta = parts[0]
        guard meta.contains(";base64") else { return nil }
        return Data(base64Encoded: parts[1])
    }

    private static func normalizeBaseURL(_ raw: String) -> String {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty {
            return trimmed
        }
        if trimmed.contains("://") {
        return trimmed
        }
        return "http://\(trimmed)"
    }

    private func tail(_ text: String, lines: Int) -> String {
        let split = text.split(separator: "\n", omittingEmptySubsequences: false)
        if split.count <= lines {
            return text
        }
        return split.suffix(lines).joined(separator: "\n")
    }

    private func runCommand(_ launchPath: String, _ arguments: [String]) -> (exitCode: Int32, output: String) {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: launchPath)
        process.arguments = arguments

        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = pipe

        do {
            try process.run()
        } catch {
            return (exitCode: 1, output: "Failed to run \(launchPath): \(error)")
        }

        process.waitUntilExit()
        let data = pipe.fileHandleForReading.readDataToEndOfFile()
        let output = String(data: data, encoding: .utf8) ?? ""
        return (exitCode: process.terminationStatus, output: output)
    }

    private static func defaultConfigPath() -> String {
        let home = FileManager.default.homeDirectoryForCurrentUser
        return home.appendingPathComponent(".nexus-edge/config.yaml").path
    }

    private var launchdLabel: String { "com.haasonsaas.nexus-edge" }

    private var plistPath: String {
        let home = FileManager.default.homeDirectoryForCurrentUser
        return home.appendingPathComponent("Library/LaunchAgents/\(launchdLabel).plist").path
    }

    private var logsPath: String {
        let home = FileManager.default.homeDirectoryForCurrentUser
        return home.appendingPathComponent("Library/Logs/nexus-edge.log").path
    }
}
