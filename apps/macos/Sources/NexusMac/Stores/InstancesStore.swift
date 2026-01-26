import Darwin
import Foundation
import Observation
import OSLog

// MARK: - NexusInstance Model

/// Represents a Nexus instance running on a device.
struct NexusInstance: Identifiable, Codable, Equatable, Hashable {
    let id: String
    var displayName: String
    var platform: String
    var ipAddress: String
    var lastSeen: Date
    var connectionStatus: ConnectionStatus
    var appVersion: String
    var modelIdentifier: String?

    enum ConnectionStatus: String, Codable {
        case online
        case offline
        case connecting
        case unknown
    }

    /// Whether this instance was seen within the last 5 minutes
    var isActive: Bool {
        Date().timeIntervalSince(lastSeen) < 300
    }

    /// Whether this instance should be pruned (not seen for 24 hours)
    var shouldPrune: Bool {
        Date().timeIntervalSince(lastSeen) > 86400
    }
}

// MARK: - Notification Names

extension Notification.Name {
    static let instancesChanged = Notification.Name("nexus.instances.changed")
    static let instanceOnline = Notification.Name("nexus.instance.online")
    static let instanceOffline = Notification.Name("nexus.instance.offline")
}

// MARK: - InstancesStore

/// Manages multi-instance tracking across devices/machines.
/// Receives updates via gateway events and persists known instances to disk.
@MainActor
@Observable
final class InstancesStore {
    static let shared = InstancesStore()

    private let logger = Logger(subsystem: "com.haasonsaas.nexus", category: "instances")
    private let fileManager = FileManager.default
    private var eventObserver: NSObjectProtocol?
    private var pruneTask: Task<Void, Never>?

    // MARK: - Published State

    private(set) var instances: [NexusInstance] = []

    var currentInstance: NexusInstance {
        let instanceId = InstanceIdentity.instanceId
        if let existing = instances.first(where: { $0.id == instanceId }) {
            return existing
        }
        return createCurrentInstance()
    }

    var activeInstances: [NexusInstance] {
        instances.filter { $0.isActive }
    }

    // MARK: - Initialization

    private init() {
        loadFromDisk()
        ensureCurrentInstance()
        subscribeToGatewayEvents()
        startPruneTimer()
        logger.info("InstancesStore initialized with \(self.instances.count) instances")
    }

    deinit {
        Task { @MainActor [weak self] in
            guard let self else { return }
            if let observer = self.eventObserver {
                NotificationCenter.default.removeObserver(observer)
            }
            self.pruneTask?.cancel()
        }
    }

    // MARK: - Public Methods

    /// Update or add an instance
    func update(instance: NexusInstance) {
        let wasOnline = instances.first(where: { $0.id == instance.id })?.connectionStatus == .online
        let isNowOnline = instance.connectionStatus == .online

        if let index = instances.firstIndex(where: { $0.id == instance.id }) {
            instances[index] = instance
            logger.debug("Updated instance: \(instance.displayName)")
        } else {
            instances.append(instance)
            logger.info("Added new instance: \(instance.displayName)")
        }

        saveToDisk()
        NotificationCenter.default.post(name: .instancesChanged, object: nil)

        // Post status change notifications
        if !wasOnline && isNowOnline {
            NotificationCenter.default.post(
                name: .instanceOnline,
                object: instance,
                userInfo: ["instanceId": instance.id]
            )
            logger.info("Instance came online: \(instance.displayName)")
        } else if wasOnline && !isNowOnline {
            NotificationCenter.default.post(
                name: .instanceOffline,
                object: instance,
                userInfo: ["instanceId": instance.id]
            )
            logger.info("Instance went offline: \(instance.displayName)")
        }
    }

    /// Mark an instance as offline
    func markOffline(instanceId: String) {
        guard let index = instances.firstIndex(where: { $0.id == instanceId }) else {
            logger.warning("Cannot mark offline: instance not found \(instanceId)")
            return
        }

        let wasOnline = instances[index].connectionStatus == .online
        instances[index].connectionStatus = .offline
        instances[index].lastSeen = Date()

        saveToDisk()
        NotificationCenter.default.post(name: .instancesChanged, object: nil)

        if wasOnline {
            NotificationCenter.default.post(
                name: .instanceOffline,
                object: instances[index],
                userInfo: ["instanceId": instanceId]
            )
            logger.info("Instance marked offline: \(self.instances[index].displayName)")
        }
    }

    /// Remove an instance from the store
    func remove(instanceId: String) {
        guard let index = instances.firstIndex(where: { $0.id == instanceId }) else {
            logger.warning("Cannot remove: instance not found \(instanceId)")
            return
        }

        let removed = instances.remove(at: index)
        saveToDisk()
        NotificationCenter.default.post(name: .instancesChanged, object: nil)
        logger.info("Removed instance: \(removed.displayName)")
    }

    /// Request instances from gateway
    func refresh() async {
        logger.debug("Refreshing instances from gateway")

        do {
            let data = try await ControlChannel.shared.request(method: "instances.list")
            let response = try JSONDecoder().decode(InstancesResponse.self, from: data)

            for instanceData in response.instances {
                let instance = NexusInstance(
                    id: instanceData.instanceId,
                    displayName: instanceData.host ?? "Unknown",
                    platform: instanceData.platform ?? "unknown",
                    ipAddress: instanceData.ip ?? "unknown",
                    lastSeen: Date(timeIntervalSince1970: instanceData.lastSeen ?? Date().timeIntervalSince1970),
                    connectionStatus: parseStatus(instanceData.status),
                    appVersion: instanceData.version ?? "unknown",
                    modelIdentifier: instanceData.modelIdentifier
                )
                update(instance: instance)
            }

            logger.info("Refreshed \(response.instances.count) instances from gateway")
        } catch {
            logger.error("Failed to refresh instances: \(error.localizedDescription)")
        }
    }

    /// Send a message to a specific instance
    func sendMessageTo(instanceId: String, message: String) async throws {
        guard instances.contains(where: { $0.id == instanceId }) else {
            throw InstancesError.instanceNotFound(instanceId)
        }

        logger.debug("Sending message to instance \(instanceId): \(message)")

        let params: [String: AnyHashable] = [
            "targetInstanceId": instanceId,
            "message": message,
            "fromInstanceId": InstanceIdentity.instanceId
        ]

        _ = try await ControlChannel.shared.request(
            method: "instances.message",
            params: params
        )

        logger.info("Message sent to instance \(instanceId)")
    }

    // MARK: - Private Methods

    private func createCurrentInstance() -> NexusInstance {
        NexusInstance(
            id: InstanceIdentity.instanceId,
            displayName: InstanceIdentity.displayName,
            platform: platformString(),
            ipAddress: primaryIPv4Address() ?? "unknown",
            lastSeen: Date(),
            connectionStatus: .online,
            appVersion: appVersionString(),
            modelIdentifier: InstanceIdentity.modelIdentifier
        )
    }

    private func ensureCurrentInstance() {
        let current = createCurrentInstance()
        if !instances.contains(where: { $0.id == current.id }) {
            instances.append(current)
            saveToDisk()
        } else {
            // Update current instance info
            update(instance: current)
        }
    }

    private func subscribeToGatewayEvents() {
        eventObserver = NotificationCenter.default.addObserver(
            forName: .controlHeartbeat,
            object: nil,
            queue: .main
        ) { [weak self] notification in
            Task { @MainActor in
                self?.handleHeartbeat(notification)
            }
        }
    }

    private func handleHeartbeat(_ notification: Notification) {
        // Parse presence beacons from other instances
        guard let userInfo = notification.userInfo,
              let instanceId = userInfo["instanceId"] as? String,
              instanceId != InstanceIdentity.instanceId else {
            return
        }

        let instance = NexusInstance(
            id: instanceId,
            displayName: userInfo["host"] as? String ?? "Unknown",
            platform: userInfo["platform"] as? String ?? "unknown",
            ipAddress: userInfo["ip"] as? String ?? "unknown",
            lastSeen: Date(),
            connectionStatus: .online,
            appVersion: userInfo["version"] as? String ?? "unknown",
            modelIdentifier: userInfo["modelIdentifier"] as? String
        )

        update(instance: instance)
    }

    private func startPruneTimer() {
        pruneTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 3600 * 1_000_000_000) // 1 hour
                await self?.pruneStaleInstances()
            }
        }
    }

    private func pruneStaleInstances() {
        let staleIds = instances.filter { $0.shouldPrune && $0.id != InstanceIdentity.instanceId }.map { $0.id }
        guard !staleIds.isEmpty else { return }

        for id in staleIds {
            remove(instanceId: id)
        }
        logger.info("Pruned \(staleIds.count) stale instances")
    }

    private func parseStatus(_ status: String?) -> NexusInstance.ConnectionStatus {
        switch status?.lowercased() {
        case "online", "connected": return .online
        case "offline", "disconnected": return .offline
        case "connecting": return .connecting
        default: return .unknown
        }
    }

    // MARK: - Persistence

    private var persistenceURL: URL {
        let appSupport = fileManager.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        let nexusDir = appSupport.appendingPathComponent("Nexus", isDirectory: true)

        if !fileManager.fileExists(atPath: nexusDir.path) {
            try? fileManager.createDirectory(at: nexusDir, withIntermediateDirectories: true)
        }

        return nexusDir.appendingPathComponent("instances.json")
    }

    private func loadFromDisk() {
        guard fileManager.fileExists(atPath: persistenceURL.path) else {
            logger.debug("No persisted instances file found")
            return
        }

        do {
            let data = try Data(contentsOf: persistenceURL)
            let decoder = JSONDecoder()
            decoder.dateDecodingStrategy = .iso8601
            instances = try decoder.decode([NexusInstance].self, from: data)
            logger.debug("Loaded \(self.instances.count) instances from disk")
        } catch {
            logger.error("Failed to load instances from disk: \(error.localizedDescription)")
        }
    }

    private func saveToDisk() {
        do {
            let encoder = JSONEncoder()
            encoder.dateEncodingStrategy = .iso8601
            encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
            let data = try encoder.encode(instances)
            try data.write(to: persistenceURL, options: .atomic)
            logger.debug("Saved \(self.instances.count) instances to disk")
        } catch {
            logger.error("Failed to save instances to disk: \(error.localizedDescription)")
        }
    }

    // MARK: - Utility Methods

    private func platformString() -> String {
        let v = ProcessInfo.processInfo.operatingSystemVersion
        return "macos \(v.majorVersion).\(v.minorVersion).\(v.patchVersion)"
    }

    private func appVersionString() -> String {
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "dev"
        if let build = Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String {
            let trimmed = build.trimmingCharacters(in: .whitespacesAndNewlines)
            if !trimmed.isEmpty, trimmed != version {
                return "\(version) (\(trimmed))"
            }
        }
        return version
    }

    private func primaryIPv4Address() -> String? {
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

// MARK: - Response Models

private struct InstancesResponse: Codable {
    let instances: [InstanceData]

    struct InstanceData: Codable {
        let instanceId: String
        let host: String?
        let ip: String?
        let platform: String?
        let version: String?
        let status: String?
        let lastSeen: TimeInterval?
        let modelIdentifier: String?
    }
}

// MARK: - Errors

enum InstancesError: Error, LocalizedError {
    case instanceNotFound(String)
    case messageFailed(String)

    var errorDescription: String? {
        switch self {
        case .instanceNotFound(let id):
            return "Instance not found: \(id)"
        case .messageFailed(let reason):
            return "Failed to send message: \(reason)"
        }
    }
}

// MARK: - Testing Support

#if DEBUG
extension InstancesStore {
    static func _testInstance(
        id: String = UUID().uuidString,
        displayName: String = "Test Device",
        platform: String = "macos 14.0.0",
        ipAddress: String = "192.168.1.100",
        lastSeen: Date = Date(),
        connectionStatus: NexusInstance.ConnectionStatus = .online,
        appVersion: String = "1.0.0"
    ) -> NexusInstance {
        NexusInstance(
            id: id,
            displayName: displayName,
            platform: platform,
            ipAddress: ipAddress,
            lastSeen: lastSeen,
            connectionStatus: connectionStatus,
            appVersion: appVersion,
            modelIdentifier: nil
        )
    }

    func _testClearInstances() {
        instances.removeAll()
        saveToDisk()
    }

    func _testAddInstance(_ instance: NexusInstance) {
        update(instance: instance)
    }
}
#endif
