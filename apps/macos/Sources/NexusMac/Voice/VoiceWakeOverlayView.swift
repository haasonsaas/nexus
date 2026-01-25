import SwiftUI

/// Voice wake overlay showing transcript and send button
struct VoiceWakeOverlayView: View {
    @Bindable var controller: VoiceWakeOverlayController
    @FocusState private var textFocused: Bool
    @State private var isHovering = false
    @State private var closeHovering = false

    var body: some View {
        ZStack(alignment: .topLeading) {
            // Main content
            VStack(spacing: 12) {
                // Transcript area
                if controller.model.isEditing {
                    editableTranscript
                } else {
                    displayTranscript
                }

                // Bottom bar with controls
                HStack(spacing: 16) {
                    // Audio level indicator
                    AudioLevelIndicator(level: Float(controller.model.level))
                        .frame(width: 24, height: 24)

                    Spacer()

                    // Edit button
                    Button {
                        controller.userBeganEditing()
                    } label: {
                        Image(systemName: "pencil")
                            .font(.system(size: 14))
                    }
                    .buttonStyle(.plain)
                    .disabled(controller.model.text.isEmpty)

                    // Send button
                    sendButton
                }
            }
            .padding()
            .frame(width: 360)
            .background {
                overlayBackground
            }
            .shadow(color: Color.black.opacity(0.3), radius: 10, y: 5)
            .onHover { isHovering = $0 }

            // Close button
            closeButton
        }
        .padding(10)
        .onAppear {
            updateFocusState()
        }
        .onChange(of: controller.model.isVisible) { _, _ in
            updateFocusState()
        }
        .onChange(of: controller.model.isEditing) { _, _ in
            updateFocusState()
        }
    }

    // MARK: - Transcript Views

    private var displayTranscript: some View {
        VStack(alignment: .leading, spacing: 8) {
            // Status indicator
            HStack {
                Circle()
                    .fill(controller.model.isListening ? Color.green : Color.orange)
                    .frame(width: 8, height: 8)

                Text(controller.model.isListening ? "Listening" : "Processing")
                    .font(.caption)
                    .foregroundStyle(.secondary)

                Spacer()
            }

            // Transcript text
            if controller.model.text.isEmpty {
                Text("Say something...")
                    .font(.title3)
                    .foregroundStyle(.tertiary)
                    .italic()
            } else {
                Text(controller.model.text)
                    .font(.title3)
                    .lineLimit(5)
                    .textSelection(.enabled)
            }
        }
        .frame(minHeight: 80, alignment: .topLeading)
        .frame(maxWidth: .infinity, alignment: .leading)
        .onTapGesture(count: 2) {
            if !controller.model.text.isEmpty {
                controller.userBeganEditing()
                textFocused = true
            }
        }
    }

    private var editableTranscript: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text("Edit Transcript")
                    .font(.caption)
                    .foregroundStyle(.secondary)

                Spacer()

                Button("Cancel") {
                    controller.endEditing()
                }
                .buttonStyle(.plain)
                .font(.caption)
            }

            TextEditor(text: Binding(
                get: { controller.model.text },
                set: { controller.updateText($0) }
            ))
            .font(.title3)
            .frame(minHeight: 60)
            .scrollContentBackground(.hidden)
            .background(Color.secondary.opacity(0.1))
            .clipShape(RoundedRectangle(cornerRadius: 8))
            .focused($textFocused)
            .onSubmit {
                controller.requestSend()
            }
        }
        .frame(minHeight: 80, alignment: .topLeading)
    }

    // MARK: - Send Button

    @ViewBuilder
    private var sendButton: some View {
        Button {
            controller.requestSend()
        } label: {
            HStack(spacing: 6) {
                Text(controller.model.isFinal ? "Send" : "Listening...")
                    .font(.subheadline.weight(.medium))

                if controller.model.isFinal {
                    Image(systemName: "arrow.up.circle.fill")
                        .font(.system(size: 18))
                }
            }
            .foregroundStyle(controller.model.isFinal ? .white : .secondary)
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
            .background(
                RoundedRectangle(cornerRadius: 16)
                    .fill(controller.model.isFinal ? Color.accentColor : Color.secondary.opacity(0.2))
            )
        }
        .buttonStyle(.plain)
        .disabled(!controller.model.forwardEnabled || controller.model.isSending)
        .keyboardShortcut(.return, modifiers: [])
    }

    // MARK: - Background

    @ViewBuilder
    private var overlayBackground: some View {
        let shape = RoundedRectangle(cornerRadius: 16, style: .continuous)

        VisualEffectView(material: .hudWindow, blendingMode: .behindWindow)
            .clipShape(shape)
            .overlay(
                shape.strokeBorder(Color.white.opacity(0.16), lineWidth: 1)
            )
    }

    // MARK: - Close Button

    @ViewBuilder
    private var closeButton: some View {
        let showClose = controller.model.isEditing || isHovering || closeHovering

        if showClose {
            Button {
                controller.cancelEditingAndDismiss()
            } label: {
                Image(systemName: "xmark")
                    .font(.system(size: 12, weight: .bold))
                    .foregroundColor(Color.white.opacity(0.9))
                    .frame(width: 22, height: 22)
                    .background(Color.black.opacity(0.4))
                    .clipShape(Circle())
                    .shadow(color: Color.black.opacity(0.45), radius: 10, x: 0, y: 3)
            }
            .buttonStyle(.plain)
            .focusable(false)
            .contentShape(Circle())
            .padding(6)
            .onHover { closeHovering = $0 }
            .offset(x: -9, y: -9)
            .transition(.opacity)
        }
    }

    // MARK: - Focus

    private func updateFocusState() {
        let shouldFocus = controller.model.isVisible && controller.model.isEditing
        if textFocused != shouldFocus {
            textFocused = shouldFocus
        }
    }
}

// MARK: - Audio Level Circular Indicator

/// Audio level circular indicator with microphone icon
struct AudioLevelIndicator: View {
    let level: Float

    var body: some View {
        ZStack {
            // Background ring
            Circle()
                .stroke(Color.secondary.opacity(0.3), lineWidth: 3)

            // Level ring
            Circle()
                .trim(from: 0, to: CGFloat(level))
                .stroke(levelColor, style: StrokeStyle(lineWidth: 3, lineCap: .round))
                .rotationEffect(.degrees(-90))
                .animation(.easeOut(duration: 0.08), value: level)

            // Mic icon
            Image(systemName: "mic.fill")
                .font(.system(size: 10))
                .foregroundStyle(level > 0.1 ? levelColor : .secondary)
        }
    }

    private var levelColor: Color {
        if level > 0.7 { return .red }
        if level > 0.4 { return .orange }
        return .green
    }
}

// MARK: - Visual Effect View

struct VisualEffectView: NSViewRepresentable {
    let material: NSVisualEffectView.Material
    let blendingMode: NSVisualEffectView.BlendingMode

    func makeNSView(context: Context) -> NSVisualEffectView {
        let view = NSVisualEffectView()
        view.material = material
        view.blendingMode = blendingMode
        view.state = .active
        return view
    }

    func updateNSView(_ nsView: NSVisualEffectView, context: Context) {
        nsView.material = material
        nsView.blendingMode = blendingMode
    }
}

#Preview {
    let controller = VoiceWakeOverlayController.shared
    return VoiceWakeOverlayView(controller: controller)
        .frame(width: 360, height: 200)
        .padding()
        .background(Color.black.opacity(0.5))
}
