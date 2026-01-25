import AppKit
import SwiftUI

/// SwiftUI overlay showing voice wake transcript with audio level meter.
struct VoiceWakeOverlayView: View {
    @Binding var transcript: String
    @Binding var audioLevel: Double
    var onCancel: () -> Void
    var onSend: () -> Void

    @State private var isHovering = false

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Text(transcript.isEmpty ? "Listening..." : transcript)
                .font(.system(size: 13))
                .foregroundColor(transcript.isEmpty ? .secondary : .primary)
                .frame(maxWidth: .infinity, alignment: .leading)

            Button(action: onSend) {
                ZStack {
                    RoundedRectangle(cornerRadius: 6).fill(Color.accentColor.opacity(0.12))
                    GeometryReader { geo in
                        RoundedRectangle(cornerRadius: 6)
                            .fill(Color.accentColor.opacity(0.3))
                            .frame(width: geo.size.width * min(1, max(0, audioLevel)))
                    }
                    Image(systemName: "paperplane.fill").font(.system(size: 11))
                }
                .frame(width: 32, height: 26)
                .clipShape(RoundedRectangle(cornerRadius: 6))
            }
            .buttonStyle(.plain)
            .disabled(transcript.isEmpty)
        }
        .padding(12)
        .background(VisualEffectBackground().clipShape(RoundedRectangle(cornerRadius: 10)))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Color.white.opacity(0.15)))
        .shadow(color: .black.opacity(0.2), radius: 12, y: 2)
        .overlay(alignment: .topLeading) {
            if isHovering {
                Button(action: onCancel) {
                    Image(systemName: "xmark").font(.system(size: 10, weight: .bold))
                        .frame(width: 18, height: 18)
                        .background(Color.black.opacity(0.4)).clipShape(Circle())
                }
                .buttonStyle(.plain).offset(x: -6, y: -6)
            }
        }
        .onHover { isHovering = $0 }
    }
}

struct VisualEffectBackground: NSViewRepresentable {
    func makeNSView(context: Context) -> NSVisualEffectView {
        let view = NSVisualEffectView()
        view.material = .hudWindow
        view.blendingMode = .behindWindow
        view.state = .active
        return view
    }
    func updateNSView(_ nsView: NSVisualEffectView, context: Context) {}
}

/// Controller for managing the voice wake overlay window.
@MainActor
final class VoiceWakeOverlayController: ObservableObject {
    static let shared = VoiceWakeOverlayController()

    @Published var transcript: String = ""
    @Published var audioLevel: Double = 0
    @Published var isVisible: Bool = false

    private var window: NSPanel?
    var onCancel: (() -> Void)?
    var onSend: (() -> Void)?

    private init() {}

    func show() {
        guard window == nil else { isVisible = true; window?.orderFront(nil); return }

        let panel = NSPanel(
            contentRect: NSRect(x: 0, y: 0, width: 320, height: 60),
            styleMask: [.nonactivatingPanel, .fullSizeContentView],
            backing: .buffered, defer: false)
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.level = .popUpMenu
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]

        panel.contentView = NSHostingView(rootView: VoiceWakeOverlayView(
            transcript: Binding(get: { self.transcript }, set: { self.transcript = $0 }),
            audioLevel: Binding(get: { self.audioLevel }, set: { self.audioLevel = $0 }),
            onCancel: { [weak self] in self?.cancel() },
            onSend: { [weak self] in self?.send() }))

        if let screen = NSScreen.main {
            panel.setFrameOrigin(NSPoint(x: screen.visibleFrame.maxX - 340, y: screen.visibleFrame.maxY - 80))
        }

        window = panel
        isVisible = true
        panel.orderFront(nil)
    }

    func dismiss() {
        isVisible = false
        window?.orderOut(nil)
        transcript = ""
        audioLevel = 0
    }

    func updateTranscript(_ text: String) { transcript = text }
    func updateLevel(_ level: Double) { audioLevel = level }
    private func cancel() { onCancel?(); dismiss() }
    private func send() { onSend?(); dismiss() }
}
