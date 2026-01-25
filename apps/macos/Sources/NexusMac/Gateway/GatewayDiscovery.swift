import Foundation
import Network
import OSLog

/// Discovers available gateways on the local network and Tailscale.
@MainActor
@Observable
final class GatewayDiscovery {
    static let shared = GatewayDiscovery()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "discovery")

    // MARK: - State

    private(set) var discoveredGateways: [DiscoveredGateway] = []
    private(set) var isScanning = false
    private(set) var lastScanAt: Date?

    private var browser: NWBrowser?
    private var scanTask: Task<Void, Never>?

    // MARK: - Models

    struct DiscoveredGateway: Identifiable, Equatable {
        let id: String
        let name: String
        let host: String
        let port: Int
        let source: DiscoverySource
        var isOnline: Bool = false
        var version: String?
        var lastSeen: Date = Date()

        var displayName: String {
            if let version {
                return "\(name) (v\(version))"
            }
            return name
        }

        var connectionString: String {
            "\(host):\(port)"
        }
    }

    enum DiscoverySource: String {
        case bonjour = "Local Network"
        case tailscale = "Tailscale"
        case manual = "Manual"
        case recent = "Recent"
    }

    private init() {
        loadRecentGateways()
    }

    // MARK: - Public API

    /// Start scanning for gateways
    func startScan() {
        guard !isScanning else { return }
        isScanning = true

        logger.info("starting gateway discovery scan")

        scanTask = Task {
            // Clear stale discoveries
            discoveredGateways.removeAll { $0.source != .recent && $0.source != .manual }

            // Scan in parallel
            await withTaskGroup(of: Void.self) { group in
                group.addTask { await self.scanBonjour() }
                group.addTask { await self.scanTailscale() }
                group.addTask { await self.checkRecentGateways() }
            }

            isScanning = false
            lastScanAt = Date()

            logger.info("gateway scan complete, found \(self.discoveredGateways.count) gateways")
        }
    }

    /// Stop scanning
    func stopScan() {
        scanTask?.cancel()
        browser?.cancel()
        browser = nil
        isScanning = false
    }

    /// Add a manual gateway entry
    func addManualGateway(host: String, port: Int = 3000, name: String? = nil) {
        let gateway = DiscoveredGateway(
            id: "manual-\(host):\(port)",
            name: name ?? host,
            host: host,
            port: port,
            source: .manual
        )

        if !discoveredGateways.contains(where: { $0.id == gateway.id }) {
            discoveredGateways.append(gateway)
            saveRecentGateway(gateway)
        }
    }

    /// Remove a gateway from the list
    func removeGateway(_ gateway: DiscoveredGateway) {
        discoveredGateways.removeAll { $0.id == gateway.id }
        removeRecentGateway(gateway)
    }

    /// Check if a specific gateway is online
    func checkGateway(_ gateway: DiscoveredGateway) async -> Bool {
        let url = URL(string: "http://\(gateway.host):\(gateway.port)/health")!

        do {
            let config = URLSessionConfiguration.default
            config.timeoutIntervalForRequest = 3
            let session = URLSession(configuration: config)

            let (_, response) = try await session.data(from: url)
            if let httpResponse = response as? HTTPURLResponse {
                return httpResponse.statusCode == 200
            }
        } catch {
            logger.debug("gateway check failed for \(gateway.host): \(error.localizedDescription)")
        }

        return false
    }

    // MARK: - Bonjour Discovery

    private func scanBonjour() async {
        let descriptor = NWBrowser.Descriptor.bonjour(type: "_nexus._tcp", domain: nil)
        let parameters = NWParameters()
        parameters.includePeerToPeer = true

        let browser = NWBrowser(for: descriptor, using: parameters)
        self.browser = browser

        browser.browseResultsChangedHandler = { [weak self] results, changes in
            Task { @MainActor in
                self?.handleBonjourResults(results)
            }
        }

        browser.stateUpdateHandler = { [weak self] state in
            Task { @MainActor in
                switch state {
                case .ready:
                    self?.logger.debug("bonjour browser ready")
                case .failed(let error):
                    self?.logger.error("bonjour browser failed: \(error.localizedDescription)")
                default:
                    break
                }
            }
        }

        browser.start(queue: .main)

        // Wait for results
        try? await Task.sleep(for: .seconds(5))

        browser.cancel()
        self.browser = nil
    }

    private func handleBonjourResults(_ results: Set<NWBrowser.Result>) {
        for result in results {
            if case let .service(name, type, domain, _) = result.endpoint {
                // Resolve the service to get host/port
                resolveService(name: name, type: type, domain: domain)
            }
        }
    }

    private func resolveService(name: String, type: String, domain: String) {
        let endpoint = NWEndpoint.service(name: name, type: type, domain: domain, interface: nil)
        let parameters = NWParameters.tcp

        let connection = NWConnection(to: endpoint, using: parameters)

        connection.stateUpdateHandler = { [weak self] state in
            if case .ready = state {
                if let endpoint = connection.currentPath?.remoteEndpoint,
                   case let .hostPort(host, port) = endpoint {
                    let hostString = host.debugDescription.replacingOccurrences(of: "%.*", with: "", options: .regularExpression)

                    Task { @MainActor in
                        let gateway = DiscoveredGateway(
                            id: "bonjour-\(name)",
                            name: name,
                            host: hostString,
                            port: Int(port.rawValue),
                            source: .bonjour,
                            isOnline: true
                        )

                        if let existing = self?.discoveredGateways.firstIndex(where: { $0.id == gateway.id }) {
                            self?.discoveredGateways[existing] = gateway
                        } else {
                            self?.discoveredGateways.append(gateway)
                        }
                    }
                }
                connection.cancel()
            }
        }

        connection.start(queue: .main)

        // Cancel after timeout
        DispatchQueue.main.asyncAfter(deadline: .now() + 3) {
            connection.cancel()
        }
    }

    // MARK: - Tailscale Discovery

    private func scanTailscale() async {
        guard TailscaleService.shared.isAvailable else {
            logger.debug("tailscale not available, skipping scan")
            return
        }

        // Check if there's a Tailscale IP we can use
        if let ip = TailscaleService.shared.ipAddress {
            let gateway = DiscoveredGateway(
                id: "tailscale-local",
                name: TailscaleService.shared.hostname ?? "This Mac (Tailscale)",
                host: ip,
                port: AppStateStore.shared.gatewayPort,
                source: .tailscale,
                isOnline: true
            )

            if !discoveredGateways.contains(where: { $0.id == gateway.id }) {
                discoveredGateways.append(gateway)
            }
        }

        // TODO: Query Tailscale for other devices running Nexus
        // This would require the Tailscale API or a custom discovery protocol
    }

    // MARK: - Recent Gateways

    private func loadRecentGateways() {
        guard let data = UserDefaults.standard.data(forKey: "recentGateways"),
              let decoded = try? JSONDecoder().decode([RecentGateway].self, from: data) else {
            return
        }

        for recent in decoded {
            let gateway = DiscoveredGateway(
                id: "recent-\(recent.host):\(recent.port)",
                name: recent.name,
                host: recent.host,
                port: recent.port,
                source: .recent,
                lastSeen: recent.lastSeen
            )
            discoveredGateways.append(gateway)
        }
    }

    private func saveRecentGateway(_ gateway: DiscoveredGateway) {
        var recent = loadRecentGatewayData()

        // Remove existing entry with same host:port
        recent.removeAll { $0.host == gateway.host && $0.port == gateway.port }

        // Add new entry
        recent.insert(RecentGateway(
            name: gateway.name,
            host: gateway.host,
            port: gateway.port,
            lastSeen: Date()
        ), at: 0)

        // Keep only last 10
        recent = Array(recent.prefix(10))

        if let data = try? JSONEncoder().encode(recent) {
            UserDefaults.standard.set(data, forKey: "recentGateways")
        }
    }

    private func removeRecentGateway(_ gateway: DiscoveredGateway) {
        var recent = loadRecentGatewayData()
        recent.removeAll { $0.host == gateway.host && $0.port == gateway.port }

        if let data = try? JSONEncoder().encode(recent) {
            UserDefaults.standard.set(data, forKey: "recentGateways")
        }
    }

    private func loadRecentGatewayData() -> [RecentGateway] {
        guard let data = UserDefaults.standard.data(forKey: "recentGateways"),
              let decoded = try? JSONDecoder().decode([RecentGateway].self, from: data) else {
            return []
        }
        return decoded
    }

    private func checkRecentGateways() async {
        for index in discoveredGateways.indices where discoveredGateways[index].source == .recent {
            let gateway = discoveredGateways[index]
            let isOnline = await checkGateway(gateway)
            discoveredGateways[index].isOnline = isOnline
        }
    }

    private struct RecentGateway: Codable {
        let name: String
        let host: String
        let port: Int
        let lastSeen: Date
    }
}
