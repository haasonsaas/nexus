import Foundation

/// Custom URL scheme for serving canvas content from session directories.
/// URLs follow the format: nexus-canvas://session/{sessionId}/path/to/file
enum CanvasScheme {
    /// The custom URL scheme identifier
    static let scheme = "nexus-canvas"

    /// Host for session-scoped resources
    static let sessionHost = "session"

    // MARK: - URL Construction

    /// Construct a canvas URL for a file within a session directory
    /// - Parameters:
    ///   - session: The session identifier
    ///   - path: The path relative to the session's canvas directory
    /// - Returns: A fully formed canvas URL
    static func makeURL(session: String, path: String = "") -> URL? {
        var components = URLComponents()
        components.scheme = scheme
        components.host = sessionHost

        // Build path: /{sessionId}/path/to/file
        var pathComponents = [session]
        if !path.isEmpty {
            // Normalize path - remove leading slash if present
            let normalizedPath = path.hasPrefix("/") ? String(path.dropFirst()) : path
            pathComponents.append(normalizedPath)
        }

        components.path = "/" + pathComponents.joined(separator: "/")

        return components.url
    }

    /// Construct a canvas URL for the index file of a session
    /// - Parameter session: The session identifier
    /// - Returns: A URL pointing to the session's index.html
    static func makeIndexURL(session: String) -> URL? {
        makeURL(session: session, path: "index.html")
    }

    // MARK: - URL Parsing

    /// Parsed components from a canvas URL
    struct ParsedURL {
        let sessionId: String
        let path: String

        /// The file path relative to the session directory
        var relativePath: String {
            path.isEmpty ? "index.html" : path
        }
    }

    /// Parse a canvas URL into its components
    /// - Parameter url: The URL to parse
    /// - Returns: Parsed components if valid, nil otherwise
    static func parse(url: URL) -> ParsedURL? {
        guard url.scheme == scheme else { return nil }
        guard url.host == sessionHost else { return nil }

        // Path format: /{sessionId}/path/to/file
        let pathComponents = url.pathComponents.filter { $0 != "/" }
        guard !pathComponents.isEmpty else { return nil }

        let sessionId = pathComponents[0]
        let filePath = pathComponents.dropFirst().joined(separator: "/")

        return ParsedURL(sessionId: sessionId, path: filePath)
    }

    /// Check if a URL uses the canvas scheme
    /// - Parameter url: The URL to check
    /// - Returns: true if the URL uses the nexus-canvas scheme
    static func isCanvasURL(_ url: URL) -> Bool {
        url.scheme == scheme
    }

    // MARK: - Path Resolution

    /// Resolve a canvas URL to a local file path
    /// - Parameters:
    ///   - url: The canvas URL
    ///   - baseDirectory: The base directory containing session directories
    /// - Returns: The resolved file URL, or nil if invalid
    static func resolveToFilePath(url: URL, baseDirectory: URL) -> URL? {
        guard let parsed = parse(url: url) else { return nil }

        var fileURL = baseDirectory
            .appendingPathComponent(parsed.sessionId)
            .appendingPathComponent("canvas")

        if parsed.path.isEmpty {
            fileURL.appendPathComponent("index.html")
        } else {
            fileURL.appendPathComponent(parsed.path)
        }

        return fileURL
    }
}
