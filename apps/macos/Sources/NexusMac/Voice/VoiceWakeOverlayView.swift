import SwiftUI

/// Overlay view showing the voice wake transcript.
struct VoiceWakeOverlayView: View {
    @Bindable var controller: VoiceWakeOverlayController
    @FocusState private var textFocused: Bool
    @State private var isHovering = false
    @State private var closeHovering = false

    var body: some View {
        ZStack(alignment: .topLeading) {
            // Main content
            HStack(alignment: .top, spacing: 8) {
                // Transcript text
                if controller.model.isEditing {
                    editableTextView
                } else {
                    displayTextView
                }

                // Send button
                sendButton
            }
            .padding(.vertical, 8)
            .padding(.horizontal, 10)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            .background {
                overlayBackground
            }
            .shadow(color: Color.black.opacity(0.22), radius: 14, x: 0, y: -2)
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

    // MARK: - Text Views

    @ViewBuilder
    private var editableTextView: some View {
        TextEditor(text: Binding(
            get: { controller.model.text },
            set: { controller.updateText($0) }
        ))
        .font(.system(size: 14))
        .scrollContentBackground(.hidden)
        .background(Color.clear)
        .focused($textFocused)
        .frame(maxWidth: .infinity, minHeight: 32, maxHeight: .infinity, alignment: .topLeading)
        .onSubmit {
            controller.requestSend()
        }
    }

    @ViewBuilder
    private var displayTextView: some View {
        Text(controller.model.text.isEmpty ? "Listening..." : controller.model.text)
            .font(.system(size: 14))
            .foregroundStyle(controller.model.text.isEmpty ? .secondary : .primary)
            .frame(maxWidth: .infinity, minHeight: 32, maxHeight: .infinity, alignment: .topLeading)
            .onTapGesture {
                controller.userBeganEditing()
                textFocused = true
            }
    }

    // MARK: - Send Button

    @ViewBuilder
    private var sendButton: some View {
        Button {
            controller.requestSend()
        } label: {
            ZStack {
                // Level indicator background
                GeometryReader { geo in
                    RoundedRectangle(cornerRadius: 8, style: .continuous)
                        .fill(Color.accentColor.opacity(0.12))

                    RoundedRectangle(cornerRadius: 8, style: .continuous)
                        .fill(Color.accentColor.opacity(0.25))
                        .frame(width: geo.size.width * controller.model.level, alignment: .leading)
                        .animation(.easeOut(duration: 0.08), value: controller.model.level)
                }
                .frame(height: 28)

                // Icon
                ZStack {
                    Image(systemName: "paperplane.fill")
                        .opacity(controller.model.isSending ? 0 : 1)
                        .scaleEffect(controller.model.isSending ? 0.5 : 1)

                    Image(systemName: "checkmark.circle.fill")
                        .foregroundStyle(.green)
                        .opacity(controller.model.isSending ? 1 : 0)
                        .scaleEffect(controller.model.isSending ? 1.05 : 0.8)
                }
                .imageScale(.small)
            }
            .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
            .frame(width: 32, height: 28)
            .animation(.spring(response: 0.35, dampingFraction: 0.78), value: controller.model.isSending)
        }
        .buttonStyle(.plain)
        .disabled(!controller.model.forwardEnabled || controller.model.isSending)
        .keyboardShortcut(.return, modifiers: .command)
    }

    // MARK: - Background

    @ViewBuilder
    private var overlayBackground: some View {
        let shape = RoundedRectangle(cornerRadius: 12, style: .continuous)

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
        .frame(width: 360, height: 100)
}
