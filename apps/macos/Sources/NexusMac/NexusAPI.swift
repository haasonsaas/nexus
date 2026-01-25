import Foundation

final class NexusAPI {
    let baseURL: String
    let apiKey: String

    init(baseURL: String, apiKey: String) {
        self.baseURL = baseURL.trimmingCharacters(in: .whitespacesAndNewlines)
        self.apiKey = apiKey
    }

    func fetchStatus() async throws -> SystemStatus {
        try await request(path: "/api/status")
    }

    func fetchNodes() async throws -> [NodeSummary] {
        let response: NodesResponse = try await request(path: "/api/nodes")
        return response.nodes
    }

    func fetchNodeTools(edgeID: String) async throws -> [NodeToolSummary] {
        let response: NodeToolsResponse = try await request(path: "/api/nodes/\(edgeID)/tools")
        return response.tools
    }

    func fetchArtifacts() async throws -> [ArtifactSummary] {
        let response: ArtifactListResponse = try await request(path: "/api/artifacts")
        return response.artifacts
    }

    func fetchSessions(page: Int = 1, size: Int = 50, channel: String? = nil, agent: String? = nil) async throws -> SessionListResponse {
        var items = [
            URLQueryItem(name: "page", value: String(page)),
            URLQueryItem(name: "size", value: String(size)),
        ]
        if let channel = channel, !channel.isEmpty {
            items.append(URLQueryItem(name: "channel", value: channel))
        }
        if let agent = agent, !agent.isEmpty {
            items.append(URLQueryItem(name: "agent", value: agent))
        }
        return try await request(path: "/api/sessions", queryItems: items)
    }

    func fetchSessionMessages(sessionID: String, page: Int = 1, size: Int = 50) async throws -> SessionMessagesResponse {
        let items = [
            URLQueryItem(name: "page", value: String(page)),
            URLQueryItem(name: "size", value: String(size)),
        ]
        return try await request(path: "/api/sessions/\(sessionID)/messages", queryItems: items)
    }

    func invokeTool(edgeID: String, toolName: String, input: String, approved: Bool) async throws -> ToolInvocationResult {
        let payload = [
            "input": input,
            "approved": approved,
        ] as [String: Any]
        let data = try JSONSerialization.data(withJSONObject: payload, options: [])
        return try await request(path: "/api/nodes/\(edgeID)/tools/\(toolName)", method: "POST", body: data)
    }

    func fetchProviders() async throws -> [ProviderStatus] {
        let response: ProvidersResponse = try await request(path: "/api/providers")
        return response.providers
    }

    func fetchProviderQR(name: String) async throws -> Data {
        try await requestData(path: "/api/providers/\(name)/qr", accept: "image/png")
    }

    func fetchSkills() async throws -> [SkillSummary] {
        let response: SkillsResponse = try await request(path: "/api/skills")
        return response.skills
    }

    func refreshSkills() async throws {
        _ = try await requestData(path: "/api/skills/refresh", method: "POST")
    }

    func fetchCron() async throws -> CronResponse {
        try await request(path: "/api/cron")
    }

    func fetchUsage() async throws -> GatewayUsageSummary {
        try await request(path: "/api/usage")
    }

    private func request<T: Decodable>(path: String, method: String = "GET", body: Data? = nil, queryItems: [URLQueryItem] = []) async throws -> T {
        let data = try await requestData(path: path, method: method, body: body, queryItems: queryItems)
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return try decoder.decode(T.self, from: data)
    }

    private func requestData(path: String, method: String = "GET", body: Data? = nil, queryItems: [URLQueryItem] = [], accept: String = "application/json") async throws -> Data {
        let url = try makeURL(path: path, queryItems: queryItems)
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.setValue(accept, forHTTPHeaderField: "Accept")
        request.setValue(apiKey, forHTTPHeaderField: "X-API-Key")
        if let body = body {
            request.httpBody = body
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }

        let (data, response) = try await URLSession.shared.data(for: request)
        if let http = response as? HTTPURLResponse, !(200...299).contains(http.statusCode) {
            if let apiError = try? JSONDecoder().decode(APIError.self, from: data) {
                throw NSError(domain: "NexusAPI", code: http.statusCode, userInfo: [NSLocalizedDescriptionKey: apiError.error])
            }
            let message = String(data: data, encoding: .utf8) ?? "HTTP \(http.statusCode)"
            throw NSError(domain: "NexusAPI", code: http.statusCode, userInfo: [NSLocalizedDescriptionKey: message])
        }
        return data
    }

    private func makeURL(path: String, queryItems: [URLQueryItem]) throws -> URL {
        let base = baseURL.hasSuffix("/") ? String(baseURL.dropLast()) : baseURL
        guard var components = URLComponents(string: base) else {
            throw NSError(domain: "NexusAPI", code: 1, userInfo: [NSLocalizedDescriptionKey: "Invalid base URL"])
        }
        let basePath = components.path
        components.path = basePath + path
        components.queryItems = queryItems.isEmpty ? nil : queryItems
        guard let url = components.url else {
            throw NSError(domain: "NexusAPI", code: 1, userInfo: [NSLocalizedDescriptionKey: "Invalid URL"])
        }
        return url
    }
}
