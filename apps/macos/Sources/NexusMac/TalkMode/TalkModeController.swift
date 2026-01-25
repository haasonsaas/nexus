import Foundation
import Observation
import OSLog

@MainActor
@Observable
final class TalkModeController {
    static let shared = TalkModeController()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "talk.controller")

    private(set) var phase: TalkModePhase = .idle
    private(set) var isPaused: Bool = false
    private(set) var isEnabled: Bool = false
    private(set) var audioLevel: Double = 0

    private init() {}

    // MARK: - Enable/Disable

    func setEnabled(_ enabled: Bool) async {
        guard enabled != isEnabled else { return }
        logger.info("talk enabled=\(enabled)")
        isEnabled = enabled

        if enabled {
            TalkOverlayController.shared.present()
        } else {
            TalkOverlayController.shared.dismiss()
        }

        await TalkModeRuntime.shared.setEnabled(enabled)
    }

    // MARK: - Phase Updates

    func updatePhase(_ newPhase: TalkModePhase) {
        guard phase != newPhase else { return }
        phase = newPhase
        TalkOverlayController.shared.updatePhase(newPhase)

        let effectivePhase = isPaused ? "paused" : newPhase.rawValue
        Task {
            await notifyGateway(phase: effectivePhase)
        }
    }

    // MARK: - Audio Level

    func updateLevel(_ level: Double) {
        audioLevel = max(0, min(1, level))
        TalkOverlayController.shared.updateLevel(audioLevel)
    }

    // MARK: - Pause/Resume

    func setPaused(_ paused: Bool) {
        guard isPaused != paused else { return }
        logger.info("talk paused=\(paused)")
        isPaused = paused
        TalkOverlayController.shared.updatePaused(paused)

        let effectivePhase = paused ? "paused" : phase.rawValue
        Task {
            await notifyGateway(phase: effectivePhase)
        }
        Task {
            await TalkModeRuntime.shared.setPaused(paused)
        }
    }

    func togglePaused() {
        setPaused(!isPaused)
    }

    // MARK: - Stop Speaking

    func stopSpeaking(reason: TalkStopReason = .userTap) {
        Task {
            await TalkModeRuntime.shared.stopSpeaking(reason: reason)
        }
    }

    // MARK: - Exit Talk Mode

    func exitTalkMode() {
        Task {
            await setEnabled(false)
        }
    }

    // MARK: - Gateway Notification

    private func notifyGateway(phase: String) async {
        do {
            let params: [String: AnyCodable] = [
                "enabled": AnyCodable(isEnabled),
                "phase": AnyCodable(phase)
            ]
            _ = try await GatewayConnection.shared.request(
                method: "talk.mode",
                params: params,
                timeoutMs: 5000
            )
        } catch {
            logger.warning("talk.mode notification failed: \(error.localizedDescription)")
        }
    }
}
