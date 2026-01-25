import UserNotifications
import AppKit

enum NotificationCategory: String, CaseIterable {
    case statusChange = "statusChange"
    case toolComplete = "toolComplete"
    case error = "error"
    case edgeStatus = "edgeStatus"
    case execApproval = "execApproval"

    var displayName: String {
        switch self {
        case .statusChange:
            return "Gateway Status Changes"
        case .toolComplete:
            return "Tool Execution Complete"
        case .error:
            return "Errors"
        case .edgeStatus:
            return "Edge Service Status"
        case .execApproval:
            return "Command Approvals"
        }
    }

    var description: String {
        switch self {
        case .statusChange:
            return "Notify when gateway connects or disconnects"
        case .toolComplete:
            return "Notify when tool invocations finish"
        case .error:
            return "Notify when errors occur during operations"
        case .edgeStatus:
            return "Notify when edge service starts or stops"
        case .execApproval:
            return "Notify when commands require approval"
        }
    }

    var userDefaultsKey: String {
        "notification_\(rawValue)_enabled"
    }
}

@MainActor
class NotificationService: NSObject, UNUserNotificationCenterDelegate {
    static let shared = NotificationService()

    private var permissionGranted = false
    private var hasRequestedPermission = false

    private override init() {
        super.init()
        UNUserNotificationCenter.current().delegate = self
    }

    // MARK: - Permission Management

    func requestPermission() {
        guard !hasRequestedPermission else { return }
        hasRequestedPermission = true

        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge]) { [weak self] granted, error in
            Task { @MainActor in
                self?.permissionGranted = granted
                if let error = error {
                    print("Notification permission error: \(error.localizedDescription)")
                }
            }
        }
    }

    func checkPermissionStatus() async -> Bool {
        let settings = await UNUserNotificationCenter.current().notificationSettings()
        return settings.authorizationStatus == .authorized
    }

    // MARK: - Preference Management

    func isEnabled(for category: NotificationCategory) -> Bool {
        // Default to true if never set
        if UserDefaults.standard.object(forKey: category.userDefaultsKey) == nil {
            return true
        }
        return UserDefaults.standard.bool(forKey: category.userDefaultsKey)
    }

    func setEnabled(_ enabled: Bool, for category: NotificationCategory) {
        UserDefaults.standard.set(enabled, forKey: category.userDefaultsKey)
    }

    // MARK: - Send Notifications

    func sendNotification(title: String, body: String, category: NotificationCategory) {
        guard isEnabled(for: category) else { return }

        let content = UNMutableNotificationContent()
        content.title = title
        content.body = body
        content.sound = .default
        content.categoryIdentifier = category.rawValue

        let request = UNNotificationRequest(
            identifier: UUID().uuidString,
            content: content,
            trigger: nil // Deliver immediately
        )

        UNUserNotificationCenter.current().add(request) { error in
            if let error = error {
                print("Failed to schedule notification: \(error.localizedDescription)")
            }
        }
    }

    // MARK: - Convenience Methods

    func notifyGatewayConnected() {
        sendNotification(
            title: "Gateway Connected",
            body: "Successfully connected to Nexus Gateway",
            category: .statusChange
        )
    }

    func notifyGatewayDisconnected(reason: String? = nil) {
        let body = reason ?? "Connection to Nexus Gateway lost"
        sendNotification(
            title: "Gateway Disconnected",
            body: body,
            category: .statusChange
        )
    }

    func notifyEdgeServiceStarted() {
        sendNotification(
            title: "Edge Service Started",
            body: "Nexus Edge service is now running",
            category: .edgeStatus
        )
    }

    func notifyEdgeServiceStopped() {
        sendNotification(
            title: "Edge Service Stopped",
            body: "Nexus Edge service has stopped",
            category: .edgeStatus
        )
    }

    func notifyEdgeServiceInstalled() {
        sendNotification(
            title: "Edge Service Installed",
            body: "Nexus Edge service has been installed and started",
            category: .edgeStatus
        )
    }

    func notifyEdgeServiceUninstalled() {
        sendNotification(
            title: "Edge Service Uninstalled",
            body: "Nexus Edge service has been removed",
            category: .edgeStatus
        )
    }

    func notifyToolCompleted(toolName: String, success: Bool, durationMs: Int64) {
        let durationText = durationMs > 1000 ? "\(durationMs / 1000)s" : "\(durationMs)ms"
        let title = success ? "Tool Completed" : "Tool Failed"
        let body = success
            ? "\(toolName) completed in \(durationText)"
            : "\(toolName) failed after \(durationText)"
        sendNotification(title: title, body: body, category: .toolComplete)
    }

    func notifyError(operation: String, message: String) {
        sendNotification(
            title: "Error: \(operation)",
            body: message,
            category: .error
        )
    }

    func notifyNodeOnline(nodeName: String) {
        sendNotification(
            title: "Node Online",
            body: "\(nodeName) is now connected",
            category: .statusChange
        )
    }

    func notifyNodeOffline(nodeName: String) {
        sendNotification(
            title: "Node Offline",
            body: "\(nodeName) has disconnected",
            category: .statusChange
        )
    }

    // MARK: - UNUserNotificationCenterDelegate

    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        // Show notifications even when app is in foreground
        completionHandler([.banner, .sound])
    }

    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        // Handle notification tap if needed
        completionHandler()
    }
}
