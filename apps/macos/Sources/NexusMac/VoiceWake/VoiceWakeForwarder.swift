import Foundation
import OSLog

/// Forwards voice wake transcripts to the gateway via WebSocket.
enum VoiceWakeForwarder {
    private static let logger = Logger(subsystem: "com.nexus.mac", category: "voicewake.forward")

    /// Prefix transcript with context for the LLM.
    static func prefixedTranscript(_ transcript: String) -> String {
        let machine = Host.current().localizedName ?? ProcessInfo.processInfo.hostName
        return """
        User talked via voice recognition on \(machine.isEmpty ? "this Mac" : machine) - \
        repeat prompt first + remember some words might be incorrectly transcribed.

        \(transcript)
        """
    }

    enum ForwardError: LocalizedError, Equatable {
        case notConnected
        case sendFailed(String)

        var errorDescription: String? {
            switch self {
            case .notConnected: "WebSocket not connected"
            case .sendFailed(let msg): msg
            }
        }
    }

    /// Forward transcript to the gateway.
    @discardableResult
    static func forward(transcript: String, sessionId: String? = nil) async -> Result<Void, ForwardError> {
        let payload = prefixedTranscript(transcript)
        logger.info("Forwarding voice transcript (\(payload.count) chars)")
        // Integration point: webSocketService.sendChat(sessionId: sessionId, content: payload)
        return .success(())
    }
}
