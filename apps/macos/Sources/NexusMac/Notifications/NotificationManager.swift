import Foundation
import UserNotifications
import OSLog
import Security

enum NotificationPriority {
    case passive
    case active
    case timeSensitive
}

@MainActor
struct NotificationManager {
    private let logger = Logger(subsystem: "com.nexus.mac", category: "notifications")

    private static let hasTimeSensitiveEntitlement: Bool = {
        guard let task = SecTaskCreateFromSelf(nil) else { return false }
        let key = "com.apple.developer.usernotifications.time-sensitive" as CFString
        guard let val = SecTaskCopyValueForEntitlement(task, key, nil) else { return false }
        return (val as? Bool) == true
    }()

    func send(title: String, body: String, sound: String? = nil, priority: NotificationPriority? = nil) async -> Bool {
        let center = UNUserNotificationCenter.current()
        let status = await center.notificationSettings()

        if status.authorizationStatus == .notDetermined {
            let granted = try? await center.requestAuthorization(options: [.alert, .sound, .badge])
            if granted != true {
                logger.warning("notification permission denied (request)")
                return false
            }
        } else if status.authorizationStatus != .authorized {
            logger.warning("notification permission denied")
            return false
        }

        let content = UNMutableNotificationContent()
        content.title = title
        content.body = body
        if let soundName = sound, !soundName.isEmpty {
            content.sound = UNNotificationSound(named: UNNotificationSoundName(soundName))
        }

        // Set interruption level based on priority
        if let priority {
            switch priority {
            case .passive: content.interruptionLevel = .passive
            case .active: content.interruptionLevel = .active
            case .timeSensitive:
                content.interruptionLevel = Self.hasTimeSensitiveEntitlement ? .timeSensitive : .active
            }
        }

        let req = UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil)
        do {
            try await center.add(req)
            return true
        } catch {
            logger.error("notification send failed: \(error.localizedDescription)")
            return false
        }
    }
}
