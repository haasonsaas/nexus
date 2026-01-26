import Foundation
import OSLog

/// Advertises node capabilities during gateway handshake
struct NodeCapabilitiesPayload: Codable, Sendable {
    let commands: [String]
    let features: [String]
    let permissions: NodePermissions
    let deviceInfo: NodeDeviceInfo
}

struct NodePermissions: Codable, Sendable {
    let screenRecording: Bool
    let camera: Bool
    let microphone: Bool
    let accessibility: Bool
    let location: Bool
}

struct NodeDeviceInfo: Codable, Sendable {
    let platform: String
    let osVersion: String
    let deviceModel: String
    let appVersion: String
    let architecture: String
    let hostname: String
}

// MARK: - NodeCommandDispatcher Extension

extension NodeCommandDispatcher {
    /// Build the capabilities payload for gateway handshake
    func buildCapabilitiesPayload() -> NodeCapabilitiesPayload {
        let pm = PermissionManager.shared

        return NodeCapabilitiesPayload(
            commands: currentCapabilities(),
            features: buildFeatureList(),
            permissions: NodePermissions(
                screenRecording: pm.status(for: .screenRecording),
                camera: pm.status(for: .camera),
                microphone: pm.status(for: .microphone),
                accessibility: pm.status(for: .accessibility),
                location: AppStateStore.shared.nodeModeEnabled
            ),
            deviceInfo: NodeDeviceInfo(
                platform: "macOS",
                osVersion: ProcessInfo.processInfo.operatingSystemVersionString,
                deviceModel: getMacModel(),
                appVersion: Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "0.0.0",
                architecture: getArchitecture(),
                hostname: Host.current().localizedName ?? "Unknown"
            )
        )
    }

    /// Build the feature list based on app state
    private func buildFeatureList() -> [String] {
        var features: [String] = []

        if AppStateStore.shared.canvasEnabled { features.append("canvas") }
        if AppStateStore.shared.cameraEnabled { features.append("camera") }
        if AppStateStore.shared.voiceWakeEnabled { features.append("voice_wake") }
        if AppStateStore.shared.nodeModeEnabled { features.append("node_mode") }
        if AppStateStore.shared.talkModeEnabled { features.append("talk_mode") }
        if AppStateStore.shared.heartbeatsEnabled { features.append("heartbeats") }

        return features
    }

    /// Get the Mac model identifier
    private func getMacModel() -> String {
        var size = 0
        sysctlbyname("hw.model", nil, &size, nil, 0)
        var model = [CChar](repeating: 0, count: size)
        sysctlbyname("hw.model", &model, &size, nil, 0)
        return String(cString: model)
    }

    /// Get the CPU architecture
    private func getArchitecture() -> String {
        #if arch(arm64)
        return "arm64"
        #elseif arch(x86_64)
        return "x86_64"
        #else
        return "unknown"
        #endif
    }

    /// Register this node with the gateway
    func registerWithGateway() async throws {
        let payload = buildCapabilitiesPayload()

        // Convert to dictionary for gateway request
        let encoder = JSONEncoder()
        let data = try encoder.encode(payload)
        guard let dict = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw NodeRegistrationError.encodingFailed
        }

        _ = try await ControlChannel.shared.requestAny(
            method: "node.register",
            params: dict
        )
    }
}

// MARK: - Registration Error

enum NodeRegistrationError: LocalizedError {
    case encodingFailed
    case registrationFailed(String)

    var errorDescription: String? {
        switch self {
        case .encodingFailed:
            return "Failed to encode capabilities payload"
        case .registrationFailed(let reason):
            return "Node registration failed: \(reason)"
        }
    }
}

// MARK: - Capability Update Notifications

extension Notification.Name {
    static let nodeCapabilitiesUpdated = Notification.Name("nexus.node.capabilities.updated")
}

extension NodeCommandDispatcher {
    /// Refresh capabilities and notify observers
    func refreshCapabilities() {
        updateCapabilities()
        NotificationCenter.default.post(name: .nodeCapabilitiesUpdated, object: self)
    }

    /// Check if a specific capability is available
    func hasCapability(_ command: NodeCommand) -> Bool {
        capabilities.contains(command)
    }

    /// Get a human-readable description of current capabilities
    func capabilitiesDescription() -> String {
        let enabled = capabilities.map { $0.rawValue }
        let disabled = NodeCommand.allCases
            .filter { !capabilities.contains($0) }
            .map { $0.rawValue }

        var description = "Enabled commands: \(enabled.joined(separator: ", "))"
        if !disabled.isEmpty {
            description += "\nDisabled commands: \(disabled.joined(separator: ", "))"
        }
        return description
    }
}

// MARK: - Capability Requirements

/// Describes what permissions are required for a command
struct NodeCommandRequirement {
    let command: NodeCommand
    let permissions: [PermissionType]
    let features: [String]

    static let requirements: [NodeCommandRequirement] = [
        NodeCommandRequirement(command: .screenCapture, permissions: [.screenRecording], features: []),
        NodeCommandRequirement(command: .cameraCapture, permissions: [.camera], features: ["camera"]),
        NodeCommandRequirement(command: .canvasOpen, permissions: [], features: ["canvas"]),
        NodeCommandRequirement(command: .canvasClose, permissions: [], features: ["canvas"]),
        NodeCommandRequirement(command: .canvasEval, permissions: [], features: ["canvas"]),
        NodeCommandRequirement(command: .canvasSnapshot, permissions: [], features: ["canvas"]),
        NodeCommandRequirement(command: .systemRun, permissions: [], features: []),
        NodeCommandRequirement(command: .clipboardRead, permissions: [], features: []),
        NodeCommandRequirement(command: .clipboardWrite, permissions: [], features: []),
        NodeCommandRequirement(command: .fileRead, permissions: [], features: []),
        NodeCommandRequirement(command: .fileWrite, permissions: [], features: []),
        NodeCommandRequirement(command: .fileList, permissions: [], features: []),
        NodeCommandRequirement(command: .notify, permissions: [], features: []),
        NodeCommandRequirement(command: .locationGet, permissions: [], features: ["node_mode"]),
    ]

    static func requirement(for command: NodeCommand) -> NodeCommandRequirement? {
        requirements.first { $0.command == command }
    }

    /// Check if all requirements are met for a command
    @MainActor
    func isSatisfied() -> Bool {
        let pm = PermissionManager.shared

        // Check permissions
        for permission in permissions {
            if !pm.status(for: permission) {
                return false
            }
        }

        // Check features
        let state = AppStateStore.shared
        for feature in features {
            switch feature {
            case "canvas": if !state.canvasEnabled { return false }
            case "camera": if !state.cameraEnabled { return false }
            case "voice_wake": if !state.voiceWakeEnabled { return false }
            case "node_mode": if !state.nodeModeEnabled { return false }
            default: break
            }
        }

        return true
    }
}
