import Foundation
import OSLog

/// Unified gateway connectivity coordinator.
/// Provides a single point of access for endpoint state, URL resolution,
/// and connection management across the application.
@MainActor
@Observable
final class GatewayConnectivityCoordinator {
    static let shared = GatewayConnectivityCoordinator()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "gateway.connectivity")

    // MARK: - State

    private(set) var currentEndpointURL: URL?
    private(set) var lastRefreshAt: Date?
    private(set) var refreshError: Error?

    private var modeObservationTask: Task<Void, Never>?
    private var endpointContinuations: [UUID: AsyncStream<EndpointState>.Continuation] = [:]

    // MARK: - Endpoint State

    struct EndpointState: Equatable, Sendable {
        let url: URL?
        let mode: ConnectionMode
        let isConnected: Bool
        let hostLabel: String
        let timestamp: Date

        static var disconnected: EndpointState {
            EndpointState(
                url: nil,
                mode: .unconfigured,
                isConnected: false,
                hostLabel: "Not configured",
                timestamp: Date()
            )
        }
    }

    // MARK: - Initialization

    private init() {
        logger.debug("coordinator initialized")
        startModeObservation()
    }

    @MainActor
    deinit {
        modeObservationTask?.cancel()
        for continuation in endpointContinuations.values {
            continuation.finish()
        }
    }

    // MARK: - Computed Properties

    /// The current effective gateway URL based on connection mode and settings.
    var effectiveURL: URL? {
        resolveEffectiveURL()
    }

    /// Human-readable description of the current host.
    var hostLabel: String {
        formatHostLabel()
    }

    /// Whether the gateway is currently connected.
    var isConnected: Bool {
        ControlChannel.shared.state == .connected
    }

    /// The current connection mode from app state.
    var connectionMode: ConnectionMode {
        AppStateStore.shared.connectionMode
    }

    /// Current endpoint state snapshot.
    var currentState: EndpointState {
        EndpointState(
            url: effectiveURL,
            mode: connectionMode,
            isConnected: isConnected,
            hostLabel: hostLabel,
            timestamp: Date()
        )
    }

    // MARK: - AsyncStream Subscription

    /// Subscribe to endpoint state changes.
    /// Returns an AsyncStream that emits endpoint state updates.
    func subscribe() -> AsyncStream<EndpointState> {
        let id = UUID()
        logger.debug("new subscription created id=\(id.uuidString)")

        return AsyncStream { [weak self] continuation in
            guard let self else {
                continuation.finish()
                return
            }

            Task { @MainActor in
                self.endpointContinuations[id] = continuation

                // Emit current state immediately
                continuation.yield(self.currentState)

                continuation.onTermination = { [weak self] _ in
                    Task { @MainActor in
                        self?.endpointContinuations.removeValue(forKey: id)
                        self?.logger.debug("subscription terminated id=\(id.uuidString)")
                    }
                }
            }
        }
    }

    // MARK: - URL Resolution

    /// Resolves the effective gateway URL based on current mode and settings.
    private func resolveEffectiveURL() -> URL? {
        let state = AppStateStore.shared
        let port = GatewayEnvironment.gatewayPort()

        switch state.connectionMode {
        case .unconfigured:
            return nil

        case .local:
            var components = URLComponents()
            components.scheme = "http"
            components.host = "127.0.0.1"
            components.port = port
            return components.url

        case .remote:
            guard let host = state.remoteHost, !host.isEmpty else {
                return nil
            }
            var components = URLComponents()
            components.scheme = state.gatewayUseTLS ? "https" : "http"
            components.host = host
            components.port = port
            return components.url
        }
    }

    /// Resolves WebSocket URL for control channel.
    func resolveWebSocketURL(path: String = "control") -> URL? {
        guard let baseURL = effectiveURL else { return nil }

        var components = URLComponents(url: baseURL, resolvingAgainstBaseURL: false)
        if baseURL.scheme == "https" {
            components?.scheme = "wss"
        } else {
            components?.scheme = "ws"
        }
        components?.path = "/\(path)"

        return components?.url
    }

    // MARK: - Host Label Formatting

    /// Formats a human-readable host label based on current mode.
    private func formatHostLabel() -> String {
        let state = AppStateStore.shared
        let port = GatewayEnvironment.gatewayPort()

        switch state.connectionMode {
        case .unconfigured:
            return "Not configured"

        case .local:
            return "Local (\(port))"

        case .remote:
            guard let host = state.remoteHost, !host.isEmpty else {
                return "Remote (not configured)"
            }

            let user = state.remoteUser
            if !user.isEmpty && user != "root" {
                return "Remote (\(user)@\(host))"
            } else {
                return "Remote (\(host))"
            }
        }
    }

    // MARK: - Refresh

    /// Refresh endpoint state and notify subscribers.
    func refresh() async {
        logger.info("refreshing endpoint state")
        lastRefreshAt = Date()
        refreshError = nil

        // Update current URL
        currentEndpointURL = resolveEffectiveURL()

        // Notify the control channel to refresh
        await ControlChannel.shared.refreshEndpoint(reason: "connectivity coordinator refresh")

        // Broadcast state to all subscribers
        broadcastState()

        logger.info("endpoint refreshed url=\(self.currentEndpointURL?.absoluteString ?? "nil")")
    }

    /// Force reconnection with the current settings.
    func reconnect() async {
        logger.info("forcing reconnection")
        await ConnectionModeCoordinator.shared.reconnect()
        broadcastState()
    }

    // MARK: - Mode Observation

    /// Starts observing connection mode changes for auto-refresh.
    private func startModeObservation() {
        modeObservationTask?.cancel()
        modeObservationTask = Task { [weak self] in
            guard let self else { return }

            var lastMode = AppStateStore.shared.connectionMode
            var lastHost = AppStateStore.shared.remoteHost
            var lastPort = AppStateStore.shared.gatewayPort

            while !Task.isCancelled {
                try? await Task.sleep(for: .milliseconds(500))

                guard !Task.isCancelled else { break }

                let currentMode = AppStateStore.shared.connectionMode
                let currentHost = AppStateStore.shared.remoteHost
                let currentPort = AppStateStore.shared.gatewayPort

                // Check if settings changed
                if currentMode != lastMode ||
                   currentHost != lastHost ||
                   currentPort != lastPort {

                    logger.info("connection settings changed mode=\(currentMode.rawValue)")

                    lastMode = currentMode
                    lastHost = currentHost
                    lastPort = currentPort

                    // Auto-refresh on changes
                    await refresh()
                }
            }
        }
    }

    // MARK: - State Broadcasting

    /// Broadcasts current state to all subscribers.
    private func broadcastState() {
        let state = currentState
        logger.debug("broadcasting state to \(self.endpointContinuations.count) subscribers")

        for continuation in endpointContinuations.values {
            continuation.yield(state)
        }
    }

    // MARK: - Convenience Methods

    /// Check if the gateway is reachable.
    func checkHealth(timeout: TimeInterval = 5) async -> Bool {
        do {
            _ = try await ControlChannel.shared.health(timeout: timeout)
            return true
        } catch {
            logger.warning("health check failed: \(error.localizedDescription)")
            return false
        }
    }

    /// Get a configured URLRequest for the gateway API.
    func makeRequest(path: String = "") -> URLRequest? {
        guard let baseURL = effectiveURL else { return nil }

        var url = baseURL
        if !path.isEmpty {
            url = baseURL.appendingPathComponent(path)
        }

        return URLRequest(url: url)
    }

    /// Returns connection status description for UI display.
    var statusDescription: String {
        switch ControlChannel.shared.state {
        case .disconnected:
            return "Disconnected"
        case .connecting:
            return "Connecting..."
        case .connected:
            return "Connected"
        case .degraded(let reason):
            return "Degraded: \(reason)"
        }
    }
}

// MARK: - Notification Support

extension Notification.Name {
    static let gatewayConnectivityChanged = Notification.Name("nexus.gateway.connectivity.changed")
}

extension GatewayConnectivityCoordinator {
    /// Posts a notification when connectivity state changes.
    func notifyStateChange() {
        NotificationCenter.default.post(
            name: .gatewayConnectivityChanged,
            object: currentState
        )
    }
}
