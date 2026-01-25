import SwiftUI

/// Root settings view with tabbed navigation.
/// Provides access to all configuration options.
struct SettingsRootView: View {
    @Bindable var appState: AppStateStore

    var body: some View {
        TabView {
            GeneralSettingsView(appState: appState)
                .tabItem {
                    Label("General", systemImage: "gear")
                }

            GatewaySettingsView()
                .tabItem {
                    Label("Gateway", systemImage: "server.rack")
                }

            VoiceWakeSettingsView(appState: appState)
                .tabItem {
                    Label("Voice", systemImage: "mic")
                }

            ChannelsSettingsView()
                .tabItem {
                    Label("Channels", systemImage: "message")
                }

            CronSettingsView()
                .tabItem {
                    Label("Cron", systemImage: "clock")
                }

            SkillsSettingsView()
                .tabItem {
                    Label("Skills", systemImage: "puzzlepiece.extension")
                }

            SecuritySettingsView()
                .tabItem {
                    Label("Security", systemImage: "shield.lefthalf.filled")
                }

            HotkeysSettingsView()
                .tabItem {
                    Label("Shortcuts", systemImage: "keyboard")
                }

            PermissionsSettingsView()
                .tabItem {
                    Label("Permissions", systemImage: "lock.shield")
                }

            AdvancedSettingsView(appState: appState)
                .tabItem {
                    Label("Advanced", systemImage: "slider.horizontal.3")
                }

            AboutSettingsView()
                .tabItem {
                    Label("About", systemImage: "info.circle")
                }
        }
        .frame(width: 550, height: 400)
    }
}

// MARK: - Settings Tabs

struct GeneralSettingsView: View {
    @Bindable var appState: AppStateStore

    var body: some View {
        Form {
            Section("Startup") {
                Toggle("Launch at Login", isOn: $appState.launchAtLogin)
                Toggle("Show Dock Icon", isOn: $appState.showDockIcon)
            }

            Section("Connection") {
                Picker("Mode", selection: $appState.connectionMode) {
                    Text("Local").tag(ConnectionMode.local)
                    Text("Remote").tag(ConnectionMode.remote)
                }
                .pickerStyle(.segmented)
            }

            Section("Features") {
                Toggle("Node Mode", isOn: $appState.nodeModeEnabled)
                Toggle("Camera Access", isOn: $appState.cameraEnabled)
                Toggle("Messaging Channels", isOn: $appState.channelsEnabled)
            }
        }
        .formStyle(.grouped)
        .padding()
    }
}

struct BasicGatewaySettingsView: View {
    @Bindable var appState: AppStateStore

    var body: some View {
        Form {
            Section("Local Gateway") {
                TextField("Port", value: $appState.gatewayPort, format: .number)
                Toggle("Auto-start", isOn: $appState.gatewayAutostart)
            }

            Section("Remote Gateway") {
                TextField("Host", text: Binding(
                    get: { appState.remoteHost ?? "" },
                    set: { appState.remoteHost = $0.isEmpty ? nil : $0 }
                ))
                TextField("User", text: $appState.remoteUser)
                TextField("Identity File", text: Binding(
                    get: { appState.remoteIdentityFile ?? "" },
                    set: { appState.remoteIdentityFile = $0.isEmpty ? nil : $0 }
                ))
            }
        }
        .formStyle(.grouped)
        .padding()
    }
}


struct ChannelsSettingsView: View {
    @State private var channels = ChannelsStore.shared

    var body: some View {
        VStack {
            if channels.channels.isEmpty {
                ContentUnavailableView(
                    "No Channels",
                    systemImage: "message.badge.waveform",
                    description: Text("Add a messaging channel to get started")
                )
            } else {
                List(channels.channels) { channel in
                    HStack {
                        Image(systemName: channelIcon(channel.type))
                        VStack(alignment: .leading) {
                            Text(channel.name)
                            Text(channel.status.rawValue)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        Spacer()
                        statusIndicator(channel.status)
                    }
                }
            }
        }
        .padding()
        .task {
            await channels.loadChannels()
        }
    }

    private func channelIcon(_ type: ChannelsStore.Channel.ChannelType) -> String {
        switch type {
        case .whatsapp: return "message.fill"
        case .telegram: return "paperplane.fill"
        case .slack: return "number"
        case .discord: return "gamecontroller.fill"
        case .sms: return "bubble.left.fill"
        case .email: return "envelope.fill"
        }
    }

    @ViewBuilder
    private func statusIndicator(_ status: ChannelsStore.Channel.ChannelStatus) -> some View {
        Circle()
            .fill(statusColor(status))
            .frame(width: 8, height: 8)
    }

    private func statusColor(_ status: ChannelsStore.Channel.ChannelStatus) -> Color {
        switch status {
        case .connected: return .green
        case .connecting: return .orange
        case .disconnected: return .gray
        case .error, .needsAuth: return .red
        }
    }
}

struct CronSettingsView: View {
    @State private var cronStore = CronJobsStore.shared

    var body: some View {
        VStack {
            if cronStore.jobs.isEmpty {
                ContentUnavailableView(
                    "No Scheduled Jobs",
                    systemImage: "clock.badge.questionmark",
                    description: Text("Create a cron job to automate tasks")
                )
            } else {
                List(cronStore.jobs) { job in
                    HStack {
                        VStack(alignment: .leading) {
                            Text(job.name)
                            Text(job.schedule.displayString)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        Spacer()
                        Toggle("", isOn: Binding(
                            get: { job.enabled },
                            set: { newValue in
                                Task {
                                    try? await cronStore.setEnabled(newValue, jobId: job.id)
                                }
                            }
                        ))
                        .labelsHidden()
                    }
                }
            }
        }
        .padding()
        .task {
            await cronStore.loadJobs()
        }
    }
}

struct PermissionsSettingsView: View {
    @State private var permissions = PermissionManager.shared

    var body: some View {
        Form {
            Section("System Permissions") {
                PermissionRowView(
                    type: .accessibility,
                    name: "Accessibility",
                    description: "Control other applications",
                    permissions: permissions
                )

                PermissionRowView(
                    type: .screenRecording,
                    name: "Screen Recording",
                    description: "Capture screen content",
                    permissions: permissions
                )

                PermissionRowView(
                    type: .microphone,
                    name: "Microphone",
                    description: "Voice input and wake words",
                    permissions: permissions
                )

                PermissionRowView(
                    type: .speechRecognition,
                    name: "Speech Recognition",
                    description: "Voice command recognition",
                    permissions: permissions
                )

                PermissionRowView(
                    type: .camera,
                    name: "Camera",
                    description: "Visual context for agents",
                    permissions: permissions
                )
            }

            Section {
                Button("Refresh Permissions") {
                    permissions.refreshAllStatuses()
                }
            }
        }
        .formStyle(.grouped)
        .padding()
    }
}

struct PermissionRowView: View {
    let type: PermissionType
    let name: String
    let description: String
    @Bindable var permissions: PermissionManager

    private var isGranted: Bool {
        permissions.status(for: type)
    }

    var body: some View {
        HStack {
            VStack(alignment: .leading) {
                Text(name)
                Text(description)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()

            if isGranted {
                Image(systemName: "checkmark.circle.fill")
                    .foregroundStyle(.green)
            } else {
                Button("Grant") {
                    Task {
                        await requestPermission()
                    }
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
            }
        }
    }

    private func requestPermission() async {
        switch type {
        case .microphone:
            _ = await permissions.requestMicrophoneAccess()
        case .speechRecognition:
            _ = await permissions.requestSpeechRecognitionAccess()
        case .camera:
            _ = await permissions.requestCameraAccess()
        case .accessibility:
            permissions.promptAccessibilityPermission()
        case .screenRecording:
            permissions.openSystemSettings(for: .screenRecording)
        }
    }
}

struct AdvancedSettingsView: View {
    @Bindable var appState: AppStateStore

    var body: some View {
        Form {
            Section("Debugging") {
                Button("Open Logs Folder") {
                    let url = LogLocator.logsDirectory()
                    NSWorkspace.shared.open(url)
                }

                Button("Clear All Data") {
                    appState.resetToDefaults()
                }
                .foregroundStyle(.red)
            }
        }
        .formStyle(.grouped)
        .padding()
    }
}

struct AboutSettingsView: View {
    var body: some View {
        VStack(spacing: 16) {
            Image(systemName: "circle.hexagongrid.fill")
                .font(.system(size: 64))
                .foregroundStyle(.accent)

            Text("Nexus")
                .font(.title)

            Text("Version \(Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "0.0.0")")
                .foregroundStyle(.secondary)

            Text("AI-powered assistant for macOS")
                .font(.caption)
                .foregroundStyle(.secondary)

            Spacer()

            Link("GitHub", destination: URL(string: "https://github.com/haasonsaas/nexus")!)
        }
        .padding()
    }
}

#Preview {
    SettingsRootView(appState: AppStateStore.shared)
}
