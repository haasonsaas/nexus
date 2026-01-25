import AppKit

/// Response from the approval alert
enum ExecApprovalAlertResponse { case approve, reject, alwaysAllow }

/// NSAlert-based prompter for command approvals
@MainActor
final class ExecApprovalAlert {
    static func show(approval: ExecApproval, completion: @escaping (ExecApprovalAlertResponse) -> Void) {
        let alert = NSAlert()
        alert.messageText = "Command Approval Required"
        alert.informativeText = "Command: \(approval.command)\n\nWorking Directory: \(approval.cwd.isEmpty ? "(not specified)" : approval.cwd)"
        alert.alertStyle = .warning
        alert.addButton(withTitle: "Approve")
        alert.addButton(withTitle: "Reject")
        alert.addButton(withTitle: "Always Allow")

        let tf = NSTextField(wrappingLabelWithString: approval.command)
        tf.font = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
        tf.isSelectable = true
        tf.frame = NSRect(x: 0, y: 0, width: 400, height: 60)
        alert.accessoryView = tf

        NSApp.activate(ignoringOtherApps: true)
        let response = alert.runModal()
        switch response {
        case .alertFirstButtonReturn: completion(.approve)
        case .alertThirdButtonReturn: completion(.alwaysAllow)
        default: completion(.reject)
        }
    }

    static func prompt(approval: ExecApproval, manager: ExecApprovalsManager) {
        show(approval: approval) { response in
            switch response {
            case .approve: manager.approve(id: approval.id)
            case .reject: manager.reject(id: approval.id)
            case .alwaysAllow: manager.alwaysAllow(id: approval.id)
            }
        }
    }
}
