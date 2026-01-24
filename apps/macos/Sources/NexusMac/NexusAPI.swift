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

    func invokeTool(edgeID: String, toolName: String, input: String, approved: Bool) async throws -> ToolInvocationResult {
        let payload = [
            "input": input,
            "approved": approved,
        ] as [String: Any]
        let data = try JSONSerialization.data(withJSONObject: payload, options: [])
        return try await request(path: "/api/nodes/\(edgeID)/tools/\(toolName)", method: "POST", body: data)
    }

    private func request<T: Decodable>(path: String, method: String = "GET", body: Data? = nil) async throws -> T {
        let base = baseURL.hasSuffix("/") ? String(baseURL.dropLast()) : baseURL
        guard let url = URL(string: base + path) else {
            throw NSError(domain: "NexusAPI", code: 1, userInfo: [NSLocalizedDescriptionKey: "Invalid base URL"])
        }

        var request = URLRequest(url: url)
        request.httpMethod = method
        request.setValue("application/json", forHTTPHeaderField: "Accept")
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

        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return try decoder.decode(T.self, from: data)
    }
}
