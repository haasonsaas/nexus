import AVFoundation
import Speech
import SwiftUI

/// Voice Wake settings view for the settings panel.
struct VoiceWakeSettingsView: View {
    @Bindable var appState: AppStateStore
    @State private var permissions = PermissionManager.shared
    @State private var micMonitor = MicLevelMonitor.shared
    @State private var audioDevices = AudioInputDeviceObserver.shared
    @State private var testState: VoiceWakeTestState = .idle
    @State private var isTesting = false
    @State private var meterLevel: Double = 0

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                // Enable toggle
                enableSection

                // Permissions status
                permissionsSection

                // Microphone selection
                microphoneSection

                // Live level meter
                levelMeterSection

                // Trigger words
                triggerWordsSection

                // Test section
                testSection
            }
            .padding()
        }
        .task {
            await startMonitoring()
        }
        .onDisappear {
            stopMonitoring()
        }
    }

    // MARK: - Enable Section

    private var enableSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Toggle("Enable Voice Wake", isOn: $appState.voiceWakeEnabled)
                .toggleStyle(.switch)

            Text("Listen for wake words to activate hands-free voice commands. Speech recognition runs on-device.")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }

    // MARK: - Permissions Section

    private var permissionsSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Permissions")
                .font(.headline)

            HStack(spacing: 12) {
                PermissionBadge(
                    title: "Microphone",
                    isGranted: permissions.microphoneGranted
                ) {
                    Task { await permissions.requestMicrophoneAccess() }
                }

                PermissionBadge(
                    title: "Speech",
                    isGranted: permissions.speechRecognitionGranted
                ) {
                    Task { await permissions.requestSpeechRecognitionAccess() }
                }
            }

            if !permissions.voiceWakePermissionsGranted {
                Text("Grant both permissions to use Voice Wake.")
                    .font(.caption)
                    .foregroundStyle(.orange)
            }
        }
    }

    // MARK: - Microphone Section

    private var microphoneSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Microphone")
                .font(.headline)

            Picker("Input Device", selection: $appState.selectedMicrophone.toUnwrapped(defaultValue: "")) {
                Text("System Default").tag("")
                ForEach(audioDevices.devices, id: \.uid) { device in
                    Text(device.name).tag(device.uid)
                }
            }
            .pickerStyle(.menu)
        }
    }

    // MARK: - Level Meter Section

    private var levelMeterSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text("Input Level")
                    .font(.headline)
                Spacer()
                Text(String(format: "%.0f dB", (meterLevel * 50) - 50))
                    .font(.caption.monospacedDigit())
                    .foregroundStyle(.secondary)
            }

            MicLevelBar(level: meterLevel)
                .frame(height: 8)
        }
    }

    // MARK: - Trigger Words Section

    private var triggerWordsSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text("Trigger Words")
                    .font(.headline)
                Spacer()
                Button("Reset Defaults") {
                    appState.voiceWakeTriggers = ["hey nexus"]
                }
                .buttonStyle(.link)
                .controlSize(.small)
            }

            VStack(spacing: 4) {
                ForEach(Array(appState.voiceWakeTriggers.enumerated()), id: \.offset) { index, trigger in
                    HStack {
                        TextField("Wake word", text: Binding(
                            get: { trigger },
                            set: { appState.voiceWakeTriggers[index] = $0 }
                        ))
                        .textFieldStyle(.roundedBorder)

                        Button {
                            appState.voiceWakeTriggers.remove(at: index)
                        } label: {
                            Image(systemName: "trash")
                        }
                        .buttonStyle(.borderless)
                        .disabled(appState.voiceWakeTriggers.count <= 1)
                    }
                }
            }

            Button {
                appState.voiceWakeTriggers.append("")
            } label: {
                Label("Add Word", systemImage: "plus")
            }
            .buttonStyle(.bordered)
            .controlSize(.small)

            Text("Nexus listens for these words to activate. Keep them short and distinct.")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }

    // MARK: - Test Section

    private var testSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Test Voice Wake")
                .font(.headline)

            HStack(spacing: 12) {
                Button(isTesting ? "Stop Test" : "Start Test") {
                    toggleTest()
                }
                .buttonStyle(.borderedProminent)
                .disabled(!permissions.voiceWakePermissionsGranted)

                testStatusView
            }

            if case .hearing(let text) = testState {
                Text("Hearing: \(text)")
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }
        }
        .padding()
        .background(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .fill(Color(NSColor.controlBackgroundColor))
        )
    }

    @ViewBuilder
    private var testStatusView: some View {
        switch testState {
        case .idle:
            Text("Ready to test")
                .foregroundStyle(.secondary)
        case .requesting:
            ProgressView()
                .controlSize(.small)
        case .listening:
            HStack(spacing: 4) {
                Circle()
                    .fill(.green)
                    .frame(width: 8, height: 8)
                Text("Listening...")
            }
        case .hearing:
            HStack(spacing: 4) {
                Circle()
                    .fill(.blue)
                    .frame(width: 8, height: 8)
                Text("Transcribing...")
            }
        case .detected(let command):
            HStack(spacing: 4) {
                Image(systemName: "checkmark.circle.fill")
                    .foregroundStyle(.green)
                Text("Detected: \(command)")
            }
        case .failed(let error):
            HStack(spacing: 4) {
                Image(systemName: "xmark.circle.fill")
                    .foregroundStyle(.red)
                Text(error)
            }
        case .finalizing:
            Text("Finalizing...")
                .foregroundStyle(.secondary)
        }
    }

    // MARK: - Actions

    private func startMonitoring() async {
        audioDevices.start()
        do {
            try await micMonitor.start { level in
                Task { @MainActor in
                    meterLevel = level
                }
            }
        } catch {
            // Ignore meter errors
        }
    }

    private func stopMonitoring() {
        audioDevices.stop()
        Task {
            await micMonitor.stop()
        }
    }

    private func toggleTest() {
        if isTesting {
            VoiceWakeRuntime.shared.stopListening()
            isTesting = false
            testState = .idle
        } else {
            isTesting = true
            testState = .requesting

            Task {
                do {
                    testState = .listening
                    // Start voice wake in test mode
                    VoiceWakeRuntime.shared.startListening()

                    // Simulate listening for 10 seconds
                    try await Task.sleep(for: .seconds(10))

                    if isTesting {
                        testState = .failed("No trigger detected")
                        isTesting = false
                    }
                } catch {
                    testState = .failed(error.localizedDescription)
                    isTesting = false
                }
            }
        }
    }
}

// MARK: - Voice Wake Test State

enum VoiceWakeTestState: Equatable {
    case idle
    case requesting
    case listening
    case hearing(String)
    case detected(String)
    case failed(String)
    case finalizing
}

// MARK: - Supporting Views

struct PermissionBadge: View {
    let title: String
    let isGranted: Bool
    let onRequest: () -> Void

    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: isGranted ? "checkmark.circle.fill" : "xmark.circle.fill")
                .foregroundStyle(isGranted ? .green : .red)
            Text(title)
                .font(.caption)
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
        .background(
            Capsule()
                .fill(Color(NSColor.controlBackgroundColor))
        )
        .onTapGesture {
            if !isGranted {
                onRequest()
            }
        }
    }
}

struct MicLevelBar: View {
    let level: Double

    var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                RoundedRectangle(cornerRadius: 4)
                    .fill(Color.secondary.opacity(0.2))

                RoundedRectangle(cornerRadius: 4)
                    .fill(levelColor)
                    .frame(width: geo.size.width * max(0, min(1, level)))
                    .animation(.easeOut(duration: 0.1), value: level)
            }
        }
    }

    private var levelColor: Color {
        if level > 0.8 {
            return .red
        } else if level > 0.5 {
            return .orange
        } else {
            return .green
        }
    }
}

// MARK: - Optional Binding Extension

extension Binding where Value == String? {
    func toUnwrapped(defaultValue: String) -> Binding<String> {
        Binding<String>(
            get: { self.wrappedValue ?? defaultValue },
            set: { self.wrappedValue = $0.isEmpty ? nil : $0 }
        )
    }
}

#Preview {
    VoiceWakeSettingsView(appState: AppStateStore.shared)
        .frame(width: 500, height: 600)
}
