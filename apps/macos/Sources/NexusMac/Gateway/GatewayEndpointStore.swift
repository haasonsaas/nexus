import Foundation
import OSLog
import Security

/// Manages gateway endpoint configuration and authentication.
/// Supports local, remote, and token-authenticated connections.
@MainActor
@Observable
final class GatewayEndpointStore {
    static let shared = GatewayEndpointStore()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "endpoint")
    private let keychainService = "com.nexus.mac.gateway"

    private(set) var currentEndpoint: Endpoint?
    private(set) var savedEndpoints: [Endpoint] = []
    private(set) var connectionMode: ConnectionMode = .local

    enum ConnectionMode: String, Codable {
        case local
        case remote
        case tunnel
    }

    struct Endpoint: Identifiable, Codable, Equatable {
        let id: String
        var name: String
        var host: String
        var port: Int
        var useTLS: Bool
        var authMethod: AuthMethod
        var isDefault: Bool

        var baseURL: URL? {
            let scheme = useTLS ? "https" : "http"
            return URL(string: "\(scheme)://\(host):\(port)")
        }

        enum AuthMethod: String, Codable {
            case none
            case token
            case password
        }

        static var local: Endpoint {
            Endpoint(
                id: "local",
                name: "Local Gateway",
                host: "localhost",
                port: GatewayEnvironment.gatewayPort(),
                useTLS: false,
                authMethod: .none,
                isDefault: true
            )
        }
    }

    struct AuthCredentials {
        let token: String?
        let password: String?
    }

    init() {
        loadSavedEndpoints()
        currentEndpoint = savedEndpoints.first(where: { $0.isDefault }) ?? .local
    }

    // MARK: - Endpoint Management

    /// Add or update an endpoint
    func saveEndpoint(_ endpoint: Endpoint) {
        if let index = savedEndpoints.firstIndex(where: { $0.id == endpoint.id }) {
            savedEndpoints[index] = endpoint
        } else {
            savedEndpoints.append(endpoint)
        }
        persistEndpoints()
        logger.info("endpoint saved id=\(endpoint.id) name=\(endpoint.name)")
    }

    /// Remove an endpoint
    func removeEndpoint(id: String) {
        savedEndpoints.removeAll { $0.id == id }
        deleteCredentials(forEndpointId: id)
        persistEndpoints()
        logger.info("endpoint removed id=\(id)")
    }

    /// Select an endpoint as current
    func selectEndpoint(id: String) {
        guard let endpoint = savedEndpoints.first(where: { $0.id == id }) else {
            logger.warning("endpoint not found id=\(id)")
            return
        }
        currentEndpoint = endpoint

        // Update default
        for i in savedEndpoints.indices {
            savedEndpoints[i].isDefault = (savedEndpoints[i].id == id)
        }
        persistEndpoints()

        // Determine connection mode
        if endpoint.id == "local" {
            connectionMode = .local
        } else if RemoteTunnelManager.shared.isConnected {
            connectionMode = .tunnel
        } else {
            connectionMode = .remote
        }

        logger.info("endpoint selected id=\(id) mode=\(self.connectionMode.rawValue)")
    }

    // MARK: - Authentication

    /// Store credentials for an endpoint
    func storeCredentials(_ credentials: AuthCredentials, forEndpointId id: String) throws {
        let account = "endpoint:\(id)"

        // Build credential data
        var data: [String: String] = [:]
        if let token = credentials.token {
            data["token"] = token
        }
        if let password = credentials.password {
            data["password"] = password
        }

        let credentialData = try JSONEncoder().encode(data)

        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: keychainService,
            kSecAttrAccount as String: account
        ]

        // Delete existing
        SecItemDelete(query as CFDictionary)

        // Add new
        var addQuery = query
        addQuery[kSecValueData as String] = credentialData

        let status = SecItemAdd(addQuery as CFDictionary, nil)
        guard status == errSecSuccess else {
            throw KeychainError.saveFailed(status)
        }

        logger.debug("credentials stored for endpoint=\(id)")
    }

    /// Retrieve credentials for an endpoint
    func getCredentials(forEndpointId id: String) -> AuthCredentials? {
        let account = "endpoint:\(id)"

        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: keychainService,
            kSecAttrAccount as String: account,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne
        ]

        var result: AnyObject?
        let status = SecItemCopyMatching(query as CFDictionary, &result)

        guard status == errSecSuccess,
              let data = result as? Data,
              let dict = try? JSONDecoder().decode([String: String].self, from: data) else {
            return nil
        }

        return AuthCredentials(token: dict["token"], password: dict["password"])
    }

    /// Delete credentials for an endpoint
    func deleteCredentials(forEndpointId id: String) {
        let account = "endpoint:\(id)"

        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: keychainService,
            kSecAttrAccount as String: account
        ]

        SecItemDelete(query as CFDictionary)
        logger.debug("credentials deleted for endpoint=\(id)")
    }

    // MARK: - URL Resolution

    /// Get the active gateway URL with authentication
    func resolveURL(path: String = "") async throws -> URLRequest {
        guard let endpoint = currentEndpoint,
              let baseURL = endpoint.baseURL else {
            throw EndpointError.noEndpoint
        }

        var url = baseURL
        if !path.isEmpty {
            url = baseURL.appendingPathComponent(path)
        }

        var request = URLRequest(url: url)

        // Add authentication if needed
        if endpoint.authMethod != .none {
            if let credentials = getCredentials(forEndpointId: endpoint.id) {
                if let token = credentials.token {
                    request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
                } else if let password = credentials.password {
                    let auth = "nexus:\(password)".data(using: .utf8)!.base64EncodedString()
                    request.setValue("Basic \(auth)", forHTTPHeaderField: "Authorization")
                }
            }
        }

        return request
    }

    /// Get WebSocket URL for the current endpoint
    func resolveWebSocketURL(path: String = "ws") -> URL? {
        guard let endpoint = currentEndpoint,
              let baseURL = endpoint.baseURL else {
            return nil
        }

        var components = URLComponents(url: baseURL, resolvingAgainstBaseURL: false)
        components?.scheme = endpoint.useTLS ? "wss" : "ws"
        components?.path = "/\(path)"

        return components?.url
    }

    // MARK: - Persistence

    private func loadSavedEndpoints() {
        let url = endpointsFileURL()
        guard FileManager.default.fileExists(atPath: url.path),
              let data = try? Data(contentsOf: url),
              let endpoints = try? JSONDecoder().decode([Endpoint].self, from: data) else {
            savedEndpoints = [.local]
            return
        }
        savedEndpoints = endpoints
        logger.debug("loaded \(endpoints.count) saved endpoints")
    }

    private func persistEndpoints() {
        let url = endpointsFileURL()
        do {
            let data = try JSONEncoder().encode(savedEndpoints)
            try data.write(to: url)
        } catch {
            logger.error("failed to persist endpoints: \(error.localizedDescription)")
        }
    }

    private func endpointsFileURL() -> URL {
        let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        let nexusDir = appSupport.appendingPathComponent("Nexus")
        try? FileManager.default.createDirectory(at: nexusDir, withIntermediateDirectories: true)
        return nexusDir.appendingPathComponent("endpoints.json")
    }
}

enum EndpointError: LocalizedError {
    case noEndpoint
    case invalidURL
    case authRequired

    var errorDescription: String? {
        switch self {
        case .noEndpoint:
            return "No gateway endpoint configured"
        case .invalidURL:
            return "Invalid endpoint URL"
        case .authRequired:
            return "Authentication required for this endpoint"
        }
    }
}

enum KeychainError: LocalizedError {
    case saveFailed(OSStatus)
    case loadFailed(OSStatus)

    var errorDescription: String? {
        switch self {
        case .saveFailed(let status):
            return "Failed to save to keychain: \(status)"
        case .loadFailed(let status):
            return "Failed to load from keychain: \(status)"
        }
    }
}
