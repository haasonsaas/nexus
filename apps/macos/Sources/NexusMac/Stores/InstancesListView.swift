import SwiftUI

/// A view displaying all known Nexus instances with status indicators.
struct InstancesListView: View {
    @State private var store = InstancesStore.shared
    @State private var selectedInstance: NexusInstance?
    @State private var isRefreshing = false
    @State private var messageText = ""
    @State private var showMessageSheet = false
    @State private var sendingMessage = false
    @State private var messageError: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            headerView
            instancesList
        }
        .padding()
        .sheet(isPresented: $showMessageSheet) {
            messageSheet
        }
    }

    // MARK: - Header

    private var headerView: some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                Text("Instances")
                    .font(.title2)
                Text("\(store.activeInstances.count) active of \(store.instances.count) total")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            Button {
                refreshInstances()
            } label: {
                Image(systemName: "arrow.clockwise")
            }
            .disabled(isRefreshing)
            .help("Refresh instances")
        }
    }

    // MARK: - Instances List

    private var instancesList: some View {
        Group {
            if store.instances.isEmpty {
                emptyState
            } else {
                List(store.instances, selection: $selectedInstance) { instance in
                    InstanceRow(
                        instance: instance,
                        isCurrentDevice: instance.id == store.currentInstance.id,
                        onSendMessage: {
                            selectedInstance = instance
                            showMessageSheet = true
                        }
                    )
                }
                .listStyle(.inset(alternatesRowBackgrounds: true))
            }
        }
    }

    private var emptyState: some View {
        VStack(spacing: 12) {
            Image(systemName: "desktopcomputer")
                .font(.system(size: 40))
                .foregroundStyle(.tertiary)

            Text("No Instances")
                .font(.headline)

            Text("Other Nexus instances will appear here when they connect.")
                .font(.caption)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)

            Button("Refresh") {
                refreshInstances()
            }
            .buttonStyle(.bordered)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding()
    }

    // MARK: - Message Sheet

    private var messageSheet: some View {
        VStack(spacing: 16) {
            HStack {
                Text("Send Message")
                    .font(.headline)
                Spacer()
                Button("Cancel") {
                    showMessageSheet = false
                    messageText = ""
                    messageError = nil
                }
                .buttonStyle(.borderless)
            }

            if let instance = selectedInstance {
                HStack(spacing: 8) {
                    InstanceStatusDot(status: instance.connectionStatus, size: 10)
                    Text("To: \(instance.displayName)")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }

            TextEditor(text: $messageText)
                .font(.body)
                .frame(minHeight: 100)
                .overlay(
                    RoundedRectangle(cornerRadius: 6)
                        .stroke(Color.gray.opacity(0.3), lineWidth: 1)
                )

            if let error = messageError {
                Text(error)
                    .font(.caption)
                    .foregroundStyle(.red)
            }

            HStack {
                Spacer()
                Button("Send") {
                    sendMessage()
                }
                .buttonStyle(.borderedProminent)
                .disabled(messageText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || sendingMessage)
            }
        }
        .padding()
        .frame(width: 400, height: 250)
    }

    // MARK: - Actions

    private func refreshInstances() {
        isRefreshing = true
        Task {
            await store.refresh()
            isRefreshing = false
        }
    }

    private func sendMessage() {
        guard let instance = selectedInstance else { return }
        let text = messageText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }

        sendingMessage = true
        messageError = nil

        Task {
            do {
                try await store.sendMessageTo(instanceId: instance.id, message: text)
                showMessageSheet = false
                messageText = ""
            } catch {
                messageError = error.localizedDescription
            }
            sendingMessage = false
        }
    }
}

// MARK: - Instance Row

private struct InstanceRow: View {
    let instance: NexusInstance
    let isCurrentDevice: Bool
    let onSendMessage: () -> Void

    var body: some View {
        HStack(spacing: 12) {
            InstanceStatusDot(status: instance.connectionStatus, size: 12)

            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 6) {
                    Text(instance.displayName)
                        .font(.headline)
                        .lineLimit(1)

                    if isCurrentDevice {
                        Text("This Device")
                            .font(.caption2)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Color.accentColor.opacity(0.2))
                            .foregroundStyle(.accentColor)
                            .clipShape(Capsule())
                    }
                }

                HStack(spacing: 8) {
                    Text(instance.ipAddress)
                        .font(.caption)
                        .foregroundStyle(.secondary)

                    Text(instance.platform)
                        .font(.caption)
                        .foregroundStyle(.tertiary)

                    if let model = instance.modelIdentifier {
                        Text(model)
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                }

                HStack(spacing: 8) {
                    Text("v\(instance.appVersion)")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)

                    Text(lastSeenText)
                        .font(.caption2)
                        .foregroundStyle(instance.isActive ? .secondary : .tertiary)
                }
            }

            Spacer()

            if !isCurrentDevice && instance.connectionStatus == .online {
                Button {
                    onSendMessage()
                } label: {
                    Image(systemName: "paperplane")
                        .font(.caption)
                }
                .buttonStyle(.borderless)
                .help("Send message to this instance")
            }
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
    }

    private var lastSeenText: String {
        let interval = Date().timeIntervalSince(instance.lastSeen)

        if interval < 60 {
            return "Just now"
        } else if interval < 3600 {
            let minutes = Int(interval / 60)
            return "\(minutes)m ago"
        } else if interval < 86400 {
            let hours = Int(interval / 3600)
            return "\(hours)h ago"
        } else {
            let days = Int(interval / 86400)
            return "\(days)d ago"
        }
    }
}

// MARK: - Status Dot

private struct InstanceStatusDot: View {
    let status: NexusInstance.ConnectionStatus
    let size: CGFloat

    var body: some View {
        ZStack {
            Circle()
                .fill(statusColor.opacity(0.2))
                .frame(width: size * 2, height: size * 2)

            Circle()
                .fill(statusColor)
                .frame(width: size, height: size)

            if status == .online {
                Circle()
                    .stroke(statusColor.opacity(0.5), lineWidth: 1)
                    .frame(width: size * 2, height: size * 2)
            }
        }
    }

    private var statusColor: Color {
        switch status {
        case .online:
            return .green
        case .offline:
            return .red
        case .connecting:
            return .orange
        case .unknown:
            return .gray
        }
    }
}

// MARK: - Preview

#if DEBUG
struct InstancesListView_Previews: PreviewProvider {
    static var previews: some View {
        InstancesListView()
            .frame(width: 500, height: 400)
    }
}
#endif
