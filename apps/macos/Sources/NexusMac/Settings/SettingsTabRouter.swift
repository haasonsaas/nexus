import Foundation

// MARK: - SettingsTab

enum SettingsTab: String, CaseIterable, Identifiable, Hashable {
    case general
    case gateway
    case channels
    case voice
    case permissions
    case advanced
    case about

    var id: String { rawValue }

    var title: String {
        switch self {
        case .general: return "General"
        case .gateway: return "Gateway"
        case .channels: return "Channels"
        case .voice: return "Voice"
        case .permissions: return "Permissions"
        case .advanced: return "Advanced"
        case .about: return "About"
        }
    }

    var icon: String {
        switch self {
        case .general: return "gear"
        case .gateway: return "server.rack"
        case .channels: return "bubble.left.and.bubble.right"
        case .voice: return "mic"
        case .permissions: return "lock.shield"
        case .advanced: return "wrench.and.screwdriver"
        case .about: return "info.circle"
        }
    }
}

// MARK: - SettingsTabRouter

@MainActor
final class SettingsTabRouter {
    private static var pendingTab: SettingsTab?

    static func request(_ tab: SettingsTab) {
        pendingTab = tab
        NotificationCenter.default.post(name: .nexusSelectSettingsTab, object: tab)
    }

    static func consume() -> SettingsTab? {
        defer { pendingTab = nil }
        return pendingTab
    }
}

// MARK: - Notification.Name Extension

extension Notification.Name {
    static let nexusSelectSettingsTab = Notification.Name("nexus.selectSettingsTab")
}
