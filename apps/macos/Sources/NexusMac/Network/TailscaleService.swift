import AppKit
import Darwin
import Foundation
import OSLog

/// Manages Tailscale integration for remote gateway connectivity.
@MainActor
@Observable
final class TailscaleService {
    static let shared = TailscaleService()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "tailscale")

    /// Tailscale local API endpoint
    private static let apiEndpoint = "http://100.100.100.100/api/data"
    private static let apiTimeout: TimeInterval = 5.0

    // MARK: - State

    /// Whether Tailscale app is installed
    private(set) var isInstalled = false

    /// Whether Tailscale is running
    private(set) var isRunning = false

    /// This device's Tailscale hostname (e.g., "my-mac.tailnet.ts.net")
    private(set) var hostname: String?

    /// This device's Tailscale IPv4 address
    private(set) var ipAddress: String?

    /// Error message if status check fails
    private(set) var statusError: String?

    /// Last refresh time
    private(set) var lastRefresh: Date?

    private var refreshTask: Task<Void, Never>?

    private init() {
        Task { await refresh() }
    }

    // MARK: - Public API

    /// Refresh Tailscale status
    func refresh() async {
        let previousIP = ipAddress

        isInstalled = checkInstalled()

        if !isInstalled {
            isRunning = false
            hostname = nil
            ipAddress = nil
            statusError = "Tailscale is not installed"
            logger.info("tailscale not installed")
        } else if let response = await fetchStatus() {
            isRunning = response.status.lowercased() == "running"

            if isRunning {
                let deviceName = response.deviceName
                    .lowercased()
                    .replacingOccurrences(of: " ", with: "-")
                let tailnetName = response.tailnetName
                    .replacingOccurrences(of: ".ts.net", with: "")
                    .replacingOccurrences(of: ".tailscale.net", with: "")

                hostname = "\(deviceName).\(tailnetName).ts.net"
                ipAddress = response.ipv4
                statusError = nil

                logger.info("tailscale running: \(self.hostname ?? "nil") @ \(self.ipAddress ?? "nil")")
            } else {
                hostname = nil
                ipAddress = nil
                statusError = "Tailscale is not running"
            }
        } else {
            isRunning = false
            hostname = nil
            ipAddress = nil
            statusError = "Please start the Tailscale app"
            logger.info("tailscale API not responding")
        }

        // Fallback: detect from network interfaces
        if ipAddress == nil, let fallback = detectTailnetIP() {
            ipAddress = fallback
            isRunning = true
            statusError = nil
            logger.info("tailscale IP detected from interface: \(fallback)")
        }

        lastRefresh = Date()

        // Notify endpoint store if IP changed
        if previousIP != ipAddress {
            await GatewayEndpointStore.shared.refresh()
        }
    }

    /// Start periodic status refresh
    func startMonitoring(interval: TimeInterval = 30) {
        refreshTask?.cancel()
        refreshTask = Task {
            while !Task.isCancelled {
                await refresh()
                try? await Task.sleep(for: .seconds(interval))
            }
        }
    }

    /// Stop periodic monitoring
    func stopMonitoring() {
        refreshTask?.cancel()
        refreshTask = nil
    }

    // MARK: - Actions

    /// Open the Tailscale app
    func openApp() {
        if let url = URL(string: "file:///Applications/Tailscale.app") {
            NSWorkspace.shared.open(url)
        }
    }

    /// Open the Mac App Store page for Tailscale
    func openAppStore() {
        if let url = URL(string: "https://apps.apple.com/us/app/tailscale/id1475387142") {
            NSWorkspace.shared.open(url)
        }
    }

    /// Open the Tailscale download page
    func openDownloadPage() {
        if let url = URL(string: "https://tailscale.com/download/macos") {
            NSWorkspace.shared.open(url)
        }
    }

    /// Open the Tailscale setup guide
    func openSetupGuide() {
        if let url = URL(string: "https://tailscale.com/kb/1017/install/") {
            NSWorkspace.shared.open(url)
        }
    }

    // MARK: - Private

    private func checkInstalled() -> Bool {
        FileManager.default.fileExists(atPath: "/Applications/Tailscale.app")
    }

    private struct TailscaleResponse: Codable {
        let status: String
        let deviceName: String
        let tailnetName: String
        let ipv4: String?

        private enum CodingKeys: String, CodingKey {
            case status = "Status"
            case deviceName = "DeviceName"
            case tailnetName = "TailnetName"
            case ipv4 = "IPv4"
        }
    }

    private func fetchStatus() async -> TailscaleResponse? {
        guard let url = URL(string: Self.apiEndpoint) else { return nil }

        do {
            let config = URLSessionConfiguration.default
            config.timeoutIntervalForRequest = Self.apiTimeout
            let session = URLSession(configuration: config)

            let (data, response) = try await session.data(from: url)
            guard let httpResponse = response as? HTTPURLResponse,
                  httpResponse.statusCode == 200 else {
                return nil
            }

            return try JSONDecoder().decode(TailscaleResponse.self, from: data)
        } catch {
            logger.debug("tailscale API fetch failed: \(error.localizedDescription)")
            return nil
        }
    }

    private nonisolated func detectTailnetIP() -> String? {
        var addrList: UnsafeMutablePointer<ifaddrs>?
        guard getifaddrs(&addrList) == 0, let first = addrList else { return nil }
        defer { freeifaddrs(addrList) }

        for ptr in sequence(first: first, next: { $0.pointee.ifa_next }) {
            let flags = Int32(ptr.pointee.ifa_flags)
            let isUp = (flags & IFF_UP) != 0
            let isLoopback = (flags & IFF_LOOPBACK) != 0
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
            let ip = String(cString: buffer)

            if isTailnetIP(ip) {
                return ip
            }
        }

        return nil
    }

    private nonisolated func isTailnetIP(_ address: String) -> Bool {
        // Tailscale uses 100.64.0.0/10 (CGNAT space)
        let parts = address.split(separator: ".")
        guard parts.count == 4,
              let a = Int(parts[0]),
              let b = Int(parts[1]) else {
            return false
        }
        return a == 100 && b >= 64 && b <= 127
    }
}

// MARK: - Convenience

extension TailscaleService {
    /// Quick check if Tailscale is available for remote connections
    var isAvailable: Bool {
        isInstalled && isRunning && ipAddress != nil
    }

    /// Get the best address for remote connections
    var bestAddress: String? {
        hostname ?? ipAddress
    }
}
