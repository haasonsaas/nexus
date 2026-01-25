import AppKit
import Foundation
import OSLog

// MARK: - Node Pairing Request

/// Represents a pairing request from a remote node
struct NodePairingRequest: Identifiable, Sendable {
    let id: String
    let nodeIdentifier: String
    let nodeName: String?
    let nodeType: String?
    let requestDetails: String?
    let capabilities: [String]?
    let timestamp: Date
    let timeoutSeconds: Int

    var isExpired: Bool {
        Date().timeIntervalSince(timestamp) > Double(timeoutSeconds)
    }

    var displayName: String {
        nodeName ?? nodeIdentifier
    }
}

/// Decision response for a node pairing request
struct NodePairingDecision: Sendable {
    let approved: Bool
    let rememberNode: Bool

    static let approve = NodePairingDecision(approved: true, rememberNode: false)
    static let approveAndRemember = NodePairingDecision(approved: true, rememberNode: true)
    static let deny = NodePairingDecision(approved: false, rememberNode: false)
}

// MARK: - NodePairingApprovalPrompter

/// Singleton service for handling remote node pairing approval prompts.
/// Shows NSAlert dialogs when remote nodes request pairing with optional
/// "remember this node" functionality backed by UserDefaults.
@MainActor
@Observable
final class NodePairingApprovalPrompter {
    static let shared = NodePairingApprovalPrompter()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "node-pairing")
    private let approvedNodesKey = "NodePairing_ApprovedNodes"

    private(set) var pendingRequests: [NodePairingRequest] = []
    private(set) var isProcessing = false

    /// Set of node identifiers that have been approved and remembered
    var approvedNodes: Set<String> {
        get {
            Set(UserDefaults.standard.stringArray(forKey: approvedNodesKey) ?? [])
        }
        set {
            UserDefaults.standard.set(Array(newValue), forKey: approvedNodesKey)
        }
    }

    private init() {}

    // MARK: - Public API

    /// Handle an incoming node pairing request
    /// - Parameter request: The pairing request to process
    func handlePairingRequest(_ request: NodePairingRequest) async {
        logger.info("Received node pairing request from \(request.nodeIdentifier, privacy: .public)")

        // Check if this node is already approved
        if approvedNodes.contains(request.nodeIdentifier) {
            logger.info("Node \(request.nodeIdentifier, privacy: .public) is already approved, auto-approving")
            await sendDecision(.approve, for: request)
            return
        }

        pendingRequests.append(request)
        defer {
            pendingRequests.removeAll { $0.id == request.id }
        }

        let decision = await promptForApproval(request)
        await sendDecision(decision, for: request)
    }

    /// Process a raw node pairing request payload from the gateway
    /// - Parameter payload: JSON data containing the pairing request
    func processPairingPayload(_ payload: Data) async {
        do {
            let request = try JSONDecoder().decode(NodePairingPayload.self, from: payload)
            let pairingRequest = NodePairingRequest(
                id: request.requestId,
                nodeIdentifier: request.nodeIdentifier,
                nodeName: request.nodeName,
                nodeType: request.nodeType,
                requestDetails: request.requestDetails,
                capabilities: request.capabilities,
                timestamp: Date(),
                timeoutSeconds: request.timeoutSeconds ?? 120
            )
            await handlePairingRequest(pairingRequest)
        } catch {
            logger.error("Failed to decode node pairing payload: \(error.localizedDescription, privacy: .public)")
        }
    }

    /// Remove a node from the approved list
    /// - Parameter nodeIdentifier: The identifier of the node to remove
    func revokeApproval(for nodeIdentifier: String) {
        var nodes = approvedNodes
        nodes.remove(nodeIdentifier)
        approvedNodes = nodes
        logger.info("Revoked approval for node: \(nodeIdentifier, privacy: .public)")
    }

    /// Clear all approved nodes
    func clearAllApprovedNodes() {
        approvedNodes = []
        logger.info("Cleared all approved nodes")
    }

    // MARK: - Private Implementation

    private func promptForApproval(_ request: NodePairingRequest) async -> NodePairingDecision {
        isProcessing = true
        defer { isProcessing = false }

        NSApp.activate(ignoringOtherApps: true)

        let alert = NSAlert()
        alert.alertStyle = .warning
        alert.messageText = "Node Pairing Request"
        alert.informativeText = "A remote node is requesting to pair with this machine."

        // Create accessory view with checkbox
        let (accessoryView, rememberCheckbox) = buildAccessoryView(request)
        alert.accessoryView = accessoryView

        alert.addButton(withTitle: "Approve")
        alert.addButton(withTitle: "Deny")

        if alert.buttons.indices.contains(1) {
            alert.buttons[1].hasDestructiveAction = true
        }

        logger.info("Showing node pairing approval dialog for: \(request.displayName, privacy: .public)")

        let response = alert.runModal()

        if response == .alertFirstButtonReturn {
            let remember = rememberCheckbox.state == .on
            if remember {
                var nodes = approvedNodes
                nodes.insert(request.nodeIdentifier)
                approvedNodes = nodes
                logger.info("Approved and remembered node: \(request.nodeIdentifier, privacy: .public)")
            } else {
                logger.info("Approved node (one-time): \(request.nodeIdentifier, privacy: .public)")
            }
            return remember ? .approveAndRemember : .approve
        } else {
            logger.info("Denied node pairing request from: \(request.nodeIdentifier, privacy: .public)")
            return .deny
        }
    }

    private func buildAccessoryView(_ request: NodePairingRequest) -> (NSView, NSButton) {
        let stack = NSStackView()
        stack.orientation = .vertical
        stack.spacing = 12
        stack.alignment = .leading

        // Node information section
        let infoTitle = NSTextField(labelWithString: "Node Information")
        infoTitle.font = NSFont.boldSystemFont(ofSize: NSFont.systemFontSize)
        stack.addArrangedSubview(infoTitle)

        let infoStack = NSStackView()
        infoStack.orientation = .vertical
        infoStack.spacing = 4
        infoStack.alignment = .leading

        addDetailRow(title: "Node ID", value: request.nodeIdentifier, to: infoStack)

        if let nodeName = request.nodeName, !nodeName.isEmpty {
            addDetailRow(title: "Name", value: nodeName, to: infoStack)
        }

        if let nodeType = request.nodeType, !nodeType.isEmpty {
            addDetailRow(title: "Type", value: nodeType, to: infoStack)
        }

        stack.addArrangedSubview(infoStack)

        // Request details section (if provided)
        if let details = request.requestDetails, !details.isEmpty {
            let detailsTitle = NSTextField(labelWithString: "Request Details")
            detailsTitle.font = NSFont.boldSystemFont(ofSize: NSFont.systemFontSize)
            stack.addArrangedSubview(detailsTitle)

            let detailsText = NSTextView()
            detailsText.isEditable = false
            detailsText.isSelectable = true
            detailsText.drawsBackground = true
            detailsText.backgroundColor = NSColor.textBackgroundColor
            detailsText.font = NSFont.systemFont(ofSize: NSFont.smallSystemFontSize)
            detailsText.string = details
            detailsText.textContainerInset = NSSize(width: 6, height: 6)
            detailsText.textContainer?.lineFragmentPadding = 0
            detailsText.textContainer?.widthTracksTextView = true
            detailsText.isHorizontallyResizable = false
            detailsText.isVerticallyResizable = false

            let detailsScroll = NSScrollView()
            detailsScroll.borderType = .lineBorder
            detailsScroll.hasVerticalScroller = false
            detailsScroll.hasHorizontalScroller = false
            detailsScroll.documentView = detailsText
            detailsScroll.translatesAutoresizingMaskIntoConstraints = false
            detailsScroll.widthAnchor.constraint(lessThanOrEqualToConstant: 400).isActive = true
            detailsScroll.heightAnchor.constraint(greaterThanOrEqualToConstant: 40).isActive = true
            stack.addArrangedSubview(detailsScroll)
        }

        // Capabilities section (if provided)
        if let capabilities = request.capabilities, !capabilities.isEmpty {
            let capTitle = NSTextField(labelWithString: "Requested Capabilities")
            capTitle.font = NSFont.boldSystemFont(ofSize: NSFont.systemFontSize)
            stack.addArrangedSubview(capTitle)

            let capText = capabilities.joined(separator: ", ")
            let capLabel = NSTextField(labelWithString: capText)
            capLabel.font = NSFont.systemFont(ofSize: NSFont.smallSystemFontSize)
            capLabel.textColor = NSColor.secondaryLabelColor
            capLabel.preferredMaxLayoutWidth = 380
            capLabel.lineBreakMode = .byWordWrapping
            stack.addArrangedSubview(capLabel)
        }

        // Remember checkbox
        let rememberCheckbox = NSButton(checkboxWithTitle: "Remember this node (auto-approve future requests)", target: nil, action: nil)
        rememberCheckbox.state = .off
        stack.addArrangedSubview(rememberCheckbox)

        // Warning footer
        let footer = NSTextField(labelWithString: "Only approve nodes you trust. Approved nodes can communicate with your machine.")
        footer.textColor = NSColor.secondaryLabelColor
        footer.font = NSFont.systemFont(ofSize: NSFont.smallSystemFontSize)
        footer.preferredMaxLayoutWidth = 380
        footer.lineBreakMode = .byWordWrapping
        stack.addArrangedSubview(footer)

        return (stack, rememberCheckbox)
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
        valueLabel.font = NSFont.monospacedSystemFont(ofSize: NSFont.smallSystemFontSize, weight: .regular)
        valueLabel.isSelectable = true
        valueLabel.lineBreakMode = .byTruncatingMiddle
        valueLabel.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)

        row.addArrangedSubview(titleLabel)
        row.addArrangedSubview(valueLabel)
        stack.addArrangedSubview(row)
    }

    private func sendDecision(_ decision: NodePairingDecision, for request: NodePairingRequest) async {
        do {
            let params: [String: AnyHashable] = [
                "requestId": request.id,
                "approved": decision.approved,
                "remembered": decision.rememberNode,
                "nodeIdentifier": request.nodeIdentifier,
            ]
            _ = try await ControlChannel.shared.request(method: "pairing.node.respond", params: params)
            logger.info("Sent node pairing decision (approved: \(decision.approved), remembered: \(decision.rememberNode)) for request \(request.id, privacy: .public)")
        } catch {
            logger.error("Failed to send node pairing decision: \(error.localizedDescription, privacy: .public)")
        }
    }
}

// MARK: - Internal Types

private struct NodePairingPayload: Codable {
    let requestId: String
    let nodeIdentifier: String
    let nodeName: String?
    let nodeType: String?
    let requestDetails: String?
    let capabilities: [String]?
    let timeoutSeconds: Int?
}
