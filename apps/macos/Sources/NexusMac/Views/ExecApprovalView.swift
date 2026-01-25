import SwiftUI

/// SwiftUI view showing a pending command approval
struct ExecApprovalView: View {
    let approval: ExecApproval
    let onApprove: () -> Void
    let onReject: () -> Void
    let onAlwaysAllow: () -> Void
    @State private var remainingSeconds: Int = 0

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Image(systemName: "terminal").foregroundColor(.orange)
                Text("Command Approval").font(.headline)
                Spacer()
                Text("\(remainingSeconds)s").font(.caption).monospacedDigit()
                    .foregroundColor(remainingSeconds <= 5 ? .red : .secondary)
            }
            ScrollView(.horizontal, showsIndicators: false) {
                Text(approval.command).font(.system(.body, design: .monospaced)).textSelection(.enabled)
            }
            .padding(8).background(Color(NSColor.textBackgroundColor)).cornerRadius(6)
            .overlay(RoundedRectangle(cornerRadius: 6).stroke(Color.orange.opacity(0.3)))

            if !approval.cwd.isEmpty {
                HStack(spacing: 4) {
                    Image(systemName: "folder").font(.caption).foregroundColor(.secondary)
                    Text(approval.cwd).font(.caption).foregroundColor(.secondary).lineLimit(1).truncationMode(.middle)
                }
            }
            HStack(spacing: 12) {
                Button(action: onReject) { Label("Reject", systemImage: "xmark.circle") }
                    .keyboardShortcut(.escape, modifiers: [])
                Button(action: onAlwaysAllow) { Label("Always Allow", systemImage: "checkmark.shield") }
                Spacer()
                Button(action: onApprove) { Label("Approve", systemImage: "checkmark.circle") }
                    .keyboardShortcut(.return, modifiers: []).buttonStyle(.borderedProminent)
            }
        }
        .padding().frame(minWidth: 400)
        .onAppear { updateRemaining() }
        .onReceive(Timer.publish(every: 1, on: .main, in: .common).autoconnect()) { _ in updateRemaining() }
    }

    private func updateRemaining() {
        remainingSeconds = max(0, approval.timeoutSeconds - Int(Date().timeIntervalSince(approval.timestamp)))
    }
}

/// List view of all pending approvals
struct ExecApprovalsListView: View {
    @ObservedObject var manager: ExecApprovalsManager

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text("Pending Approvals").font(.headline)
                Spacer()
                if !manager.pendingApprovals.isEmpty {
                    Button("Reject All") { manager.rejectAll() }.foregroundColor(.red)
                }
            }
            if manager.pendingApprovals.isEmpty {
                Text("No pending approvals").foregroundColor(.secondary)
                    .frame(maxWidth: .infinity, alignment: .center).padding()
            } else {
                ForEach(manager.pendingApprovals) { approval in
                    ExecApprovalView(
                        approval: approval,
                        onApprove: { manager.approve(id: approval.id) },
                        onReject: { manager.reject(id: approval.id) },
                        onAlwaysAllow: { manager.alwaysAllow(id: approval.id) }
                    ).background(Color(NSColor.windowBackgroundColor)).cornerRadius(8)
                }
            }
        }.padding()
    }
}
