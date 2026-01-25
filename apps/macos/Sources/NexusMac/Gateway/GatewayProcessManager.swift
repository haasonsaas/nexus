import Foundation
import Observation

@MainActor
@Observable
final class GatewayProcessManager {
    static let shared = GatewayProcessManager()

    enum Status: Equatable {
        case stopped
        case starting
        case running(details: String?)
        case attachedExisting(details: String?)
        case failed(String)

        var label: String {
            switch self {
            case .stopped: return "Stopped"
            case .starting: return "Starting..."
            case let .running(details):
                if let details, !details.isEmpty { return "Running (\(details))" }
                return "Running"
            case let .attachedExisting(details):
                if let details, !details.isEmpty {
                    return "Using existing gateway (\(details))"
                }
                return "Using existing gateway"
            case let .failed(reason): return "Failed: \(reason)"
            }
        }
    }

    private(set) var status: Status = .stopped
    private(set) var log: String = ""
    private(set) var environmentStatus: GatewayEnvironmentStatus = .checking
    private(set) var existingGatewayDetails: String?
    private(set) var lastFailureReason: String?

    private var desiredActive = false
    private var environmentRefreshTask: Task<Void, Never>?
    private var logRefreshTask: Task<Void, Never>?
    private let logLimit = 20000

    // MARK: - Public API

    func setActive(_ active: Bool) {
        desiredActive = active
        refreshEnvironmentStatus()
        if active {
            startIfNeeded()
        } else {
            stop()
        }
    }

    func startIfNeeded() {
        guard desiredActive else { return }

        switch status {
        case .starting, .running, .attachedExisting:
            return
        case .stopped, .failed:
            break
        }

        status = .starting

        Task { [weak self] in
            guard let self else { return }
            if await self.attachExistingGatewayIfAvailable() {
                return
            }
            await self.enableLaunchdGateway()
        }
    }

    func stop() {
        desiredActive = false
        existingGatewayDetails = nil
        lastFailureReason = nil
        status = .stopped

        let bundlePath = Bundle.main.bundleURL.path
        Task {
            _ = await LaunchAgentManager.set(
                enabled: false,
                bundlePath: bundlePath,
                port: GatewayEnvironment.gatewayPort())
        }
    }

    func refreshEnvironmentStatus(force: Bool = false) {
        if !force && environmentRefreshTask != nil { return }

        environmentRefreshTask = Task { [weak self] in
            let status = await Task.detached(priority: .utility) {
                GatewayEnvironment.check()
            }.value
            await MainActor.run {
                guard let self else { return }
                self.environmentStatus = status
                self.environmentRefreshTask = nil
            }
        }
    }

    func refreshLog() {
        guard logRefreshTask == nil else { return }
        let path = LaunchAgentManager.gatewayLogPath()
        let limit = logLimit

        logRefreshTask = Task { [weak self] in
            let log = await Task.detached(priority: .utility) {
                Self.readGatewayLog(path: path, limit: limit)
            }.value
            await MainActor.run {
                guard let self else { return }
                if !log.isEmpty {
                    self.log = log
                }
                self.logRefreshTask = nil
            }
        }
    }

    func clearLog() {
        log = ""
        try? FileManager.default.removeItem(atPath: LaunchAgentManager.gatewayLogPath())
    }

    func waitForGatewayReady(timeout: TimeInterval = 6) async -> Bool {
        let deadline = Date().addingTimeInterval(timeout)
        let port = GatewayEnvironment.gatewayPort()

        while Date() < deadline {
            if !desiredActive { return false }
            if await probeHealth(port: port) {
                return true
            }
            try? await Task.sleep(nanoseconds: 300_000_000)
        }
        appendLog("[gateway] readiness wait timed out\n")
        return false
    }

    // MARK: - Internals

    private func attachExistingGatewayIfAvailable() async -> Bool {
        let port = GatewayEnvironment.gatewayPort()
        let instance = await PortGuardian.shared.describe(port: port)
        let instanceText = instance.map { describe(instance: $0) }
        let hasListener = instance != nil

        for attempt in 0..<(hasListener ? 3 : 1) {
            if await probeHealth(port: port) {
                let details = instanceText ?? "port \(port)"
                existingGatewayDetails = details
                status = .attachedExisting(details: details)
                appendLog("[gateway] using existing instance: \(details)\n")
                refreshLog()
                return true
            }

            if attempt < 2, hasListener {
                try? await Task.sleep(nanoseconds: 250_000_000)
                continue
            }

            if hasListener {
                let reason = "Gateway on port \(port) not responding to health check"
                existingGatewayDetails = instanceText
                status = .failed(reason)
                lastFailureReason = reason
                appendLog("[gateway] existing listener on port \(port) but attach failed\n")
                return true
            }

            existingGatewayDetails = nil
            return false
        }

        existingGatewayDetails = nil
        return false
    }

    private func enableLaunchdGateway() async {
        existingGatewayDetails = nil

        let resolution = await Task.detached(priority: .utility) {
            GatewayEnvironment.resolveGatewayCommand()
        }.value

        await MainActor.run { environmentStatus = resolution.status }

        guard resolution.command != nil else {
            await MainActor.run {
                status = .failed(resolution.status.message)
            }
            return
        }

        let bundlePath = Bundle.main.bundleURL.path
        let port = GatewayEnvironment.gatewayPort()
        appendLog("[gateway] enabling launchd job (\(gatewayLaunchdLabel)) on port \(port)\n")

        let err = await LaunchAgentManager.set(enabled: true, bundlePath: bundlePath, port: port)
        if let err {
            status = .failed(err)
            lastFailureReason = err
            return
        }

        // Wait for gateway to accept connections
        let deadline = Date().addingTimeInterval(6)
        while Date() < deadline {
            if !desiredActive { return }
            if await probeHealth(port: port) {
                let instance = await PortGuardian.shared.describe(port: port)
                let details = instance.map { "pid \($0.pid)" }
                status = .running(details: details)
                refreshLog()
                return
            }
            try? await Task.sleep(nanoseconds: 400_000_000)
        }

        status = .failed("Gateway did not start in time")
        lastFailureReason = "launchd start timeout"
    }

    private func probeHealth(port: Int) async -> Bool {
        let url = URL(string: "http://127.0.0.1:\(port)/health")!
        let config = URLSessionConfiguration.ephemeral
        config.timeoutIntervalForRequest = 2
        config.timeoutIntervalForResource = 2
        let session = URLSession(configuration: config)

        var request = URLRequest(url: url)
        request.timeoutInterval = 2

        do {
            let (_, response) = try await session.data(for: request)
            if let http = response as? HTTPURLResponse {
                return (200...299).contains(http.statusCode)
            }
            return false
        } catch {
            return false
        }
    }

    private func describe(instance: PortGuardian.Descriptor) -> String {
        let path = instance.executablePath ?? "path unknown"
        return "pid \(instance.pid) \(instance.command) @ \(path)"
    }

    private func appendLog(_ chunk: String) {
        log.append(chunk)
        if log.count > logLimit {
            log = String(log.suffix(logLimit))
        }
    }

    private nonisolated static func readGatewayLog(path: String, limit: Int) -> String {
        guard FileManager.default.fileExists(atPath: path) else { return "" }
        guard let data = try? Data(contentsOf: URL(fileURLWithPath: path)) else { return "" }
        let text = String(data: data, encoding: .utf8) ?? ""
        if text.count <= limit { return text }
        return String(text.suffix(limit))
    }
}
