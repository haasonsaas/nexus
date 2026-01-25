import Foundation
import OSLog

/// Bridges different session types and manages session state.
/// Enables seamless transitions between chat, voice, and agent modes.
@MainActor
@Observable
final class SessionBridge {
    static let shared = SessionBridge()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "session.bridge")

    private(set) var activeSessions: [Session] = []
    private(set) var primarySessionId: String?

    struct Session: Identifiable, Codable {
        let id: String
        var type: SessionType
        var status: SessionStatus
        var metadata: SessionMetadata
        var createdAt: Date
        var lastActiveAt: Date
        var parentId: String?

        enum SessionType: String, Codable {
            case chat
            case voice
            case agent
            case computerUse
            case mcp
        }

        enum SessionStatus: String, Codable {
            case active
            case paused
            case completed
            case error
        }

        struct SessionMetadata: Codable {
            var title: String?
            var model: String?
            var provider: String?
            var tokenCount: Int?
            var messageCount: Int?
            var tags: [String]?
        }
    }

    // MARK: - Session Lifecycle

    /// Create a new session
    func createSession(type: Session.SessionType, metadata: Session.SessionMetadata = .init()) -> Session {
        let session = Session(
            id: generateSessionId(),
            type: type,
            status: .active,
            metadata: metadata,
            createdAt: Date(),
            lastActiveAt: Date(),
            parentId: primarySessionId
        )

        activeSessions.append(session)
        logger.info("session created id=\(session.id) type=\(type.rawValue)")

        return session
    }

    /// End a session
    func endSession(id: String, status: Session.SessionStatus = .completed) {
        guard let index = activeSessions.firstIndex(where: { $0.id == id }) else { return }

        activeSessions[index].status = status
        activeSessions[index].lastActiveAt = Date()

        if primarySessionId == id {
            primarySessionId = nil
        }

        logger.info("session ended id=\(id) status=\(status.rawValue)")
    }

    /// Set the primary session
    func setPrimarySession(id: String) {
        guard activeSessions.contains(where: { $0.id == id }) else { return }
        primarySessionId = id
        updateActivity(sessionId: id)
        logger.debug("primary session set id=\(id)")
    }

    /// Update session activity timestamp
    func updateActivity(sessionId: String) {
        guard let index = activeSessions.firstIndex(where: { $0.id == sessionId }) else { return }
        activeSessions[index].lastActiveAt = Date()
    }

    /// Update session metadata
    func updateMetadata(sessionId: String, update: (inout Session.SessionMetadata) -> Void) {
        guard let index = activeSessions.firstIndex(where: { $0.id == sessionId }) else { return }
        update(&activeSessions[index].metadata)
        activeSessions[index].lastActiveAt = Date()
    }

    // MARK: - Session Bridging

    /// Bridge from one session type to another
    func bridge(from sourceId: String, to targetType: Session.SessionType) -> Session? {
        guard let source = activeSessions.first(where: { $0.id == sourceId }) else { return nil }

        // Create new session inheriting context
        var metadata = source.metadata
        metadata.title = "Bridged from \(source.type.rawValue)"

        let target = Session(
            id: generateSessionId(),
            type: targetType,
            status: .active,
            metadata: metadata,
            createdAt: Date(),
            lastActiveAt: Date(),
            parentId: sourceId
        )

        activeSessions.append(target)
        logger.info("session bridged from=\(sourceId) to=\(target.id) type=\(targetType.rawValue)")

        return target
    }

    /// Get session lineage (parent chain)
    func getLineage(sessionId: String) -> [Session] {
        var lineage: [Session] = []
        var currentId: String? = sessionId

        while let id = currentId,
              let session = activeSessions.first(where: { $0.id == id }) {
            lineage.append(session)
            currentId = session.parentId
        }

        return lineage
    }

    // MARK: - Gateway Integration

    /// Notify gateway of session changes
    func notifyGateway(event: SessionEvent) async {
        do {
            _ = try await ControlChannel.shared.request(
                method: "session.event",
                params: [
                    "event": event.type.rawValue,
                    "sessionId": event.sessionId,
                    "data": event.data ?? [:]
                ] as [String: AnyHashable]
            )
        } catch {
            logger.warning("failed to notify gateway: \(error.localizedDescription)")
        }
    }

    struct SessionEvent {
        let type: EventType
        let sessionId: String
        let data: [String: Any]?

        enum EventType: String {
            case created
            case updated
            case ended
            case bridged
            case primaryChanged
        }
    }

    // MARK: - Private

    private func generateSessionId() -> String {
        let timestamp = Int(Date().timeIntervalSince1970 * 1000)
        let random = Int.random(in: 0..<10000)
        return "session_\(timestamp)_\(random)"
    }
}
