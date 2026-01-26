import SwiftUI

/// Settings view for gateway connection configuration.
struct GatewaySettingsView: View {
    @State private var appState = AppStateStore.shared
    @State private var discovery = GatewayDiscovery.shared
    @State private var coordinator = ConnectionModeCoordinator.shared

    @State private var manualHost = ""
    @State private var manualPort = "8080"
    @State private var showingManualEntry = false

    var body: some View {
        Form {
            Section {
                connectionModeSection
            } header: {
                Text("Connection Mode")
            }

            if appState.connectionMode == .remote {
                Section {
                    remoteSettingsSection
                } header: {
                    Text("Remote Server")
                }
            }

            Section {
                discoveredGatewaysSection
            } header: {
                HStack {
                    Text("Discovered Gateways")
                    Spacer()
                    if discovery.isScanning {
                        ProgressView()
                            .controlSize(.small)
                    } else {
                        Button {
                            discovery.startScan()
                        } label: {
                            Image(systemName: "arrow.clockwise")
                        }
                        .buttonStyle(.borderless)
                    }
                }
            }

            Section {
                connectionStatusSection
            } header: {
                Text("Status")
            }
        }
        .formStyle(.grouped)
        .frame(minWidth: 400)
        .sheet(isPresented: $showingManualEntry) {
            manualEntrySheet
        }
        .onAppear {
            discovery.startScan()
        }
    }

    // MARK: - Connection Mode Section

    private var connectionModeSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Picker("Mode", selection: $appState.connectionMode) {
                Text("Local").tag(ConnectionMode.local)
                Text("Remote").tag(ConnectionMode.remote)
            }
            .pickerStyle(.segmented)
            .onChange(of: appState.connectionMode) { _, newMode in
                Task {
                    await coordinator.apply(mode: newMode, paused: appState.isPaused)
                }
            }

            Text(modeDescription)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }

    private var modeDescription: String {
        switch appState.connectionMode {
        case .local:
            return "Run the Nexus gateway on this Mac. Best for personal use."
        case .remote:
            return "Connect to a gateway running on another machine."
        case .unconfigured:
            return "Choose a connection mode to get started."
        }
    }

    // MARK: - Remote Settings Section

    private var remoteSettingsSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            LabeledContent("Host") {
                TextField("hostname or IP", text: Binding(
                    get: { appState.remoteHost ?? "" },
                    set: { appState.remoteHost = $0.isEmpty ? nil : $0 }
                ))
                .textFieldStyle(.roundedBorder)
            }

            LabeledContent("User") {
                TextField("username", text: $appState.remoteUser)
                    .textFieldStyle(.roundedBorder)
            }

            LabeledContent("SSH Key") {
                HStack {
                    Text((appState.remoteIdentityFile as NSString?)?.lastPathComponent ?? "None")
                        .foregroundStyle(.secondary)

                    Spacer()

                    Button("Choose...") {
                        chooseIdentityFile()
                    }

                    if appState.remoteIdentityFile != nil {
                        Button {
                            appState.remoteIdentityFile = nil
                        } label: {
                            Image(systemName: "xmark.circle.fill")
                                .foregroundStyle(.secondary)
                        }
                        .buttonStyle(.borderless)
                    }
                }
            }

            if TailscaleService.shared.isAvailable {
                HStack {
                    Image(systemName: "checkmark.circle.fill")
                        .foregroundStyle(.green)
                    Text("Tailscale available")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    private func chooseIdentityFile() {
        let panel = NSOpenPanel()
        panel.title = "Choose SSH Identity File"
        panel.canChooseFiles = true
        panel.canChooseDirectories = false
        panel.allowsMultipleSelection = false
        panel.directoryURL = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".ssh")

        if panel.runModal() == .OK, let url = panel.url {
            appState.remoteIdentityFile = url.path
        }
    }

    // MARK: - Discovered Gateways Section

    private var discoveredGatewaysSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            if discovery.discoveredGateways.isEmpty {
                HStack {
                    Image(systemName: "magnifyingglass")
                        .foregroundStyle(.tertiary)
                    Text("No gateways found")
                        .foregroundStyle(.secondary)
                }
                .padding(.vertical, 8)
            } else {
                ForEach(discovery.discoveredGateways) { gateway in
                    GatewayRowView(gateway: gateway) {
                        selectGateway(gateway)
                    }
                }
            }

            Button {
                showingManualEntry = true
            } label: {
                Label("Add Manual Entry", systemImage: "plus")
            }
            .buttonStyle(.borderless)
        }
    }

    private func selectGateway(_ gateway: GatewayDiscovery.DiscoveredGateway) {
        if gateway.source == .tailscale || gateway.host.contains(".ts.net") {
            appState.connectionMode = .remote
            appState.remoteHost = gateway.host
        } else if gateway.host == "127.0.0.1" || gateway.host == "localhost" {
            appState.connectionMode = .local
        } else {
            appState.connectionMode = .remote
            appState.remoteHost = gateway.host
        }

        Task {
            await coordinator.apply(mode: appState.connectionMode, paused: appState.isPaused)
        }
    }

    // MARK: - Connection Status Section

    private var connectionStatusSection: some View {
        HStack {
            StatusBadge(status: statusBadgeStatus, variant: .animated)

            VStack(alignment: .leading, spacing: 2) {
                Text(coordinator.statusDescription)
                    .font(.body)

                if let lastConnected = coordinator.lastConnectedAt {
                    Text("Connected \(lastConnected, style: .relative) ago")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                }
            }

            Spacer()

            if coordinator.isConnecting {
                ProgressView()
                    .controlSize(.small)
            } else if !coordinator.isConnected {
                Button("Retry") {
                    Task {
                        await coordinator.reconnect()
                    }
                }
            }
        }
    }

    private var statusBadgeStatus: StatusBadge.Status {
        switch coordinator.state {
        case .connected: return .online
        case .connecting, .reconnecting: return .connecting
        case .error: return .error
        case .disconnected: return .offline
        }
    }

    // MARK: - Manual Entry Sheet

    private var manualEntrySheet: some View {
        VStack(spacing: 16) {
            Text("Add Gateway Manually")
                .font(.headline)

            Form {
                TextField("Host", text: $manualHost)
                    .textFieldStyle(.roundedBorder)

                TextField("Port", text: $manualPort)
                    .textFieldStyle(.roundedBorder)
            }
            .frame(width: 300)

            HStack {
                Button("Cancel") {
                    showingManualEntry = false
                    manualHost = ""
                    manualPort = "3000"
                }
                .keyboardShortcut(.cancelAction)

                Button("Add") {
                    if let port = Int(manualPort), !manualHost.isEmpty {
                        discovery.addManualGateway(host: manualHost, port: port)
                    }
                    showingManualEntry = false
                    manualHost = ""
                    manualPort = "3000"
                }
                .keyboardShortcut(.defaultAction)
                .disabled(manualHost.isEmpty)
            }
        }
        .padding()
        .frame(minWidth: 350)
    }
}

// MARK: - Gateway Row View

struct GatewayRowView: View {
    let gateway: GatewayDiscovery.DiscoveredGateway
    let onSelect: () -> Void

    @State private var isHovered = false

    var body: some View {
        Button(action: onSelect) {
            HStack(spacing: 12) {
                Image(systemName: sourceIcon)
                    .font(.system(size: 16))
                    .foregroundStyle(gateway.isOnline ? .green : .secondary)
                    .frame(width: 24)

                VStack(alignment: .leading, spacing: 2) {
                    Text(gateway.displayName)
                        .font(.body)

                    Text(gateway.connectionString)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                Spacer()

                Text(gateway.source.rawValue)
                    .font(.caption2)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 2)
                    .background(
                        Capsule()
                            .fill(Color.secondary.opacity(0.2))
                    )

                if gateway.isOnline {
                    Circle()
                        .fill(.green)
                        .frame(width: 8, height: 8)
                }
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 8)
            .background(
                RoundedRectangle(cornerRadius: 6, style: .continuous)
                    .fill(isHovered ? Color.accentColor.opacity(0.1) : Color.clear)
            )
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering in
            withAnimation(.easeOut(duration: 0.15)) {
                isHovered = hovering
            }
        }
    }

    private var sourceIcon: String {
        switch gateway.source {
        case .bonjour: return "wifi"
        case .tailscale: return "network"
        case .manual: return "server.rack"
        case .recent: return "clock"
        }
    }
}

#Preview {
    GatewaySettingsView()
        .frame(width: 450, height: 500)
}
