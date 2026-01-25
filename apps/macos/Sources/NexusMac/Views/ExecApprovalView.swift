import SwiftUI

/// SwiftUI view showing a pending command approval
struct ExecApprovalView: View {
    let approval: ExecApproval
    let onApprove: () -> Void
    let onReject: () -> Void
    let onAlwaysAllow: () -> Void

    @State private var remainingSeconds: Int = 0
    @State private var isUrgent = false

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            // Header
            HStack(spacing: 10) {
                ZStack {
                    Circle()
                        .fill(Color.orange.opacity(0.2))
                        .frame(width: 36, height: 36)

                    Image(systemName: "terminal.fill")
                        .font(.system(size: 16))
                        .foregroundStyle(.orange)
                        .symbolEffect(.pulse, isActive: isUrgent)
                }

                VStack(alignment: .leading, spacing: 2) {
                    Text("Command Approval")
                        .font(.headline)
                    Text("Review and approve this command")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                Spacer()

                // Countdown timer
                timerBadge
            }

            // Command preview
            VStack(alignment: .leading, spacing: 8) {
                ScrollView(.horizontal, showsIndicators: false) {
                    Text(approval.command)
                        .font(.system(.body, design: .monospaced))
                        .textSelection(.enabled)
                        .padding(.horizontal, 4)
                }
                .padding(12)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(
                    RoundedRectangle(cornerRadius: 8, style: .continuous)
                        .fill(Color(NSColor.textBackgroundColor))
                )
                .overlay(
                    RoundedRectangle(cornerRadius: 8, style: .continuous)
                        .stroke(isUrgent ? Color.red.opacity(0.5) : Color.orange.opacity(0.3), lineWidth: 1)
                )

                // Working directory
                if !approval.cwd.isEmpty {
                    HStack(spacing: 6) {
                        Image(systemName: "folder.fill")
                            .font(.caption)
                            .foregroundStyle(.tertiary)
                        Text(approval.cwd)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                            .truncationMode(.middle)
                    }
                }
            }

            // Action buttons
            HStack(spacing: 12) {
                Button {
                    withAnimation(.spring(response: 0.3)) {
                        onReject()
                    }
                } label: {
                    Label("Reject", systemImage: "xmark.circle")
                }
                .keyboardShortcut(.escape, modifiers: [])
                .buttonStyle(.bordered)

                Button {
                    onAlwaysAllow()
                } label: {
                    Label("Always Allow", systemImage: "checkmark.shield")
                }
                .buttonStyle(.bordered)

                Spacer()

                Button {
                    withAnimation(.spring(response: 0.3)) {
                        onApprove()
                    }
                } label: {
                    Label("Approve", systemImage: "checkmark.circle.fill")
                }
                .keyboardShortcut(.return, modifiers: [])
                .buttonStyle(.borderedProminent)
            }
        }
        .padding(16)
        .frame(minWidth: 420)
        .background(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .fill(Color(NSColor.windowBackgroundColor))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .stroke(isUrgent ? Color.red.opacity(0.3) : Color.clear, lineWidth: 2)
        )
        .onAppear { updateRemaining() }
        .onReceive(Timer.publish(every: 1, on: .main, in: .common).autoconnect()) { _ in
            updateRemaining()
        }
    }

    private var timerBadge: some View {
        HStack(spacing: 4) {
            Image(systemName: "clock.fill")
                .font(.caption2)
            Text("\(remainingSeconds)s")
                .font(.system(.caption, design: .monospaced).weight(.semibold))
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
        .background(
            Capsule()
                .fill(isUrgent ? Color.red.opacity(0.15) : Color.secondary.opacity(0.1))
        )
        .foregroundStyle(isUrgent ? .red : .secondary)
    }

    private func updateRemaining() {
        remainingSeconds = max(0, approval.timeoutSeconds - Int(Date().timeIntervalSince(approval.timestamp)))
        withAnimation(.easeInOut(duration: 0.3)) {
            isUrgent = remainingSeconds <= 5
        }
    }
}

/// List view of all pending approvals
struct ExecApprovalsListView: View {
    @ObservedObject var manager: ExecApprovalsManager

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            // Header
            HStack {
                Text("Pending Approvals")
                    .font(.headline)

                if !manager.pendingApprovals.isEmpty {
                    Text("\(manager.pendingApprovals.count)")
                        .font(.caption.weight(.semibold))
                        .padding(.horizontal, 8)
                        .padding(.vertical, 2)
                        .background(
                            Capsule()
                                .fill(Color.orange.opacity(0.2))
                        )
                        .foregroundStyle(.orange)
                }

                Spacer()

                if !manager.pendingApprovals.isEmpty {
                    Button {
                        manager.rejectAll()
                    } label: {
                        Label("Reject All", systemImage: "xmark.circle")
                    }
                    .buttonStyle(.bordered)
                    .foregroundStyle(.red)
                }
            }

            // Content
            if manager.pendingApprovals.isEmpty {
                EmptyStateView(
                    icon: "checkmark.shield",
                    title: "No Pending Approvals",
                    description: "Commands requiring approval will appear here."
                )
            } else {
                ScrollView {
                    LazyVStack(spacing: 12) {
                        ForEach(manager.pendingApprovals) { approval in
                            ExecApprovalView(
                                approval: approval,
                                onApprove: { manager.approve(id: approval.id) },
                                onReject: { manager.reject(id: approval.id) },
                                onAlwaysAllow: { manager.alwaysAllow(id: approval.id) }
                            )
                            .transition(.asymmetric(
                                insertion: .move(edge: .top).combined(with: .opacity),
                                removal: .move(edge: .trailing).combined(with: .opacity)
                            ))
                        }
                    }
                }
            }
        }
        .padding()
        .animation(.spring(response: 0.3, dampingFraction: 0.8), value: manager.pendingApprovals.count)
    }
}
