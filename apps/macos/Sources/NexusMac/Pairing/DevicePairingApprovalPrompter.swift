import AppKit
import Foundation
import OSLog

// MARK: - Device Pairing Request

/// Represents a device pairing request with verification details
struct DevicePairingRequest: Identifiable, Sendable {
    let id: String
    let deviceName: String
    let ipAddress: String
    let verificationCode: String
    let timestamp: Date
    let timeoutSeconds: Int

    var isExpired: Bool {
        Date().timeIntervalSince(timestamp) > Double(timeoutSeconds)
    }
}

/// Decision response for a device pairing request
enum DevicePairingDecision: String, Codable, Sendable {
    case approve
    case deny
}

// MARK: - DevicePairingApprovalPrompter

/// Singleton service for handling device pairing approval prompts.
/// Shows NSAlert dialogs when new devices request pairing and communicates
/// the approval decision back to the gateway via ControlChannel.
@MainActor
@Observable
final class DevicePairingApprovalPrompter {
    static let shared = DevicePairingApprovalPrompter()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "device-pairing")

    private(set) var pendingRequests: [DevicePairingRequest] = []
    private(set) var isProcessing = false

    private init() {}

    // MARK: - Public API

    /// Handle an incoming device pairing request
    /// - Parameter request: The pairing request to process
    func handlePairingRequest(_ request: DevicePairingRequest) async {
        logger.info("Received device pairing request from \(request.deviceName, privacy: .public) at \(request.ipAddress, privacy: .public)")

        pendingRequests.append(request)
        defer {
            pendingRequests.removeAll { $0.id == request.id }
        }

        let decision = await promptForApproval(request)
        await sendDecision(decision, for: request)
    }

    /// Process a raw pairing request payload from the gateway
    /// - Parameter payload: JSON data containing the pairing request
    func processPairingPayload(_ payload: Data) async {
        do {
            let request = try JSONDecoder().decode(PairingPayload.self, from: payload)
            let pairingRequest = DevicePairingRequest(
                id: request.requestId,
                deviceName: request.deviceName,
                ipAddress: request.ipAddress,
                verificationCode: request.verificationCode,
                timestamp: Date(),
                timeoutSeconds: request.timeoutSeconds ?? 120
            )
            await handlePairingRequest(pairingRequest)
        } catch {
            logger.error("Failed to decode pairing payload: \(error.localizedDescription, privacy: .public)")
        }
    }

    // MARK: - Private Implementation

    private func promptForApproval(_ request: DevicePairingRequest) async -> DevicePairingDecision {
        isProcessing = true
        defer { isProcessing = false }

        NSApp.activate(ignoringOtherApps: true)

        let alert = NSAlert()
        alert.alertStyle = .warning
        alert.messageText = "Device Pairing Request"
        alert.informativeText = "A new device is requesting to pair with this machine. Verify the details below before approving."
        alert.accessoryView = buildAccessoryView(request)

        alert.addButton(withTitle: "Approve")
        alert.addButton(withTitle: "Deny")

        if alert.buttons.indices.contains(1) {
            alert.buttons[1].hasDestructiveAction = true
        }

        logger.info("Showing pairing approval dialog for device: \(request.deviceName, privacy: .public)")

        let response = alert.runModal()
        let decision: DevicePairingDecision = response == .alertFirstButtonReturn ? .approve : .deny

        logger.info("User decision for device \(request.deviceName, privacy: .public): \(decision.rawValue, privacy: .public)")

        return decision
    }

    private func buildAccessoryView(_ request: DevicePairingRequest) -> NSView {
        let stack = NSStackView()
        stack.orientation = .vertical
        stack.spacing = 12
        stack.alignment = .leading

        // Device information section
        let infoTitle = NSTextField(labelWithString: "Device Information")
        infoTitle.font = NSFont.boldSystemFont(ofSize: NSFont.systemFontSize)
        stack.addArrangedSubview(infoTitle)

        let infoStack = NSStackView()
        infoStack.orientation = .vertical
        infoStack.spacing = 4
        infoStack.alignment = .leading

        addDetailRow(title: "Device Name", value: request.deviceName, to: infoStack)
        addDetailRow(title: "IP Address", value: request.ipAddress, to: infoStack)

        stack.addArrangedSubview(infoStack)

        // Verification code section
        let verificationTitle = NSTextField(labelWithString: "Verification Code")
        verificationTitle.font = NSFont.boldSystemFont(ofSize: NSFont.systemFontSize)
        stack.addArrangedSubview(verificationTitle)

        let codeLabel = NSTextField(labelWithString: request.verificationCode)
        codeLabel.font = NSFont.monospacedSystemFont(ofSize: 24, weight: .bold)
        codeLabel.textColor = NSColor.systemBlue
        codeLabel.alignment = .center
        codeLabel.isSelectable = true

        let codeContainer = NSView()
        codeContainer.translatesAutoresizingMaskIntoConstraints = false
        codeLabel.translatesAutoresizingMaskIntoConstraints = false
        codeContainer.addSubview(codeLabel)

        NSLayoutConstraint.activate([
            codeLabel.centerXAnchor.constraint(equalTo: codeContainer.centerXAnchor),
            codeLabel.topAnchor.constraint(equalTo: codeContainer.topAnchor, constant: 8),
            codeLabel.bottomAnchor.constraint(equalTo: codeContainer.bottomAnchor, constant: -8),
            codeContainer.widthAnchor.constraint(equalToConstant: 300),
        ])

        stack.addArrangedSubview(codeContainer)

        // Warning footer
        let footer = NSTextField(labelWithString: "Only approve if you initiated this pairing request and the verification code matches.")
        footer.textColor = NSColor.secondaryLabelColor
        footer.font = NSFont.systemFont(ofSize: NSFont.smallSystemFontSize)
        footer.preferredMaxLayoutWidth = 300
        footer.lineBreakMode = .byWordWrapping
        stack.addArrangedSubview(footer)

        return stack
    }

    private func addDetailRow(title: String, value: String, to stack: NSStackView) {
        let row = NSStackView()
        row.orientation = .horizontal
        row.spacing = 6
        row.alignment = .firstBaseline

        let titleLabel = NSTextField(labelWithString: "\(title):")
        titleLabel.font = NSFont.systemFont(ofSize: NSFont.smallSystemFontSize, weight: .semibold)
        titleLabel.textColor = NSColor.secondaryLabelColor

        let valueLabel = NSTextField(labelWithString: value)
        valueLabel.font = NSFont.systemFont(ofSize: NSFont.smallSystemFontSize)
        valueLabel.isSelectable = true
        valueLabel.lineBreakMode = .byTruncatingMiddle
        valueLabel.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)

        row.addArrangedSubview(titleLabel)
        row.addArrangedSubview(valueLabel)
        stack.addArrangedSubview(row)
    }

    private func sendDecision(_ decision: DevicePairingDecision, for request: DevicePairingRequest) async {
        do {
            let params: [String: AnyHashable] = [
                "requestId": request.requestId,
                "decision": decision.rawValue,
                "deviceName": request.deviceName,
            ]
            _ = try await ControlChannel.shared.request(method: "pairing.device.respond", params: params)
            logger.info("Sent pairing decision '\(decision.rawValue, privacy: .public)' for request \(request.id, privacy: .public)")
        } catch {
            logger.error("Failed to send pairing decision: \(error.localizedDescription, privacy: .public)")
        }
    }
}

// MARK: - Internal Types

private extension DevicePairingRequest {
    var requestId: String { id }
}

private struct PairingPayload: Codable {
    let requestId: String
    let deviceName: String
    let ipAddress: String
    let verificationCode: String
    let timeoutSeconds: Int?
}
