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

    private let keychain = KeychainStore()
    private let edgeBinary: String

    init() {
        baseURL = UserDefaults.standard.string(forKey: "NexusBaseURL") ?? "http://localhost:8080"
        baseURL = normalizeBaseURL(baseURL)
        apiKey = keychain.read() ?? ""
        configPath = AppModel.defaultConfigPath()
        edgeBinary = ProcessInfo.processInfo.environment["NEXUS_EDGE_BIN"] ?? "nexus-edge"

        refreshEdgeServiceStatus()
        loadConfig()
        loadLogs()
        Task {
            await refreshAll()
        }
    }

    func saveSettings() {
        baseURL = normalizeBaseURL(baseURL)
        UserDefaults.standard.set(baseURL, forKey: "NexusBaseURL")
        _ = keychain.write(apiKey)
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
            lastError = nil
        } catch {
            lastError = error.localizedDescription
        }
    }

    func refreshNodes() async {
        guard let api = makeAPI() else { return }
        do {
            nodes = try await api.fetchNodes()
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
            return result
        } catch {
            lastError = error.localizedDescription
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
        }
        refreshEdgeServiceStatus()
    }

    func uninstallService() {
        lastError = nil
        let result = runCommand("/usr/bin/env", [edgeBinary, "uninstall", "--keep-config"])
        if result.exitCode != 0 {
            lastError = result.output.trimmingCharacters(in: .whitespacesAndNewlines)
        }
        refreshEdgeServiceStatus()
    }

    func startService() {
        lastError = nil
        let result = runCommand("/bin/launchctl", ["load", "-w", plistPath])
        if result.exitCode != 0 {
            lastError = result.output.trimmingCharacters(in: .whitespacesAndNewlines)
        }
        refreshEdgeServiceStatus()
    }

    func stopService() {
        lastError = nil
        let result = runCommand("/bin/launchctl", ["unload", plistPath])
        if result.exitCode != 0 {
            lastError = result.output.trimmingCharacters(in: .whitespacesAndNewlines)
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
        let trimmedURL = normalizeBaseURL(baseURL)
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

    private func normalizeBaseURL(_ raw: String) -> String {
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
