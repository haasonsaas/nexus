import Cocoa
import Darwin
import Foundation
import Observation
import OSLog

/// Reports periodic presence beacons to the gateway.
/// Each beacon contains machine identity, network info, and activity state.
@MainActor
@Observable
final class PresenceReporter {
    static let shared = PresenceReporter()

    // MARK: - Configuration

    /// Beacon interval in seconds (default: 180 seconds / 3 minutes)
    private let interval: TimeInterval = 180

    // MARK: - Private State

    private let logger = Logger(subsystem: "com.haasonsaas.nexus", category: "presence")
    private var beaconTask: Task<Void, Never>?
    private let instanceId: String

    // MARK: - Initialization

    private init() {
        self.instanceId = Self.loadOrCreateInstanceId()
    }

    // MARK: - Public Methods

    /// Start periodic presence beacons.
    /// Sends an initial beacon with reason "launch", then periodic beacons every 180 seconds.
    func start() {
        guard beaconTask == nil else {
            logger.debug("Presence reporter already running")
            return
        }
        logger.info("Starting presence reporter with interval \(self.interval)s")
        beaconTask = Task.detached { [weak self] in
            guard let self else { return }
            await self.sendBeacon(reason: "launch")
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: UInt64(self.interval * 1_000_000_000))
                if Task.isCancelled { break }
                await self.sendBeacon(reason: "periodic")
            }
        }
    }

    /// Stop the periodic beacon loop.
    func stop() {
        logger.info("Stopping presence reporter")
        beaconTask?.cancel()
        beaconTask = nil
    }

    /// Send an immediate presence beacon with the given reason.
    /// - Parameter reason: The reason for the beacon (e.g., "connect", "wake", "manual")
    func sendImmediate(reason: String = "connect") {
        Task {
            await sendBeacon(reason: reason)
        }
    }

    // MARK: - Beacon Implementation

    @Sendable
    private func sendBeacon(reason: String) async {
        let mode = await MainActor.run { Self.connectionMode() }
        let host = Self.displayName()
        let ip = Self.primaryIPv4Address() ?? "ip-unknown"
        let version = Self.appVersionString()
        let platform = Self.platformString()
        let lastInput = Self.lastInputSeconds()

        let summary = Self.composePresenceSummary(
            host: host,
            ip: ip,
            version: version,
            lastInput: lastInput,
            mode: mode,
            reason: reason
        )

        var params: [String: AnyHashable] = [
            "instanceId": AnyHashable(instanceId),
            "host": AnyHashable(host),
            "ip": AnyHashable(ip),
            "mode": AnyHashable(mode),
            "version": AnyHashable(version),
            "platform": AnyHashable(platform),
            "deviceFamily": AnyHashable("Mac"),
            "reason": AnyHashable(reason),
        ]

        if let model = Self.modelIdentifier() {
            params["modelIdentifier"] = AnyHashable(model)
        }
        if let lastInput {
            params["lastInputSeconds"] = AnyHashable(lastInput)
        }

        logger.debug("Sending presence beacon: \(summary, privacy: .public)")

        do {
            try await ControlChannel.shared.sendSystemEvent(summary, params: params)
            logger.info("Presence beacon sent: reason=\(reason, privacy: .public)")
        } catch {
            logger.error("Presence beacon failed: \(error.localizedDescription, privacy: .public)")
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

    // MARK: - Host Information

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

    private static func composePresenceSummary(
        host: String,
        ip: String,
        version: String,
        lastInput: Int?,
        mode: String,
        reason: String
    ) -> String {
        let lastLabel = lastInput.map { "last input \($0)s ago" } ?? "last input unknown"
        return "Node: \(host) (\(ip)) \u{00B7} app \(version) \u{00B7} \(lastLabel) \u{00B7} mode \(mode) \u{00B7} reason \(reason)"
    }

    // MARK: - App Information

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
                NI_NUMERICHOST
            )
            guard result == 0 else { continue }
            let len = buffer.prefix { $0 != 0 }
            let bytes = len.map { UInt8(bitPattern: $0) }
            guard let ip = String(bytes: bytes, encoding: .utf8) else { continue }

            if name == "en0" {
                en0 = ip
                break
            }
            if fallback == nil {
                fallback = ip
            }
        }

        return en0 ?? fallback
    }
}

// MARK: - Testing Support

#if DEBUG
extension PresenceReporter {
    static func _testComposePresenceSummary(
        host: String,
        ip: String,
        version: String,
        lastInput: Int?,
        mode: String,
        reason: String
    ) -> String {
        composePresenceSummary(
            host: host,
            ip: ip,
            version: version,
            lastInput: lastInput,
            mode: mode,
            reason: reason
        )
    }

    static func _testAppVersionString() -> String {
        appVersionString()
    }

    static func _testPlatformString() -> String {
        platformString()
    }

    static func _testLastInputSeconds() -> Int? {
        lastInputSeconds()
    }

    static func _testPrimaryIPv4Address() -> String? {
        primaryIPv4Address()
    }

    static func _testDisplayName() -> String {
        displayName()
    }

    static func _testModelIdentifier() -> String? {
        modelIdentifier()
    }

    static func _testConnectionMode() -> String {
        connectionMode()
    }
}
#endif
