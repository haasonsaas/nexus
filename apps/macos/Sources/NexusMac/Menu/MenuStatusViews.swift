import SwiftUI

// MARK: - Menu Status Pill

/// Status pill showing connection state with animated indicators.
/// Displays connection status and optional voice wake status.
struct MenuStatusPill: View {
    @State private var health = HealthStore.shared
    @State private var gateway = ControlChannel.shared
    @State private var voiceWake = VoiceWakeOverlayRuntime.shared
    @State private var appState = AppStateStore.shared

    var body: some View {
        HStack(spacing: 6) {
            // Connection indicator
            connectionIndicator

            // Voice wake indicator (only shown when enabled)
            if appState.voiceWakeEnabled {
                voiceWakeIndicator
            }
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(
            Capsule()
                .fill(Color.secondary.opacity(0.1))
        )
    }

    @ViewBuilder
    private var connectionIndicator: some View {
        HStack(spacing: 4) {
            ZStack {
                Circle()
                    .fill(connectionColor)
                    .frame(width: 8, height: 8)

                if case .connecting = gateway.state {
                    Circle()
                        .stroke(connectionColor, lineWidth: 2)
                        .frame(width: 12, height: 12)
                        .scaleEffect(pulseScale)
                        .opacity(pulseOpacity)
                }
            }

            Text(connectionText)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }

    @ViewBuilder
    private var voiceWakeIndicator: some View {
        HStack(spacing: 4) {
            Image(systemName: "mic.fill")
                .font(.caption)
                .foregroundStyle(voiceWake.isListening ? .green : .secondary)
                .symbolEffect(.pulse, isActive: voiceWake.isCapturing)
        }
    }

    // MARK: - Animation State

    @State private var pulseScale: CGFloat = 1.0
    @State private var pulseOpacity: Double = 0.6

    private var connectionColor: Color {
        switch gateway.state {
        case .connected:
            return health.state == .ok ? .green : .orange
        case .connecting:
            return .orange
        case .disconnected:
            return .red
        case .degraded:
            return .orange
        }
    }

    private var connectionText: String {
        switch gateway.state {
        case .connected:
            return health.state == .ok ? "Connected" : "Degraded"
        case .connecting:
            return "Connecting"
        case .disconnected:
            return "Disconnected"
        case .degraded(let msg):
            return msg.count > 15 ? "Degraded" : msg
        }
    }
}

// MARK: - Critter Status View

/// Animated critter status label showing agent activity.
/// Displays different animals based on whether agents are running.
struct CritterStatusView: View {
    @State private var orchestrator = AgentOrchestrator.shared
    @State private var blinkOpacity: Double = 1.0
    @State private var wiggleAngle: Double = 0
    @State private var lastRunningState = false

    var body: some View {
        HStack(spacing: 8) {
            // Animated critter
            Image(systemName: critterIcon)
                .font(.system(size: 24))
                .foregroundStyle(critterColor)
                .opacity(blinkOpacity)
                .rotationEffect(.degrees(wiggleAngle))
                .contentTransition(.symbolEffect(.replace))

            VStack(alignment: .leading, spacing: 2) {
                Text(statusText)
                    .font(.subheadline.weight(.medium))

                if let agent = orchestrator.activeAgents.first,
                   let task = agent.currentTask, !task.isEmpty {
                    Text(task)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }
        }
        .onAppear {
            startBlinkAnimation()
        }
        .onChange(of: isAnyAgentRunning) { _, isRunning in
            if isRunning && !lastRunningState {
                startWiggle()
            }
            lastRunningState = isRunning
        }
    }

    private var isAnyAgentRunning: Bool {
        !orchestrator.activeAgents.isEmpty && orchestrator.isProcessing
    }

    private var critterIcon: String {
        if isAnyAgentRunning {
            return "hare.fill"
        } else {
            return "tortoise.fill"
        }
    }

    private var critterColor: Color {
        if isAnyAgentRunning {
            return .blue
        } else {
            return .secondary
        }
    }

    private var statusText: String {
        if let agent = orchestrator.activeAgents.first {
            switch agent.status {
            case .executing: return "Working"
            case .thinking: return "Thinking"
            case .waiting: return "Waiting"
            case .completed: return "Completed"
            case .error: return "Error"
            case .idle: return "Active"
            }
        }
        return "Ready"
    }

    private func startBlinkAnimation() {
        withAnimation(.easeInOut(duration: 2).repeatForever(autoreverses: true)) {
            blinkOpacity = 0.7
        }
    }

    private func startWiggle() {
        withAnimation(.easeInOut(duration: 0.15).repeatCount(3, autoreverses: true)) {
            wiggleAngle = 5
        }
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
            withAnimation(.easeOut) {
                wiggleAngle = 0
            }
        }
    }
}

// MARK: - Menu Quick Toggles

/// Quick toggles for menu bar settings.
/// Provides fast access to voice, camera, canvas, and exec mode settings.
struct MenuQuickToggles: View {
    @State private var appState = AppStateStore.shared
    @State private var voiceWake = VoiceWakeOverlayRuntime.shared

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            // Voice Wake Toggle
            Toggle(isOn: Binding(
                get: { appState.voiceWakeEnabled },
                set: { enabled in
                    Task {
                        await appState.setVoiceWakeEnabled(enabled)
                    }
                }
            )) {
                Label("Voice Wake", systemImage: "mic")
                    .font(.subheadline)
            }
            .toggleStyle(.switch)
            .controlSize(.small)

            // Camera Toggle
            Toggle(isOn: $appState.cameraEnabled) {
                Label("Camera", systemImage: "camera")
                    .font(.subheadline)
            }
            .toggleStyle(.switch)
            .controlSize(.small)

            // Canvas Toggle
            Toggle(isOn: $appState.canvasEnabled) {
                Label("Canvas", systemImage: "rectangle.on.rectangle")
                    .font(.subheadline)
            }
            .toggleStyle(.switch)
            .controlSize(.small)

            // Exec Approval Mode
            VStack(alignment: .leading, spacing: 4) {
                Text("Exec Mode")
                    .font(.caption)
                    .foregroundStyle(.secondary)

                Picker("", selection: $appState.execApprovalMode) {
                    Text("Deny").tag(ExecApprovalMode.deny)
                    Text("Ask").tag(ExecApprovalMode.prompt)
                    Text("Allow").tag(ExecApprovalMode.approve)
                }
                .pickerStyle(.segmented)
                .labelsHidden()
            }
        }
    }
}

// MARK: - Menu Session Preview

/// Compact preview of a chat session for the menu.
/// Shows title, last message, and metadata.
struct MenuSessionPreview: View {
    let session: ChatSession

    @State private var isHovered = false

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(session.title ?? "Untitled")
                    .font(.subheadline.weight(.medium))
                    .lineLimit(1)

                Spacer()

                Text(session.updatedAt, style: .relative)
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }

            if let lastMessage = session.lastMessage, !lastMessage.isEmpty {
                Text(lastMessage)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }

            HStack(spacing: 8) {
                Label("\(session.messageCount)", systemImage: "message")

                if let model = session.model {
                    Text("*")
                        .foregroundStyle(.tertiary)
                    Text(model)
                }
            }
            .font(.caption2)
            .foregroundStyle(.tertiary)
        }
        .padding(8)
        .background(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .fill(isHovered ? Color.accentColor.opacity(0.1) : Color.secondary.opacity(0.05))
        )
        .onHover { hovering in
            withAnimation(.easeOut(duration: 0.15)) {
                isHovered = hovering
            }
        }
    }
}

// MARK: - Agent Instance Row

/// Row displaying a single agent instance in the menu.
/// Shows type, status, and current task.
struct AgentInstanceRow: View {
    let agent: AgentOrchestrator.AgentInstance

    @State private var isHovered = false

    var body: some View {
        HStack(spacing: 10) {
            // Type icon
            ZStack {
                Circle()
                    .fill(agentColor.opacity(0.2))
                    .frame(width: 28, height: 28)

                Image(systemName: agentIcon)
                    .font(.system(size: 12))
                    .foregroundStyle(agentColor)
                    .symbolEffect(.pulse, isActive: agent.status == .executing || agent.status == .thinking)
            }

            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(agentTypeName)
                        .font(.caption.weight(.medium))

                    AgentStatusBadge(status: agent.status)
                }

                if let task = agent.currentTask, !task.isEmpty {
                    Text(task)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }

            Spacer()

            // Activity indicator
            Text(agent.lastActivityAt, style: .relative)
                .font(.caption2)
                .foregroundStyle(.tertiary)
        }
        .padding(8)
        .background(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .fill(isHovered ? Color.accentColor.opacity(0.1) : Color.secondary.opacity(0.05))
        )
        .onHover { hovering in
            withAnimation(.easeOut(duration: 0.15)) {
                isHovered = hovering
            }
        }
    }

    private var agentTypeName: String {
        switch agent.type {
        case .computerUse: return "Computer Use"
        case .coder: return "Coder"
        case .researcher: return "Researcher"
        case .assistant: return "Assistant"
        case .custom: return "Custom"
        }
    }

    private var agentIcon: String {
        switch agent.type {
        case .computerUse: return "desktopcomputer"
        case .coder: return "chevron.left.forwardslash.chevron.right"
        case .researcher: return "magnifyingglass"
        case .assistant: return "bubble.left.and.bubble.right"
        case .custom: return "cpu"
        }
    }

    private var agentColor: Color {
        switch agent.type {
        case .computerUse: return .purple
        case .coder: return .blue
        case .researcher: return .green
        case .assistant: return .orange
        case .custom: return .gray
        }
    }
}

// MARK: - Menu Content View

/// Main menu content view combining all status components.
/// Provides a comprehensive overview and quick access to features.
struct MenuContentViewV2: View {
    @State private var orchestrator = AgentOrchestrator.shared
    @State private var sessions = ChatSessionManager.shared
    @State private var health = HealthStore.shared

    var onOpenChat: () -> Void = {}
    var onOpenSettings: () -> Void = {}

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            // Status header
            HStack {
                CritterStatusView()
                Spacer()
                MenuStatusPill()
            }

            Divider()

            // Quick toggles
            MenuQuickToggles()

            Divider()

            // Active agents
            if !orchestrator.activeAgents.isEmpty {
                VStack(alignment: .leading, spacing: 6) {
                    Text("Active")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(.secondary)

                    ForEach(orchestrator.activeAgents) { agent in
                        AgentInstanceRow(agent: agent)
                    }
                }

                Divider()
            }

            // Recent sessions
            VStack(alignment: .leading, spacing: 6) {
                HStack {
                    Text("Recent Sessions")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(.secondary)

                    Spacer()

                    Text("\(sessions.sessions.count)")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                }

                if sessions.sessions.isEmpty {
                    HStack(spacing: 8) {
                        Image(systemName: "tray")
                            .foregroundStyle(.tertiary)
                        Text("No sessions")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    .padding(.vertical, 8)
                } else {
                    ForEach(sessions.recentSessions.prefix(3)) { session in
                        MenuSessionPreview(session: session)
                            .contentShape(Rectangle())
                            .onTapGesture {
                                Task {
                                    try? await sessions.switchSession(session.id)
                                    WebChatManager.shared.openChat(for: session.id)
                                }
                            }
                    }
                }
            }

            Divider()

            // Actions
            HStack(spacing: 12) {
                Button {
                    onOpenChat()
                } label: {
                    Label("New Chat", systemImage: "plus.message")
                        .font(.caption)
                }
                .buttonStyle(.bordered)
                .controlSize(.small)

                Spacer()

                Button {
                    onOpenSettings()
                } label: {
                    Image(systemName: "gear")
                        .font(.caption)
                }
                .buttonStyle(.plain)
                .foregroundStyle(.secondary)

                Button {
                    NSApplication.shared.terminate(nil)
                } label: {
                    Image(systemName: "power")
                        .font(.caption)
                }
                .buttonStyle(.plain)
                .foregroundStyle(.secondary)
            }
        }
        .padding()
        .frame(width: 300)
    }
}

// MARK: - Previews

#Preview("Menu Content V2") {
    MenuContentViewV2()
}

#Preview("Status Pill") {
    MenuStatusPill()
        .padding()
}

#Preview("Critter Status") {
    CritterStatusView()
        .padding()
}

#Preview("Quick Toggles") {
    MenuQuickToggles()
        .padding()
        .frame(width: 300)
}

#Preview("Session Preview") {
    MenuSessionPreview(session: ChatSession(
        id: "test-123",
        title: "Test Session",
        model: "claude-3-opus"
    ))
    .padding()
    .frame(width: 280)
}
