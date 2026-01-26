import AVFoundation
import Speech
import SwiftUI

/// Voice Wake settings view for the settings panel.
struct VoiceWakeSettingsView: View {
    @Bindable var appState: AppStateStore
    @State private var permissions = PermissionManager.shared
    @State private var micMonitor = MicLevelMonitor.shared
    @State private var audioDevices = AudioInputObserver.shared
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
                ForEach(audioDevices.availableDevices, id: \.uid) { device in
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

            VoiceWakeMicLevelBar(level: meterLevel)
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
        audioDevices.startObserving()
        audioDevices.refreshDevices()
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
        audioDevices.stopObserving()
        Task {
            await micMonitor.stop()
        }
    }

    private func toggleTest() {
        if isTesting {
            VoiceWakeOverlayRuntime.shared.stopListening()
            isTesting = false
            testState = .idle
        } else {
            isTesting = true
            testState = .requesting

            Task {
                do {
                    testState = .listening
                    // Start voice wake in test mode
                    VoiceWakeOverlayRuntime.shared.startListening()

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

struct VoiceWakeMicLevelBar: View {
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

// MARK: - Wake Word Config Section

/// Advanced configuration section for the WakeWordEngine.
/// Allows users to manage trigger words, confidence threshold, and silence timeout.
struct WakeWordConfigSection: View {
    @State private var engine = WakeWordEngine.shared
    @State private var newWord = ""
    @State private var showAdvanced = false

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            // Wake Words Management
            wakeWordsSection

            // Detection Settings
            detectionSettingsSection

            // Advanced Settings (collapsible)
            advancedSettingsSection

            // Engine Status
            engineStatusSection
        }
    }

    // MARK: - Wake Words Section

    private var wakeWordsSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text("Wake Words")
                    .font(.headline)
                Spacer()
                Button("Reset") {
                    engine.setTriggerWords(WakeWordConfig.default.triggerWords)
                }
                .buttonStyle(.link)
                .controlSize(.small)
            }

            VStack(spacing: 4) {
                ForEach(engine.config.triggerWords, id: \.self) { word in
                    HStack {
                        Text(word)
                            .font(.body)
                        Spacer()
                        Button {
                            engine.removeTriggerWord(word)
                        } label: {
                            Image(systemName: "minus.circle")
                                .foregroundStyle(.red)
                        }
                        .buttonStyle(.plain)
                        .disabled(engine.config.triggerWords.count <= 1)
                    }
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(
                        RoundedRectangle(cornerRadius: 6)
                            .fill(Color(NSColor.controlBackgroundColor))
                    )
                }
            }

            HStack {
                TextField("Add wake word", text: $newWord)
                    .textFieldStyle(.roundedBorder)
                    .onSubmit {
                        addWord()
                    }

                Button("Add") {
                    addWord()
                }
                .disabled(newWord.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }

            Text("Wake words trigger voice activation. Use distinct phrases to avoid false positives.")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }

    // MARK: - Detection Settings Section

    private var detectionSettingsSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Detection Settings")
                .font(.headline)

            // Confidence Threshold
            VStack(alignment: .leading, spacing: 4) {
                HStack {
                    Text("Confidence Threshold")
                        .font(.subheadline)
                    Spacer()
                    Text("\(engine.config.confidenceThreshold, specifier: "%.0f")%")
                        .font(.caption.monospacedDigit())
                        .foregroundStyle(.secondary)
                }

                Slider(
                    value: Binding(
                        get: { Double(engine.config.confidenceThreshold) },
                        set: { engine.config.confidenceThreshold = Float($0) }
                    ),
                    in: 0.5...1.0,
                    step: 0.05
                )

                Text("Higher values reduce false positives but may miss quieter speech.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            // Silence Timeout
            VStack(alignment: .leading, spacing: 4) {
                HStack {
                    Text("Silence Timeout")
                        .font(.subheadline)
                    Spacer()
                    Text("\(engine.config.silenceTimeout, specifier: "%.1f")s")
                        .font(.caption.monospacedDigit())
                        .foregroundStyle(.secondary)
                }

                Slider(
                    value: $engine.config.silenceTimeout,
                    in: 0.5...5.0,
                    step: 0.5
                )

                Text("Time to wait for speech to complete before processing.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }

    // MARK: - Advanced Settings Section

    private var advancedSettingsSection: some View {
        DisclosureGroup("Advanced Settings", isExpanded: $showAdvanced) {
            VStack(alignment: .leading, spacing: 12) {
                // Max Listen Duration
                VStack(alignment: .leading, spacing: 4) {
                    HStack {
                        Text("Max Listen Duration")
                            .font(.subheadline)
                        Spacer()
                        Text("\(Int(engine.config.maxListenDuration))s")
                            .font(.caption.monospacedDigit())
                            .foregroundStyle(.secondary)
                    }

                    Slider(
                        value: $engine.config.maxListenDuration,
                        in: 30...300,
                        step: 30
                    )

                    Text("Recognition restarts after this duration to maintain accuracy.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                // Noise Floor RMS
                VStack(alignment: .leading, spacing: 4) {
                    HStack {
                        Text("Noise Floor Threshold")
                            .font(.subheadline)
                        Spacer()
                        Text("\(engine.config.noiseFloorRMS, specifier: "%.3f")")
                            .font(.caption.monospacedDigit())
                            .foregroundStyle(.secondary)
                    }

                    Slider(
                        value: Binding(
                            get: { Double(engine.config.noiseFloorRMS) },
                            set: { engine.config.noiseFloorRMS = Float($0) }
                        ),
                        in: 0.005...0.1,
                        step: 0.005
                    )

                    Text("Audio level below this is considered silence. Increase in noisy environments.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .padding(.top, 8)
        }
    }

    // MARK: - Engine Status Section

    private var engineStatusSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Engine Status")
                .font(.headline)

            HStack(spacing: 16) {
                // State indicator
                HStack(spacing: 6) {
                    Circle()
                        .fill(stateColor)
                        .frame(width: 8, height: 8)
                    Text(engine.state.rawValue.capitalized)
                        .font(.caption)
                }

                // Enabled status
                HStack(spacing: 6) {
                    Image(systemName: engine.isEnabled ? "checkmark.circle.fill" : "xmark.circle.fill")
                        .foregroundStyle(engine.isEnabled ? .green : .secondary)
                    Text(engine.isEnabled ? "Enabled" : "Disabled")
                        .font(.caption)
                }

                Spacer()

                // Audio level
                if engine.isEnabled {
                    HStack(spacing: 4) {
                        Image(systemName: "waveform")
                            .font(.caption)
                        Text("\(Int(engine.audioLevel * 100))%")
                            .font(.caption.monospacedDigit())
                    }
                    .foregroundStyle(.secondary)
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            .background(
                RoundedRectangle(cornerRadius: 8)
                    .fill(Color(NSColor.controlBackgroundColor))
            )

            // Last detection info
            if let lastWord = engine.lastDetectedWord {
                HStack {
                    Image(systemName: "checkmark.circle.fill")
                        .foregroundStyle(.green)
                    Text("Last: \"\(lastWord)\" (\(Int(engine.lastDetectionConfidence * 100))%)")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    // MARK: - Helpers

    private var stateColor: Color {
        switch engine.state {
        case .idle:
            return .secondary
        case .listening:
            return .green
        case .hearing:
            return .blue
        case .finalizing:
            return .orange
        case .detected:
            return .green
        case .failed:
            return .red
        }
    }

    private func addWord() {
        let trimmed = newWord.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        engine.addTriggerWord(trimmed)
        newWord = ""
    }
}

// MARK: - Wake Word Config Section Preview

#Preview("WakeWordConfigSection") {
    WakeWordConfigSection()
        .padding()
        .frame(width: 400)
}

#Preview {
    VoiceWakeSettingsView(appState: AppStateStore.shared)
        .frame(width: 500, height: 600)
}
