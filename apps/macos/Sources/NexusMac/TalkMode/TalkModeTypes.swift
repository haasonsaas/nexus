import Foundation

// MARK: - Talk Mode Phase

enum TalkModePhase: String, Sendable {
    case idle
    case listening
    case thinking
    case speaking
}

// MARK: - Stop Reason

enum TalkStopReason: Sendable {
    case userTap
    case speech
    case manual
}

// MARK: - Session Role

enum SessionRole: String, Sendable {
    case user
    case assistant
    case system
}

// MARK: - Activity Kind

enum ActivityKind: String, Sendable {
    case chat
    case tool
    case thinking
}

// MARK: - Tool Kind

enum ToolKind: String, Sendable {
    case bash
    case computer
    case file
    case mcp
    case unknown
}

// MARK: - Playback Result

struct TalkPlaybackResult: Sendable {
    let finished: Bool
    let interruptedAt: Double?
}

// MARK: - TTS Request

struct ElevenLabsTTSRequest: Sendable {
    let text: String
    let modelId: String?
    let outputFormat: String?
    let speed: Double?
    let stability: Double?
    let similarity: Double?
    let style: Double?
    let speakerBoost: Bool?
    let seed: UInt32?
    let normalize: String?
    let language: String?
    let latencyTier: Int?
}

// MARK: - TTS Voice

struct ElevenLabsVoice: Decodable, Sendable {
    let voiceId: String
    let name: String?

    enum CodingKeys: String, CodingKey {
        case voiceId = "voice_id"
        case name
    }
}

// MARK: - TTS Voices Response

struct ElevenLabsVoicesResponse: Decodable, Sendable {
    let voices: [ElevenLabsVoice]
}
