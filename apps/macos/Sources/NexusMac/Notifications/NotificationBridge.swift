import Foundation
import OSLog
import UserNotifications

/// Bridges system notifications for AI agent awareness.
/// Captures and processes notifications for context.
@MainActor
@Observable
final class NotificationBridge: NSObject {
    static let shared = NotificationBridge()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "notification.bridge")

    private(set) var recentNotifications: [CapturedNotification] = []
    private(set) var isCapturing = false

    var onNotificationReceived: ((CapturedNotification) -> Void)?

    struct CapturedNotification: Identifiable, Codable {
        let id: String
        let appBundleId: String
        let appName: String
        let title: String
        let body: String?
        let timestamp: Date
        let category: String?
        var isRead: Bool
    }

    override init() {
        super.init()
        registerForNotifications()
    }

    // MARK: - Notification Capture

    /// Start capturing notifications
    func startCapturing() {
        isCapturing = true
        logger.info("notification capture started")
    }

    /// Stop capturing notifications
    func stopCapturing() {
        isCapturing = false
        logger.info("notification capture stopped")
    }

    /// Clear captured notifications
    func clearNotifications() {
        recentNotifications.removeAll()
    }

    /// Mark notification as read
    func markAsRead(id: String) {
        if let index = recentNotifications.firstIndex(where: { $0.id == id }) {
            recentNotifications[index].isRead = true
        }
    }

    /// Get unread notifications
    func unreadNotifications() -> [CapturedNotification] {
        recentNotifications.filter { !$0.isRead }
    }

    /// Get notifications from specific app
    func notifications(from bundleId: String) -> [CapturedNotification] {
        recentNotifications.filter { $0.appBundleId == bundleId }
    }

    // MARK: - Outgoing Notifications

    /// Send a notification from nexus
    func send(title: String, body: String?, category: String? = nil) async throws {
        let center = UNUserNotificationCenter.current()

        // Check permission
        let settings = await center.notificationSettings()
        guard settings.authorizationStatus == .authorized else {
            throw NotificationBridgeError.notAuthorized
        }

        let content = UNMutableNotificationContent()
        content.title = title
        if let body {
            content.body = body
        }
        if let category {
            content.categoryIdentifier = category
        }
        content.sound = .default

        let request = UNNotificationRequest(
            identifier: UUID().uuidString,
            content: content,
            trigger: nil
        )

        try await center.add(request)
        logger.debug("notification sent title=\(title)")
    }

    /// Request notification permission
    func requestPermission() async throws -> Bool {
        let center = UNUserNotificationCenter.current()
        let granted = try await center.requestAuthorization(options: [.alert, .sound, .badge])
        logger.info("notification permission granted=\(granted)")
        return granted
    }

    // MARK: - Private

    private func registerForNotifications() {
        // Register for distributed notifications to capture system-wide notifications
        DistributedNotificationCenter.default().addObserver(
            self,
            selector: #selector(handleDistributedNotification(_:)),
            name: nil,
            object: nil
        )

        // Set ourselves as UNUserNotificationCenter delegate
        UNUserNotificationCenter.current().delegate = self
    }

    @objc private func handleDistributedNotification(_ notification: Notification) {
        guard isCapturing else { return }

        // Filter for relevant notification types
        let name = notification.name.rawValue
        guard name.contains("notification") || name.contains("alert") else { return }

        let captured = CapturedNotification(
            id: UUID().uuidString,
            appBundleId: notification.object as? String ?? "unknown",
            appName: notification.object as? String ?? "Unknown",
            title: name,
            body: notification.userInfo?.description,
            timestamp: Date(),
            category: nil,
            isRead: false
        )

        Task { @MainActor in
            addNotification(captured)
        }
    }

    private func addNotification(_ notification: CapturedNotification) {
        // Keep last 100 notifications
        if recentNotifications.count >= 100 {
            recentNotifications.removeFirst()
        }
        recentNotifications.append(notification)
        onNotificationReceived?(notification)
        logger.debug("notification captured app=\(notification.appBundleId)")
    }
}

// MARK: - UNUserNotificationCenterDelegate

extension NotificationBridge: UNUserNotificationCenterDelegate {
    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification
    ) async -> UNNotificationPresentationOptions {
        // Allow notifications to show even when app is in foreground
        return [.banner, .sound]
    }

    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse
    ) async {
        let content = response.notification.request.content

        let captured = CapturedNotification(
            id: response.notification.request.identifier,
            appBundleId: "com.nexus.mac",
            appName: "Nexus",
            title: content.title,
            body: content.body,
            timestamp: response.notification.date,
            category: content.categoryIdentifier.isEmpty ? nil : content.categoryIdentifier,
            isRead: false
        )

        await MainActor.run {
            addNotification(captured)
        }
    }
}

enum NotificationBridgeError: LocalizedError {
    case notAuthorized
    case sendFailed(String)

    var errorDescription: String? {
        switch self {
        case .notAuthorized:
            return "Notification permission not granted"
        case .sendFailed(let reason):
            return "Failed to send notification: \(reason)"
        }
    }
}
