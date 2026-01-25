import Cocoa
import Darwin
import Foundation
import OSLog

@MainActor
final class PresenceReporter {
    static let shared = PresenceReporter()

    private let logger = Logger(subsystem: "com.haasonsaas.nexus", category: "presence")
    private var task: Task<Void, Never>?
    private let interval: TimeInterval = 180 // 3 minutes
    private let instanceId: String

    private init() {
        self.instanceId = Self.loadOrCreateInstanceId()
    }

    // MARK: - Public Methods

    func start() {
        guard self.task == nil else { return }
        self.task = Task.detached { [weak self] in
            guard let self else { return }
            await self.push(reason: "launch")
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: UInt64(self.interval * 1_000_000_000))
                await self.push(reason: "periodic")
            }
        }
    }

    func stop() {
        self.task?.cancel()
        self.task = nil
    }

    /// Fire an immediate presence beacon (e.g., right after connecting).
    func sendImmediate(reason: String = "connect") {
        Task { await self.push(reason: reason) }
    }

    // MARK: - Push Implementation

    @Sendable
    private func push(reason: String) async {
        let mode = await MainActor.run { Self.connectionMode() }
        let host = Self.displayName()
        let ip = Self.primaryIPv4Address() ?? "ip-unknown"
        let version = Self.appVersionString()
        let platform = Self.platformString()
        let lastInput = Self.lastInputSeconds()
        let text = Self.composePresenceSummary(mode: mode, reason: reason)
        var params: [String: AnyHashable] = [
            "instanceId": AnyHashable(self.instanceId),
            "host": AnyHashable(host),
            "ip": AnyHashable(ip),
            "mode": AnyHashable(mode),
            "version": AnyHashable(version),
            "platform": AnyHashable(platform),
            "deviceFamily": AnyHashable("Mac"),
            "reason": AnyHashable(reason),
        ]
        if let model = Self.modelIdentifier() { params["modelIdentifier"] = AnyHashable(model) }
        if let lastInput { params["lastInputSeconds"] = AnyHashable(lastInput) }
        do {
            try await ControlChannel.shared.sendSystemEvent(text, params: params)
        } catch {
            self.logger.error("presence send failed: \(error.localizedDescription, privacy: .public)")
        }
    }

    // MARK: - Instance Identity

    private static func loadOrCreateInstanceId() -> String {
        let defaults = UserDefaults.standard
        let key = "NexusInstanceId"
        if let existing = defaults.string(forKey: key)?
            .trimmingCharacters(in: .whitespacesAndNewlines),
            !existing.isEmpty
        {
            return existing
        }
        let id = UUID().uuidString.lowercased()
        defaults.set(id, forKey: key)
        return id
    }

    private static func displayName() -> String {
        if let name = Host.current().localizedName?.trimmingCharacters(in: .whitespacesAndNewlines),
           !name.isEmpty
        {
            return name
        }
        return "nexus"
    }

    private static func modelIdentifier() -> String? {
        var size = 0
        guard sysctlbyname("hw.model", nil, &size, nil, 0) == 0, size > 1 else { return nil }

        var buffer = [CChar](repeating: 0, count: size)
        guard sysctlbyname("hw.model", &buffer, &size, nil, 0) == 0 else { return nil }

        let bytes = buffer.prefix { $0 != 0 }.map { UInt8(bitPattern: $0) }
        guard let raw = String(bytes: bytes, encoding: .utf8) else { return nil }
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }

    // MARK: - Connection Mode

    private static func connectionMode() -> String {
        switch ControlChannel.shared.state {
        case .connected: return "connected"
        case .connecting: return "connecting"
        case .disconnected: return "disconnected"
        case .degraded: return "degraded"
        }
    }

    // MARK: - Presence Summary

    private static func composePresenceSummary(mode: String, reason: String) -> String {
        let host = displayName()
        let ip = primaryIPv4Address() ?? "ip-unknown"
        let version = appVersionString()
        let lastInput = lastInputSeconds()
        let lastLabel = lastInput.map { "last input \($0)s ago" } ?? "last input unknown"
        return "Node: \(host) (\(ip)) - app \(version) - \(lastLabel) - mode \(mode) - reason \(reason)"
    }

    // MARK: - App Info

    private static func appVersionString() -> String {
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "dev"
        if let build = Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String {
            let trimmed = build.trimmingCharacters(in: .whitespacesAndNewlines)
            if !trimmed.isEmpty, trimmed != version {
                return "\(version) (\(trimmed))"
            }
        }
        return version
    }

    private static func platformString() -> String {
        let v = ProcessInfo.processInfo.operatingSystemVersion
        return "macos \(v.majorVersion).\(v.minorVersion).\(v.patchVersion)"
    }

    // MARK: - Last Input Detection

    private static func lastInputSeconds() -> Int? {
        let anyEvent = CGEventType(rawValue: UInt32.max) ?? .null
        let seconds = CGEventSource.secondsSinceLastEventType(.combinedSessionState, eventType: anyEvent)
        if seconds.isNaN || seconds.isInfinite || seconds < 0 { return nil }
        return Int(seconds.rounded())
    }

    // MARK: - IP Address Detection

    private static func primaryIPv4Address() -> String? {
        var addrList: UnsafeMutablePointer<ifaddrs>?
        guard getifaddrs(&addrList) == 0, let first = addrList else { return nil }
        defer { freeifaddrs(addrList) }

        var fallback: String?
        var en0: String?

        for ptr in sequence(first: first, next: { $0.pointee.ifa_next }) {
            let flags = Int32(ptr.pointee.ifa_flags)
            let isUp = (flags & IFF_UP) != 0
            let isLoopback = (flags & IFF_LOOPBACK) != 0
            let name = String(cString: ptr.pointee.ifa_name)
            let family = ptr.pointee.ifa_addr.pointee.sa_family
            if !isUp || isLoopback || family != UInt8(AF_INET) { continue }

            var addr = ptr.pointee.ifa_addr.pointee
            var buffer = [CChar](repeating: 0, count: Int(NI_MAXHOST))
            let result = getnameinfo(
                &addr,
                socklen_t(ptr.pointee.ifa_addr.pointee.sa_len),
                &buffer,
                socklen_t(buffer.count),
                nil,
                0,
                NI_NUMERICHOST)
            guard result == 0 else { continue }
            let len = buffer.prefix { $0 != 0 }
            let bytes = len.map { UInt8(bitPattern: $0) }
            guard let ip = String(bytes: bytes, encoding: .utf8) else { continue }

            if name == "en0" { en0 = ip; break }
            if fallback == nil { fallback = ip }
        }

        return en0 ?? fallback
    }
}

#if DEBUG
extension PresenceReporter {
    static func _testComposePresenceSummary(mode: String, reason: String) -> String {
        self.composePresenceSummary(mode: mode, reason: reason)
    }

    static func _testAppVersionString() -> String {
        self.appVersionString()
    }

    static func _testPlatformString() -> String {
        self.platformString()
    }

    static func _testLastInputSeconds() -> Int? {
        self.lastInputSeconds()
    }

    static func _testPrimaryIPv4Address() -> String? {
        self.primaryIPv4Address()
    }
}
#endif
