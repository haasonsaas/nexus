import AppKit
import OSLog
import SwiftUI

// MARK: - Onboarding Step Model

/// Onboarding step definition
struct OnboardingStep: Identifiable {
    let id: Int
    let title: String
    let subtitle: String
    let icon: String
    let action: OnboardingAction?

    enum OnboardingAction {
        case requestPermission(PermissionType)
        case configureGateway
        case enableVoiceWake
        case completeSetup
    }
}

// MARK: - Main Onboarding View

/// Main onboarding view with multi-step wizard flow
struct OnboardingView: View {
    @State private var currentStep = 0
    @State private var isAnimating = false
    @State private var appState = AppStateStore.shared
    @State private var permissions = PermissionManager.shared

    private let steps: [OnboardingStep] = [
        OnboardingStep(
            id: 0,
            title: "Welcome to Nexus",
            subtitle: "Your AI-powered assistant for macOS",
            icon: "circle.hexagongrid.fill",
            action: nil
        ),
        OnboardingStep(
            id: 1,
            title: "Connect to Gateway",
            subtitle: "Configure your connection to the Nexus gateway",
            icon: "server.rack",
            action: .configureGateway
        ),
        OnboardingStep(
            id: 2,
            title: "Accessibility Permission",
            subtitle: "Allow Nexus to control your computer for automation tasks",
            icon: "accessibility",
            action: .requestPermission(.accessibility)
        ),
        OnboardingStep(
            id: 3,
            title: "Screen Recording",
            subtitle: "Enable screen capture for visual context",
            icon: "rectangle.dashed.badge.record",
            action: .requestPermission(.screenRecording)
        ),
        OnboardingStep(
            id: 4,
            title: "Microphone Access",
            subtitle: "Enable voice commands and dictation",
            icon: "mic",
            action: .requestPermission(.microphone)
        ),
        OnboardingStep(
            id: 5,
            title: "Voice Wake",
            subtitle: "Say \"Hey Nexus\" to activate",
            icon: "waveform",
            action: .enableVoiceWake
        ),
        OnboardingStep(
            id: 6,
            title: "You're All Set!",
            subtitle: "Nexus is ready to help you",
            icon: "checkmark.circle.fill",
            action: .completeSetup
        ),
    ]

    var body: some View {
        VStack(spacing: 0) {
            // Progress indicator
            progressIndicator
                .padding(.top, 20)

            // Content
            TabView(selection: $currentStep) {
                ForEach(steps) { step in
                    OnboardingStepView(
                        step: step,
                        onContinue: { advanceStep() },
                        onSkip: { skipStep() }
                    )
                    .tag(step.id)
                }
            }
            .tabViewStyle(.automatic)

            Divider()

            // Navigation footer
            navigationFooter
                .padding(.horizontal, 24)
                .padding(.vertical, 16)
        }
        .frame(width: 600, height: 500)
        .background(Color(NSColor.windowBackgroundColor))
    }

    // MARK: - Progress Indicator

    private var progressIndicator: some View {
        HStack(spacing: 8) {
            ForEach(0..<steps.count, id: \.self) { index in
                Circle()
                    .fill(index <= currentStep ? Color.accentColor : Color.secondary.opacity(0.3))
                    .frame(width: 8, height: 8)
                    .animation(.easeInOut(duration: 0.2), value: currentStep)
            }
        }
    }

    // MARK: - Navigation Footer

    private var navigationFooter: some View {
        HStack {
            if currentStep > 0 {
                Button("Back") {
                    withAnimation(.spring(response: 0.3, dampingFraction: 0.8)) {
                        currentStep -= 1
                    }
                }
                .buttonStyle(.bordered)
            }

            Spacer()

            if currentStep < steps.count - 1 {
                Button("Skip All") {
                    completeOnboarding()
                }
                .buttonStyle(.plain)
                .foregroundStyle(.secondary)
            }
        }
    }

    // MARK: - Navigation Actions

    private func advanceStep() {
        withAnimation(.spring(response: 0.3, dampingFraction: 0.8)) {
            if currentStep < steps.count - 1 {
                currentStep += 1
            } else {
                completeOnboarding()
            }
        }
    }

    private func skipStep() {
        advanceStep()
    }

    private func completeOnboarding() {
        OnboardingController.shared.complete()
    }
}

// MARK: - Onboarding Step View

/// Individual onboarding step view
struct OnboardingStepView: View {
    let step: OnboardingStep
    let onContinue: () -> Void
    let onSkip: () -> Void

    @State private var isProcessing = false
    @State private var isComplete = false

    var body: some View {
        ScrollView {
            VStack(spacing: 24) {
                Spacer(minLength: 20)

                // Icon with animation
                Image(systemName: step.icon)
                    .font(.system(size: 64))
                    .foregroundStyle(.accent)
                    .symbolEffect(.pulse, options: .repeating, isActive: isProcessing)

                // Title and subtitle
                VStack(spacing: 12) {
                    Text(step.title)
                        .font(.largeTitle.weight(.semibold))

                    Text(step.subtitle)
                        .font(.body)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                        .frame(maxWidth: 400)
                }

                Spacer(minLength: 20)

                // Action area
                actionView
                    .frame(maxWidth: 480)

                Spacer(minLength: 20)
            }
            .frame(maxWidth: .infinity)
            .padding(.horizontal, 40)
            .padding(.vertical, 32)
        }
    }

    @ViewBuilder
    private var actionView: some View {
        switch step.action {
        case .requestPermission(let permissionType):
            PermissionRequestView(
                permissionType: permissionType,
                onGranted: {
                    isComplete = true
                    onContinue()
                },
                onSkip: onSkip
            )

        case .configureGateway:
            GatewaySetupView(onComplete: onContinue)

        case .enableVoiceWake:
            VoiceWakeSetupView(onComplete: onContinue, onSkip: onSkip)

        case .completeSetup:
            CompletionView(onComplete: onContinue)

        case .none:
            WelcomeActionView(onContinue: onContinue)
        }
    }
}

// MARK: - Welcome Action View

struct WelcomeActionView: View {
    let onContinue: () -> Void

    var body: some View {
        VStack(spacing: 16) {
            OnboardingCard {
                HStack(alignment: .top, spacing: 12) {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .font(.title3)
                        .foregroundStyle(.orange)

                    VStack(alignment: .leading, spacing: 6) {
                        Text("Security Notice")
                            .font(.headline)

                        Text(
                            "Nexus can perform powerful actions on your Mac, including running commands, reading/writing files, and capturing screenshots. Only enable features you understand and trust."
                        )
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                    }
                }
            }

            Button("Continue") {
                onContinue()
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
        }
    }
}

// MARK: - Permission Request View

/// Permission request component
struct PermissionRequestView: View {
    let permissionType: PermissionType
    let onGranted: () -> Void
    let onSkip: () -> Void

    @State private var permissions = PermissionManager.shared
    @State private var hasCheckedInitially = false

    private var isGranted: Bool {
        permissions.status(for: permissionType)
    }

    private var permissionTitle: String {
        switch permissionType {
        case .microphone: return "Microphone"
        case .speechRecognition: return "Speech Recognition"
        case .accessibility: return "Accessibility"
        case .screenRecording: return "Screen Recording"
        case .camera: return "Camera"
        }
    }

    private var permissionDescription: String {
        switch permissionType {
        case .microphone:
            return "Required for voice commands and wake word detection"
        case .speechRecognition:
            return "Required for voice-to-text transcription"
        case .accessibility:
            return "Required to control other applications and perform automation"
        case .screenRecording:
            return "Required to capture screen content for AI context"
        case .camera:
            return "Required for visual context in AI interactions"
        }
    }

    var body: some View {
        OnboardingCard {
            VStack(spacing: 16) {
                // Permission info
                HStack(alignment: .top, spacing: 12) {
                    Image(systemName: iconForPermission)
                        .font(.title2)
                        .foregroundStyle(isGranted ? .green : .secondary)
                        .frame(width: 32)

                    VStack(alignment: .leading, spacing: 4) {
                        Text(permissionTitle)
                            .font(.headline)
                        Text(permissionDescription)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }

                    Spacer()

                    if isGranted {
                        Image(systemName: "checkmark.circle.fill")
                            .foregroundStyle(.green)
                            .font(.title2)
                    }
                }

                Divider()

                // Action buttons
                if isGranted {
                    HStack {
                        Image(systemName: "checkmark.circle.fill")
                            .foregroundStyle(.green)
                        Text("Permission granted")
                            .foregroundStyle(.secondary)
                    }

                    Button("Continue") {
                        onGranted()
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.large)
                } else {
                    VStack(spacing: 12) {
                        Button("Grant Permission") {
                            Task {
                                await requestPermission()
                            }
                        }
                        .buttonStyle(.borderedProminent)
                        .controlSize(.large)

                        Button("Skip for now") {
                            onSkip()
                        }
                        .buttonStyle(.plain)
                        .foregroundStyle(.secondary)
                    }
                }
            }
        }
        .task {
            // Check permission status on appear
            if !hasCheckedInitially {
                hasCheckedInitially = true
                permissions.refreshAllStatuses()
                if isGranted {
                    // Auto-advance if already granted
                    try? await Task.sleep(for: .milliseconds(500))
                    if isGranted {
                        onGranted()
                    }
                }
            }
        }
    }

    private var iconForPermission: String {
        switch permissionType {
        case .microphone: return "mic"
        case .speechRecognition: return "waveform"
        case .accessibility: return "accessibility"
        case .screenRecording: return "rectangle.dashed.badge.record"
        case .camera: return "camera"
        }
    }

    private func requestPermission() async {
        switch permissionType {
        case .microphone:
            let granted = await permissions.requestMicrophoneAccess()
            if granted { onGranted() }
        case .speechRecognition:
            let granted = await permissions.requestSpeechRecognitionAccess()
            if granted { onGranted() }
        case .camera:
            let granted = await permissions.requestCameraAccess()
            if granted { onGranted() }
        case .accessibility:
            permissions.promptAccessibilityPermission()
            // User needs to manually grant in System Settings
        case .screenRecording:
            permissions.openSystemSettings(for: .screenRecording)
            // User needs to manually grant in System Settings
        }
    }
}

// MARK: - Gateway Setup View

/// Gateway setup component
struct GatewaySetupView: View {
    let onComplete: () -> Void

    @State private var appState = AppStateStore.shared
    @State private var isConnecting = false
    @State private var connectionError: String?
    @State private var isConnected = false

    var body: some View {
        OnboardingCard {
            VStack(alignment: .leading, spacing: 16) {
                // Local mode option
                ConnectionModeButton(
                    title: "This Mac (Local)",
                    subtitle: "Run the gateway locally on this Mac",
                    icon: "desktopcomputer",
                    isSelected: appState.connectionMode == .local
                ) {
                    appState.connectionMode = .local
                    connectionError = nil
                }

                Divider()

                // Remote mode option
                ConnectionModeButton(
                    title: "Remote Server",
                    subtitle: "Connect to a gateway running elsewhere",
                    icon: "server.rack",
                    isSelected: appState.connectionMode == .remote
                ) {
                    appState.connectionMode = .remote
                    connectionError = nil
                }

                // Remote host configuration
                if appState.connectionMode == .remote {
                    VStack(alignment: .leading, spacing: 8) {
                        TextField(
                            "Host",
                            text: Binding(
                                get: { appState.remoteHost ?? "" },
                                set: { appState.remoteHost = $0.isEmpty ? nil : $0 }
                            )
                        )
                        .textFieldStyle(.roundedBorder)

                        TextField("User", text: $appState.remoteUser)
                            .textFieldStyle(.roundedBorder)

                        TextField(
                            "Identity File (optional)",
                            text: Binding(
                                get: { appState.remoteIdentityFile ?? "" },
                                set: { appState.remoteIdentityFile = $0.isEmpty ? nil : $0 }
                            )
                        )
                        .textFieldStyle(.roundedBorder)
                    }
                    .padding(.leading, 32)
                    .padding(.top, 4)
                }

                Divider()

                // Skip option
                ConnectionModeButton(
                    title: "Configure Later",
                    subtitle: "Skip gateway setup for now",
                    icon: "clock",
                    isSelected: appState.connectionMode == .unconfigured
                ) {
                    appState.connectionMode = .unconfigured
                    connectionError = nil
                }

                // Error display
                if let error = connectionError {
                    HStack {
                        Image(systemName: "exclamationmark.triangle.fill")
                            .foregroundStyle(.red)
                        Text(error)
                            .foregroundStyle(.red)
                            .font(.caption)
                    }
                }

                // Connection status
                if isConnected {
                    HStack {
                        Image(systemName: "checkmark.circle.fill")
                            .foregroundStyle(.green)
                        Text("Connected successfully")
                            .foregroundStyle(.green)
                    }
                }

                Divider()

                // Action buttons
                HStack {
                    if appState.connectionMode != .unconfigured {
                        Button(isConnecting ? "Connecting..." : "Test Connection") {
                            testConnection()
                        }
                        .buttonStyle(.bordered)
                        .disabled(isConnecting)
                    }

                    Spacer()

                    Button("Continue") {
                        onComplete()
                    }
                    .buttonStyle(.borderedProminent)
                }
            }
        }
    }

    private func testConnection() {
        isConnecting = true
        connectionError = nil
        isConnected = false

        Task {
            do {
                try await GatewayConnection.shared.refresh()
                let healthy = try await GatewayConnection.shared.healthOK()
                if healthy {
                    isConnected = true
                } else {
                    connectionError = "Gateway health check failed"
                }
            } catch {
                connectionError = error.localizedDescription
            }
            isConnecting = false
        }
    }
}

// MARK: - Voice Wake Setup View

/// Voice wake setup component
struct VoiceWakeSetupView: View {
    let onComplete: () -> Void
    let onSkip: () -> Void

    @State private var appState = AppStateStore.shared
    @State private var permissions = PermissionManager.shared
    @State private var isEnabled = false
    @State private var isTestingVoice = false
    @State private var testStatus: String?

    private var canEnableVoiceWake: Bool {
        permissions.voiceWakePermissionsGranted
    }

    var body: some View {
        OnboardingCard {
            VStack(spacing: 16) {
                // Voice wake toggle
                HStack {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Enable Voice Wake")
                            .font(.headline)
                        Text("Say \"Hey Nexus\" to activate")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }

                    Spacer()

                    Toggle("", isOn: $isEnabled)
                        .toggleStyle(.switch)
                        .disabled(!canEnableVoiceWake)
                }

                // Permission warning
                if !canEnableVoiceWake {
                    HStack(alignment: .top, spacing: 8) {
                        Image(systemName: "exclamationmark.triangle.fill")
                            .foregroundStyle(.orange)
                        Text(
                            "Microphone and Speech Recognition permissions are required for Voice Wake"
                        )
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    }
                    .padding(8)
                    .background(Color.orange.opacity(0.1))
                    .cornerRadius(8)
                }

                // Test voice wake
                if isEnabled {
                    Divider()

                    VStack(spacing: 12) {
                        Text("Try saying \"Hey Nexus\"")
                            .font(.subheadline)
                            .foregroundStyle(.secondary)

                        if isTestingVoice {
                            HStack(spacing: 8) {
                                ProgressView()
                                    .controlSize(.small)
                                Text("Listening...")
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                        }

                        if let status = testStatus {
                            Text(status)
                                .font(.caption)
                                .foregroundStyle(status.contains("detected") ? .green : .secondary)
                        }
                    }
                }

                Divider()

                // Action buttons
                HStack {
                    Button("Skip") {
                        onSkip()
                    }
                    .buttonStyle(.bordered)

                    Spacer()

                    Button("Continue") {
                        if isEnabled {
                            Task {
                                await appState.setVoiceWakeEnabled(true)
                            }
                        }
                        onComplete()
                    }
                    .buttonStyle(.borderedProminent)
                }
            }
        }
        .onChange(of: isEnabled) { _, enabled in
            if enabled {
                startVoiceTest()
            } else {
                stopVoiceTest()
            }
        }
    }

    private func startVoiceTest() {
        isTestingVoice = true
        testStatus = nil

        Task {
            let config = VoiceWakeRuntime.RuntimeConfig(
                triggers: appState.voiceWakeTriggers,
                micID: appState.voiceWakeMicID.isEmpty ? appState.selectedMicrophone : appState.voiceWakeMicID,
                localeID: appState.voiceWakeLocaleID.isEmpty ? nil : appState.voiceWakeLocaleID,
                triggerChime: .subtle,
                sendChime: .none
            )
            await VoiceWakeRuntime.shared.start(with: config)

            // Test for a few seconds
            try? await Task.sleep(for: .seconds(5))

            if isTestingVoice {
                await VoiceWakeRuntime.shared.stop()
                isTestingVoice = false
                testStatus = "Test complete"
            }
        }
    }

    private func stopVoiceTest() {
        isTestingVoice = false
        testStatus = nil
        Task {
            await VoiceWakeRuntime.shared.stop()
        }
    }
}

// MARK: - Completion View

/// Completion step view
struct CompletionView: View {
    let onComplete: () -> Void

    @State private var appState = AppStateStore.shared

    var body: some View {
        VStack(spacing: 20) {
            OnboardingCard {
                VStack(alignment: .leading, spacing: 16) {
                    FeatureRow(
                        icon: "message",
                        title: "Open Chat",
                        description: "Click the menu bar icon to start chatting"
                    )

                    Divider()

                    FeatureRow(
                        icon: "mic",
                        title: "Voice Wake",
                        description: "Say \"Hey Nexus\" to activate hands-free"
                    )

                    Divider()

                    FeatureRow(
                        icon: "gear",
                        title: "Settings",
                        description: "Customize Nexus in Settings"
                    )

                    Divider()

                    Toggle("Launch Nexus at Login", isOn: $appState.launchAtLogin)
                }
            }

            Button("Get Started") {
                onComplete()
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
        }
    }
}

// MARK: - Supporting Views

struct OnboardingCard<Content: View>: View {
    @ViewBuilder let content: Content

    var body: some View {
        content
            .padding(16)
            .background(
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .fill(Color(NSColor.controlBackgroundColor))
            )
            .overlay(
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .strokeBorder(Color.secondary.opacity(0.2), lineWidth: 1)
            )
    }
}

struct ConnectionModeButton: View {
    let title: String
    let subtitle: String
    let icon: String
    let isSelected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 12) {
                Image(systemName: icon)
                    .font(.title2)
                    .foregroundStyle(isSelected ? Color.accentColor : .secondary)
                    .frame(width: 32)

                VStack(alignment: .leading, spacing: 2) {
                    Text(title)
                        .font(.headline)
                    Text(subtitle)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                Spacer()

                if isSelected {
                    Image(systemName: "checkmark.circle.fill")
                        .foregroundStyle(.accent)
                }
            }
            .padding(12)
            .background(
                RoundedRectangle(cornerRadius: 8, style: .continuous)
                    .fill(isSelected ? Color.accentColor.opacity(0.1) : Color.clear)
            )
            .overlay(
                RoundedRectangle(cornerRadius: 8, style: .continuous)
                    .strokeBorder(isSelected ? Color.accentColor.opacity(0.4) : Color.clear, lineWidth: 1)
            )
        }
        .buttonStyle(.plain)
    }
}

struct FeatureRow: View {
    let icon: String
    let title: String
    let description: String

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: icon)
                .font(.title2)
                .foregroundStyle(.accent)
                .frame(width: 32)

            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.headline)
                Text(description)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }
}

// MARK: - Preview

#Preview {
    OnboardingView()
}
