import SwiftUI

/// Main onboarding view with multi-page wizard.
struct OnboardingView: View {
    @State private var currentPage = 0
    @State private var appState = AppStateStore.shared
    @State private var permissions = PermissionManager.shared

    static let windowWidth: CGFloat = 600
    static let windowHeight: CGFloat = 500

    private let totalPages = 4

    var body: some View {
        VStack(spacing: 0) {
            // Page content
            TabView(selection: $currentPage) {
                welcomePage
                    .tag(0)

                connectionPage
                    .tag(1)

                permissionsPage
                    .tag(2)

                readyPage
                    .tag(3)
            }
            .tabViewStyle(.automatic)
            .frame(maxWidth: .infinity, maxHeight: .infinity)

            Divider()

            // Navigation footer
            navigationFooter
                .padding(.horizontal, 24)
                .padding(.vertical, 16)
        }
        .frame(width: Self.windowWidth, height: Self.windowHeight)
        .background(Color(NSColor.windowBackgroundColor))
    }

    // MARK: - Navigation Footer

    private var navigationFooter: some View {
        HStack {
            // Page indicators
            HStack(spacing: 8) {
                ForEach(0..<totalPages, id: \.self) { index in
                    Circle()
                        .fill(index == currentPage ? Color.accentColor : Color.secondary.opacity(0.3))
                        .frame(width: 8, height: 8)
                }
            }

            Spacer()

            // Navigation buttons
            HStack(spacing: 12) {
                if currentPage > 0 {
                    Button("Back") {
                        withAnimation {
                            currentPage -= 1
                        }
                    }
                    .buttonStyle(.bordered)
                }

                Button(currentPage == totalPages - 1 ? "Get Started" : "Next") {
                    if currentPage == totalPages - 1 {
                        OnboardingController.shared.complete()
                    } else {
                        withAnimation {
                            currentPage += 1
                        }
                    }
                }
                .buttonStyle(.borderedProminent)
            }
        }
    }

    // MARK: - Welcome Page

    private var welcomePage: some View {
        OnboardingPageView {
            VStack(spacing: 24) {
                Image(systemName: "circle.hexagongrid.fill")
                    .font(.system(size: 72))
                    .foregroundStyle(.accent)

                VStack(spacing: 12) {
                    Text("Welcome to Nexus")
                        .font(.largeTitle.weight(.semibold))

                    Text("Nexus is your AI-powered personal assistant for macOS.")
                        .font(.body)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                        .frame(maxWidth: 400)
                }

                // Security notice
                OnboardingCard {
                    HStack(alignment: .top, spacing: 12) {
                        Image(systemName: "exclamationmark.triangle.fill")
                            .font(.title3)
                            .foregroundStyle(.orange)

                        VStack(alignment: .leading, spacing: 6) {
                            Text("Security Notice")
                                .font(.headline)

                            Text("Nexus can perform powerful actions on your Mac, including running commands, reading/writing files, and capturing screenshots. Only enable features you understand and trust.")
                                .font(.subheadline)
                                .foregroundStyle(.secondary)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                    }
                }
                .frame(maxWidth: 480)
            }
        }
    }

    // MARK: - Connection Page

    private var connectionPage: some View {
        OnboardingPageView {
            VStack(spacing: 24) {
                VStack(spacing: 12) {
                    Text("Choose Your Gateway")
                        .font(.largeTitle.weight(.semibold))

                    Text("Nexus uses a gateway to process AI requests. Choose how to connect.")
                        .font(.body)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                        .frame(maxWidth: 400)
                }

                OnboardingCard {
                    VStack(alignment: .leading, spacing: 12) {
                        ConnectionModeButton(
                            title: "This Mac (Local)",
                            subtitle: "Run the gateway locally on this Mac",
                            icon: "desktopcomputer",
                            isSelected: appState.connectionMode == .local
                        ) {
                            appState.connectionMode = .local
                        }

                        Divider()

                        ConnectionModeButton(
                            title: "Remote Server",
                            subtitle: "Connect to a gateway running elsewhere",
                            icon: "server.rack",
                            isSelected: appState.connectionMode == .remote
                        ) {
                            appState.connectionMode = .remote
                        }

                        if appState.connectionMode == .remote {
                            VStack(alignment: .leading, spacing: 8) {
                                TextField("Host", text: Binding(
                                    get: { appState.remoteHost ?? "" },
                                    set: { appState.remoteHost = $0.isEmpty ? nil : $0 }
                                ))
                                .textFieldStyle(.roundedBorder)

                                TextField("User", text: $appState.remoteUser)
                                    .textFieldStyle(.roundedBorder)
                            }
                            .padding(.leading, 32)
                            .padding(.top, 4)
                        }

                        Divider()

                        ConnectionModeButton(
                            title: "Configure Later",
                            subtitle: "Skip gateway setup for now",
                            icon: "clock",
                            isSelected: appState.connectionMode == .unconfigured
                        ) {
                            appState.connectionMode = .unconfigured
                        }
                    }
                }
                .frame(maxWidth: 480)
            }
        }
    }

    // MARK: - Permissions Page

    private var permissionsPage: some View {
        OnboardingPageView {
            VStack(spacing: 24) {
                VStack(spacing: 12) {
                    Text("Grant Permissions")
                        .font(.largeTitle.weight(.semibold))

                    Text("These permissions let Nexus automate apps and capture context.")
                        .font(.body)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                        .frame(maxWidth: 400)
                }

                OnboardingCard {
                    VStack(spacing: 12) {
                        OnboardingPermissionRow(
                            type: .accessibility,
                            title: "Accessibility",
                            description: "Control other applications",
                            permissions: permissions
                        )

                        Divider()

                        OnboardingPermissionRow(
                            type: .screenRecording,
                            title: "Screen Recording",
                            description: "Capture screen content for AI context",
                            permissions: permissions
                        )

                        Divider()

                        OnboardingPermissionRow(
                            type: .microphone,
                            title: "Microphone",
                            description: "Voice input and wake words",
                            permissions: permissions
                        )

                        Divider()

                        OnboardingPermissionRow(
                            type: .camera,
                            title: "Camera",
                            description: "Visual context for AI agents",
                            permissions: permissions
                        )
                    }
                }
                .frame(maxWidth: 480)

                Button("Refresh Status") {
                    permissions.refreshAllStatuses()
                }
                .buttonStyle(.bordered)
            }
        }
    }

    // MARK: - Ready Page

    private var readyPage: some View {
        OnboardingPageView {
            VStack(spacing: 24) {
                Image(systemName: "checkmark.circle.fill")
                    .font(.system(size: 72))
                    .foregroundStyle(.green)

                VStack(spacing: 12) {
                    Text("All Set!")
                        .font(.largeTitle.weight(.semibold))

                    Text("Nexus is ready to use. Here's what you can do next:")
                        .font(.body)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                        .frame(maxWidth: 400)
                }

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
                .frame(maxWidth: 480)
            }
        }
    }
}

// MARK: - Supporting Views

struct OnboardingPageView<Content: View>: View {
    @ViewBuilder let content: Content

    var body: some View {
        ScrollView {
            content
                .frame(maxWidth: .infinity)
                .padding(.horizontal, 40)
                .padding(.vertical, 32)
        }
    }
}

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

struct OnboardingPermissionRow: View {
    let type: PermissionType
    let title: String
    let description: String
    @Bindable var permissions: PermissionManager

    private var isGranted: Bool {
        permissions.status(for: type)
    }

    var body: some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.headline)
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

#Preview {
    OnboardingView()
}
