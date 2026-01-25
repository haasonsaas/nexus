import AppKit
import Foundation
import OSLog

/// Chime types for voice wake audio feedback.
enum VoiceWakeChime: Codable, Equatable, Sendable {
    case none
    case subtle
    case standard

    var displayLabel: String {
        switch self {
        case .none: "No Sound"
        case .subtle: "Subtle"
        case .standard: "Standard"
        }
    }

    var soundName: String? {
        switch self {
        case .none: nil
        case .subtle: "Tink"
        case .standard: "Glass"
        }
    }
}

/// Player for voice wake chimes.
@MainActor
enum VoiceWakeChimePlayer {
    private static let logger = Logger(subsystem: "com.nexus.mac", category: "voicewake.chime")

    static func play(_ chime: VoiceWakeChime) {
        guard let soundName = chime.soundName else { return }
        guard let sound = NSSound(named: NSSound.Name(soundName)) else {
            logger.warning("Sound not found: \(soundName)")
            return
        }
        logger.debug("Playing chime: \(chime.displayLabel)")
        sound.play()
    }
}
