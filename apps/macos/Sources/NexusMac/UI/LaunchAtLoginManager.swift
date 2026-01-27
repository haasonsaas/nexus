import OSLog
import ServiceManagement

@MainActor
final class LaunchAtLoginManager {
    static let shared = LaunchAtLoginManager()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "launch-at-login")

    private init() {}

    func applyPreference(_ enabled: Bool) {
        do {
            try setEnabled(enabled)
        } catch {
            logger.error("launch at login update failed: \(error.localizedDescription, privacy: .public)")
        }
    }

    func isEnabled() -> Bool {
        guard #available(macOS 13.0, *) else { return false }
        return SMAppService.mainApp.status == .enabled
    }

    private func setEnabled(_ enabled: Bool) throws {
        guard #available(macOS 13.0, *) else {
            logger.warning("launch at login requires macOS 13 or later")
            return
        }

        let service = SMAppService.mainApp

        switch (enabled, service.status) {
        case (true, .enabled):
            return
        case (false, .notRegistered), (false, .notFound):
            return
        case (true, _):
            try service.register()
        case (false, _):
            try service.unregister()
        }

        logger.info("launch at login set to \(enabled, privacy: .public)")
    }
}
