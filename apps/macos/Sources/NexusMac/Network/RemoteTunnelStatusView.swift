import SwiftUI

// MARK: - RemoteTunnelStatusView

/// Displays the status and configuration of remote port tunnels.
struct RemoteTunnelStatusView: View {
    @State private var tunnel = RemotePortTunnel.shared

    @State private var showConfiguration = false
    @State private var isReconnecting = false

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            // Header with status
            headerSection

            Divider()

            // Connection details
            if tunnel.state.isActive || tunnel.connectedHost != nil {
                connectionDetailsSection
            }

            // Active forwards
            if !tunnel.activeForwards.isEmpty {
                forwardsSection
            }

            // Error display
            if let error = tunnel.lastError {
                errorSection(error: error)
            }

            // Actions
            actionsSection
        }
        .padding()
        .frame(minWidth: 320)
    }

    // MARK: - Header Section

    private var headerSection: some View {
        HStack(spacing: 12) {
            // Status indicator
            statusIndicator

            VStack(alignment: .leading, spacing: 2) {
                Text("Remote Tunnel")
                    .font(.headline)

                Text(statusDescription)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            // Configuration button
            Button {
                showConfiguration.toggle()
            } label: {
                Image(systemName: "gearshape")
                    .font(.system(size: 14))
            }
            .buttonStyle(.plain)
            .foregroundStyle(.secondary)
            .help("Tunnel Configuration")
            .popover(isPresented: $showConfiguration) {
                TunnelConfigurationView(tunnel: tunnel)
            }
        }
    }

    @ViewBuilder
    private var statusIndicator: some View {
        ZStack {
            Circle()
                .fill(statusColor.opacity(0.2))
                .frame(width: 36, height: 36)

            Circle()
                .fill(statusColor)
                .frame(width: 12, height: 12)

            if tunnel.state == .connecting {
                Circle()
                    .stroke(statusColor.opacity(0.5), lineWidth: 2)
                    .frame(width: 24, height: 24)
                    .rotationEffect(.degrees(isReconnecting ? 360 : 0))
                    .animation(.linear(duration: 1).repeatForever(autoreverses: false), value: isReconnecting)
                    .onAppear { isReconnecting = true }
                    .onDisappear { isReconnecting = false }
            }
        }
    }

    private var statusColor: Color {
        switch tunnel.state {
        case .connected:
            return .green
        case .connecting:
            return .orange
        case .disconnected:
            return .secondary
        case .error:
            return .red
        }
    }

    private var statusDescription: String {
        switch tunnel.state {
        case .connected:
            if let lastConnected = tunnel.lastConnectedAt {
                return "Connected \(age(from: lastConnected))"
            }
            return "Connected"
        case .connecting:
            return "Establishing connection..."
        case .disconnected:
            return "Not connected"
        case .error(let message):
            return message
        }
    }

    // MARK: - Connection Details Section

    private var connectionDetailsSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Connection")
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.secondary)

            VStack(alignment: .leading, spacing: 6) {
                if let host = tunnel.connectedHost {
                    detailRow(label: "Host", value: host)
                }
                if let port = tunnel.connectedPort, port != 22 {
                    detailRow(label: "SSH Port", value: "\(port)")
                }
                if let user = tunnel.connectedUser {
                    detailRow(label: "User", value: user)
                }
            }
            .padding(10)
            .background(
                RoundedRectangle(cornerRadius: 8, style: .continuous)
                    .fill(Color(NSColor.controlBackgroundColor))
            )
        }
    }

    private func detailRow(label: String, value: String) -> some View {
        HStack {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
                .frame(width: 60, alignment: .leading)

            Text(value)
                .font(.caption.monospaced())
                .foregroundStyle(.primary)

            Spacer()
        }
    }

    // MARK: - Forwards Section

    private var forwardsSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Port Forwards")
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.secondary)

            VStack(spacing: 4) {
                ForEach(tunnel.activeForwards) { forward in
                    forwardRow(forward)
                }
            }
            .padding(10)
            .background(
                RoundedRectangle(cornerRadius: 8, style: .continuous)
                    .fill(Color(NSColor.controlBackgroundColor))
            )
        }
    }

    private func forwardRow(_ forward: PortForward) -> some View {
        HStack(spacing: 8) {
            // Direction icon
            Image(systemName: forward.direction == .local ? "arrow.down.circle" : "arrow.up.circle")
                .font(.system(size: 12))
                .foregroundStyle(forward.direction == .local ? .blue : .green)

            // Direction label
            Text(forward.direction == .local ? "L" : "R")
                .font(.caption2.weight(.semibold))
                .foregroundStyle(.secondary)
                .frame(width: 12)

            // Port mapping
            Text(forwardDescription(forward))
                .font(.caption.monospaced())
                .foregroundStyle(.primary)

            Spacer()

            // Copy button
            Button {
                copyForwardToClipboard(forward)
            } label: {
                Image(systemName: "doc.on.doc")
                    .font(.system(size: 10))
            }
            .buttonStyle(.plain)
            .foregroundStyle(.secondary)
            .help("Copy to clipboard")
        }
    }

    private func forwardDescription(_ forward: PortForward) -> String {
        switch forward.direction {
        case .local:
            return "localhost:\(forward.localPort) -> \(forward.remoteHost):\(forward.remotePort)"
        case .remote:
            return "remote:\(forward.remotePort) -> \(forward.remoteHost):\(forward.localPort)"
        }
    }

    private func copyForwardToClipboard(_ forward: PortForward) {
        let text = forwardDescription(forward)
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
    }

    // MARK: - Error Section

    private func errorSection(error: String) -> some View {
        HStack(spacing: 8) {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.orange)

            Text(error)
                .font(.caption)
                .foregroundStyle(.secondary)

            Spacer()
        }
        .padding(10)
        .background(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .fill(Color.orange.opacity(0.1))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .strokeBorder(Color.orange.opacity(0.3), lineWidth: 1)
        )
    }

    // MARK: - Actions Section

    private var actionsSection: some View {
        HStack(spacing: 12) {
            if tunnel.state == .connected {
                Button("Disconnect") {
                    tunnel.disconnect()
                }
                .buttonStyle(.bordered)
            } else if tunnel.state == .disconnected || tunnel.state.isError {
                Button("Reconnect") {
                    Task {
                        try? await tunnel.reconnect()
                    }
                }
                .buttonStyle(.borderedProminent)
                .disabled(!tunnel.canReconnect)
            }

            if tunnel.reconnectAttempts > 0 {
                Text("Attempt \(tunnel.reconnectAttempts)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Spacer()
        }
    }
}

// Extension to check if state is error
extension TunnelState {
    var isError: Bool {
        if case .error = self { return true }
        return false
    }
}

// MARK: - TunnelConfigurationView

/// Configuration panel for tunnel settings.
struct TunnelConfigurationView: View {
    let tunnel: RemotePortTunnel

    @State private var host: String = ""
    @State private var port: String = "22"
    @State private var user: String = ""
    @State private var identityPath: String = ""
    @State private var localPort: String = "8080"
    @State private var remotePort: String = "8080"

    @State private var autoReconnect: Bool = true
    @State private var maxAttempts: String = "5"

    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Tunnel Configuration")
                .font(.headline)

            // Connection settings
            GroupBox("SSH Connection") {
                VStack(alignment: .leading, spacing: 10) {
                    HStack {
                        Text("Host:")
                            .frame(width: 80, alignment: .trailing)
                        TextField("hostname.example.com", text: $host)
                            .textFieldStyle(.roundedBorder)
                    }

                    HStack {
                        Text("Port:")
                            .frame(width: 80, alignment: .trailing)
                        TextField("22", text: $port)
                            .textFieldStyle(.roundedBorder)
                            .frame(width: 60)
                        Spacer()
                    }

                    HStack {
                        Text("User:")
                            .frame(width: 80, alignment: .trailing)
                        TextField("username", text: $user)
                            .textFieldStyle(.roundedBorder)
                    }

                    HStack {
                        Text("Identity:")
                            .frame(width: 80, alignment: .trailing)
                        TextField("~/.ssh/id_ed25519", text: $identityPath)
                            .textFieldStyle(.roundedBorder)

                        Button("Browse...") {
                            selectIdentityFile()
                        }
                        .controlSize(.small)
                    }
                }
                .padding(8)
            }

            // Port forwarding
            GroupBox("Port Forwarding") {
                VStack(alignment: .leading, spacing: 10) {
                    HStack {
                        Text("Local:")
                            .frame(width: 80, alignment: .trailing)
                        TextField("8080", text: $localPort)
                            .textFieldStyle(.roundedBorder)
                            .frame(width: 60)

                        Image(systemName: "arrow.right")
                            .foregroundStyle(.secondary)

                        Text("Remote:")
                        TextField("8080", text: $remotePort)
                            .textFieldStyle(.roundedBorder)
                            .frame(width: 60)

                        Spacer()
                    }
                }
                .padding(8)
            }

            // Reconnection settings
            GroupBox("Auto-Reconnect") {
                VStack(alignment: .leading, spacing: 10) {
                    Toggle("Enable auto-reconnect", isOn: $autoReconnect)

                    if autoReconnect {
                        HStack {
                            Text("Max attempts:")
                            TextField("5", text: $maxAttempts)
                                .textFieldStyle(.roundedBorder)
                                .frame(width: 40)
                            Text("(0 = unlimited)")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
                .padding(8)
            }

            Divider()

            // Actions
            HStack {
                Button("Cancel") {
                    dismiss()
                }
                .keyboardShortcut(.cancelAction)

                Spacer()

                Button("Connect") {
                    connectWithConfiguration()
                }
                .keyboardShortcut(.defaultAction)
                .disabled(!isValid)
            }
        }
        .padding()
        .frame(width: 400)
        .onAppear {
            loadCurrentSettings()
        }
    }

    private var isValid: Bool {
        !host.isEmpty && !user.isEmpty && Int(port) != nil
    }

    private func loadCurrentSettings() {
        if let connectedHost = tunnel.connectedHost {
            host = connectedHost
        }
        if let connectedPort = tunnel.connectedPort {
            port = String(connectedPort)
        }
        if let connectedUser = tunnel.connectedUser {
            user = connectedUser
        }
        autoReconnect = tunnel.autoReconnect
        maxAttempts = String(tunnel.maxReconnectAttempts)

        if let forward = tunnel.activeForwards.first {
            localPort = String(forward.localPort)
            remotePort = String(forward.remotePort)
        }
    }

    private func selectIdentityFile() {
        let panel = NSOpenPanel()
        panel.allowsMultipleSelection = false
        panel.canChooseDirectories = false
        panel.canChooseFiles = true
        panel.directoryURL = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".ssh")

        if panel.runModal() == .OK, let url = panel.url {
            identityPath = url.path
        }
    }

    private func connectWithConfiguration() {
        guard let sshPort = Int(port),
              let local = Int(localPort),
              let remote = Int(remotePort) else {
            return
        }

        let identityURL: URL? = identityPath.isEmpty ? nil : URL(fileURLWithPath: expandPath(identityPath))

        tunnel.autoReconnect = autoReconnect
        tunnel.maxReconnectAttempts = Int(maxAttempts) ?? 5

        // Add forward
        tunnel.clearPendingForwards()
        tunnel.forwardLocal(localPort: local, remoteHost: "127.0.0.1", remotePort: remote)

        Task {
            try? await tunnel.connect(
                host: host,
                port: sshPort,
                user: user,
                identityFile: identityURL
            )
            await MainActor.run {
                dismiss()
            }
        }
    }

    private func expandPath(_ path: String) -> String {
        if path.hasPrefix("~") {
            return (path as NSString).expandingTildeInPath
        }
        return path
    }
}

// MARK: - Compact Status View

/// Compact tunnel status for menu bar or sidebar display.
struct CompactTunnelStatusView: View {
    @State private var tunnel = RemotePortTunnel.shared

    var body: some View {
        HStack(spacing: 8) {
            // Status dot
            Circle()
                .fill(statusColor)
                .frame(width: 8, height: 8)

            // Label
            Text(statusLabel)
                .font(.caption)
                .foregroundStyle(.secondary)

            if tunnel.state == .connected, let port = tunnel.primaryLocalPort {
                Text(":\(port)")
                    .font(.caption.monospaced())
                    .foregroundStyle(.tertiary)
            }
        }
    }

    private var statusColor: Color {
        switch tunnel.state {
        case .connected: return .green
        case .connecting: return .orange
        case .disconnected: return .secondary
        case .error: return .red
        }
    }

    private var statusLabel: String {
        switch tunnel.state {
        case .connected:
            return "Tunnel"
        case .connecting:
            return "Connecting..."
        case .disconnected:
            return "No tunnel"
        case .error:
            return "Error"
        }
    }
}

// MARK: - Tunnel Status Row

/// Row view for displaying tunnel status in lists.
struct TunnelStatusRow: View {
    let tunnel: RemotePortTunnel

    var body: some View {
        HStack(spacing: 12) {
            // Icon
            Image(systemName: tunnelIcon)
                .font(.system(size: 20))
                .foregroundStyle(iconColor)
                .frame(width: 32)

            // Info
            VStack(alignment: .leading, spacing: 2) {
                Text(tunnel.connectionDescription)
                    .font(.subheadline.weight(.medium))

                if !tunnel.activeForwards.isEmpty {
                    Text("\(tunnel.activeForwards.count) port forward\(tunnel.activeForwards.count == 1 ? "" : "s")")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }

            Spacer()

            // Status badge
            statusBadge
        }
        .padding(.vertical, 4)
    }

    private var tunnelIcon: String {
        switch tunnel.state {
        case .connected: return "tunnel.fill"
        case .connecting: return "network"
        case .disconnected: return "tunnel"
        case .error: return "exclamationmark.triangle"
        }
    }

    private var iconColor: Color {
        switch tunnel.state {
        case .connected: return .green
        case .connecting: return .orange
        case .disconnected: return .secondary
        case .error: return .red
        }
    }

    @ViewBuilder
    private var statusBadge: some View {
        HStack(spacing: 4) {
            Circle()
                .fill(iconColor)
                .frame(width: 6, height: 6)

            Text(statusText)
                .font(.caption)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(
            Capsule()
                .fill(iconColor.opacity(0.15))
        )
    }

    private var statusText: String {
        switch tunnel.state {
        case .connected: return "Connected"
        case .connecting: return "Connecting"
        case .disconnected: return "Disconnected"
        case .error: return "Error"
        }
    }
}

// MARK: - Previews

#Preview("Tunnel Status - Disconnected") {
    RemoteTunnelStatusView()
        .frame(width: 360)
}

#Preview("Compact Status") {
    CompactTunnelStatusView()
        .padding()
}

#Preview("Configuration") {
    TunnelConfigurationView(tunnel: RemotePortTunnel.shared)
}
