import AppKit
import Observation
import OSLog
import SwiftUI

// MARK: - Overlay Controller

@MainActor
@Observable
final class TalkOverlayController {
    static let shared = TalkOverlayController()
    static let overlaySize: CGFloat = 440
    static let orbSize: CGFloat = 96
    static let orbPadding: CGFloat = 12
    static let orbHitSlop: CGFloat = 10

    private let logger = Logger(subsystem: "com.nexus.mac", category: "talk.overlay")

    struct Model {
        var isVisible: Bool = false
        var phase: TalkModePhase = .idle
        var isPaused: Bool = false
        var level: Double = 0
    }

    var model = Model()
    private var window: NSPanel?
    private var hostingView: NSHostingView<TalkOverlayView>?
    private let screenInset: CGFloat = 0

    private init() {}

    func present() {
        ensureWindow()
        hostingView?.rootView = TalkOverlayView(controller: self)
        let target = targetFrame()

        guard let window else { return }
        if !model.isVisible {
            model.isVisible = true
            let start = target.offsetBy(dx: 0, dy: -6)
            window.setFrame(start, display: true)
            window.alphaValue = 0
            window.orderFrontRegardless()
            NSAnimationContext.runAnimationGroup { context in
                context.duration = 0.18
                context.timingFunction = CAMediaTimingFunction(name: .easeOut)
                window.animator().setFrame(target, display: true)
                window.animator().alphaValue = 1
            }
        } else {
            window.setFrame(target, display: true)
            window.orderFrontRegardless()
        }
    }

    func dismiss() {
        guard let window else {
            model.isVisible = false
            return
        }

        let target = window.frame.offsetBy(dx: 6, dy: 6)
        NSAnimationContext.runAnimationGroup { context in
            context.duration = 0.16
            context.timingFunction = CAMediaTimingFunction(name: .easeOut)
            window.animator().setFrame(target, display: true)
            window.animator().alphaValue = 0
        } completionHandler: {
            Task { @MainActor in
                window.orderOut(nil)
                self.model.isVisible = false
            }
        }
    }

    func updatePhase(_ phase: TalkModePhase) {
        guard model.phase != phase else { return }
        logger.info("talk overlay phase=\(phase.rawValue, privacy: .public)")
        model.phase = phase
    }

    func updatePaused(_ paused: Bool) {
        guard model.isPaused != paused else { return }
        logger.info("talk overlay paused=\(paused)")
        model.isPaused = paused
    }

    func updateLevel(_ level: Double) {
        guard model.isVisible else { return }
        model.level = max(0, min(1, level))
    }

    func currentWindowOrigin() -> CGPoint? {
        window?.frame.origin
    }

    func setWindowOrigin(_ origin: CGPoint) {
        guard let window else { return }
        window.setFrameOrigin(origin)
    }

    // MARK: - Private

    private func ensureWindow() {
        if window != nil { return }
        let panel = NSPanel(
            contentRect: NSRect(x: 0, y: 0, width: Self.overlaySize, height: Self.overlaySize),
            styleMask: [.nonactivatingPanel, .borderless],
            backing: .buffered,
            defer: false
        )
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = false
        panel.level = NSWindow.Level(rawValue: NSWindow.Level.popUpMenu.rawValue - 4)
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .transient]
        panel.hidesOnDeactivate = false
        panel.isMovable = false
        panel.acceptsMouseMovedEvents = true
        panel.isFloatingPanel = true
        panel.becomesKeyOnlyIfNeeded = true
        panel.titleVisibility = .hidden
        panel.titlebarAppearsTransparent = true

        let host = TalkOverlayHostingView(rootView: TalkOverlayView(controller: self))
        host.translatesAutoresizingMaskIntoConstraints = false
        panel.contentView = host
        hostingView = host
        window = panel
    }

    private func targetFrame() -> NSRect {
        let screen = window?.screen ?? NSScreen.main ?? NSScreen.screens.first
        guard let screen else { return .zero }
        let size = NSSize(width: Self.overlaySize, height: Self.overlaySize)
        let visible = screen.visibleFrame
        let origin = CGPoint(
            x: visible.maxX - size.width - screenInset,
            y: visible.maxY - size.height - screenInset
        )
        return NSRect(origin: origin, size: size)
    }
}

// MARK: - Hosting View

private final class TalkOverlayHostingView: NSHostingView<TalkOverlayView> {
    override func acceptsFirstMouse(for event: NSEvent?) -> Bool {
        true
    }
}

// MARK: - Overlay View

struct TalkOverlayView: View {
    var controller: TalkOverlayController
    @State private var hoveringWindow = false

    private static let defaultAccentColor = Color(red: 79 / 255.0, green: 122 / 255.0, blue: 154 / 255.0)

    var body: some View {
        ZStack(alignment: .topTrailing) {
            let isPaused = controller.model.isPaused
            Color.clear
            TalkOrbView(
                phase: controller.model.phase,
                level: controller.model.level,
                accent: Self.defaultAccentColor,
                isPaused: isPaused
            )
            .frame(width: TalkOverlayController.orbSize, height: TalkOverlayController.orbSize)
            .padding(.top, TalkOverlayController.orbPadding)
            .padding(.trailing, TalkOverlayController.orbPadding)
            .contentShape(Circle())
            .opacity(isPaused ? 0.55 : 1)
            .background(
                TalkOrbInteractionView(
                    onSingleClick: { TalkModeController.shared.togglePaused() },
                    onDoubleClick: { TalkModeController.shared.stopSpeaking(reason: .userTap) },
                    onDragStart: { TalkModeController.shared.setPaused(true) }
                )
            )
            .overlay(alignment: .topLeading) {
                Button {
                    TalkModeController.shared.exitTalkMode()
                } label: {
                    Image(systemName: "xmark")
                        .font(.system(size: 10, weight: .bold))
                        .foregroundStyle(Color.white.opacity(0.95))
                        .frame(width: 18, height: 18)
                        .background(Color.black.opacity(0.4))
                        .clipShape(Circle())
                }
                .buttonStyle(.plain)
                .contentShape(Circle())
                .offset(x: -2, y: -2)
                .opacity(hoveringWindow ? 1 : 0)
                .animation(.easeOut(duration: 0.12), value: hoveringWindow)
            }
            .onHover { hoveringWindow = $0 }
        }
        .frame(
            width: TalkOverlayController.overlaySize,
            height: TalkOverlayController.overlaySize,
            alignment: .topTrailing
        )
    }
}

// MARK: - Orb Interaction View

private struct TalkOrbInteractionView: NSViewRepresentable {
    let onSingleClick: () -> Void
    let onDoubleClick: () -> Void
    let onDragStart: () -> Void

    func makeNSView(context: Context) -> NSView {
        let view = OrbInteractionNSView()
        view.onSingleClick = onSingleClick
        view.onDoubleClick = onDoubleClick
        view.onDragStart = onDragStart
        view.wantsLayer = true
        view.layer?.backgroundColor = NSColor.clear.cgColor
        return view
    }

    func updateNSView(_ nsView: NSView, context: Context) {
        guard let view = nsView as? OrbInteractionNSView else { return }
        view.onSingleClick = onSingleClick
        view.onDoubleClick = onDoubleClick
        view.onDragStart = onDragStart
    }
}

private final class OrbInteractionNSView: NSView {
    var onSingleClick: (() -> Void)?
    var onDoubleClick: (() -> Void)?
    var onDragStart: (() -> Void)?
    private var mouseDownEvent: NSEvent?
    private var didDrag = false
    private var suppressSingleClick = false

    override var acceptsFirstResponder: Bool { true }
    override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }

    override func mouseDown(with event: NSEvent) {
        mouseDownEvent = event
        didDrag = false
        suppressSingleClick = event.clickCount > 1
        if event.clickCount == 2 {
            onDoubleClick?()
        }
    }

    override func mouseDragged(with event: NSEvent) {
        guard let startEvent = mouseDownEvent else { return }
        if !didDrag {
            let dx = event.locationInWindow.x - startEvent.locationInWindow.x
            let dy = event.locationInWindow.y - startEvent.locationInWindow.y
            if abs(dx) + abs(dy) < 2 { return }
            didDrag = true
            onDragStart?()
            window?.performDrag(with: startEvent)
        }
    }

    override func mouseUp(with event: NSEvent) {
        if !didDrag, !suppressSingleClick {
            onSingleClick?()
        }
        mouseDownEvent = nil
        didDrag = false
        suppressSingleClick = false
    }
}

// MARK: - Orb View

private struct TalkOrbView: View {
    let phase: TalkModePhase
    let level: Double
    let accent: Color
    let isPaused: Bool

    var body: some View {
        if isPaused {
            Circle()
                .fill(orbGradient)
                .overlay(Circle().stroke(Color.white.opacity(0.35), lineWidth: 1))
                .shadow(color: Color.black.opacity(0.18), radius: 10, x: 0, y: 5)
        } else {
            TimelineView(.animation) { context in
                let t = context.date.timeIntervalSinceReferenceDate
                let listenScale = phase == .listening ? (1 + CGFloat(level) * 0.12) : 1
                let pulse = phase == .speaking ? (1 + 0.06 * sin(t * 6)) : 1

                ZStack {
                    Circle()
                        .fill(orbGradient)
                        .overlay(Circle().stroke(Color.white.opacity(0.45), lineWidth: 1))
                        .shadow(color: Color.black.opacity(0.22), radius: 10, x: 0, y: 5)
                        .scaleEffect(pulse * listenScale)

                    TalkWaveRings(phase: phase, level: level, time: t, accent: accent)

                    if phase == .thinking {
                        TalkOrbitArcs(time: t)
                    }
                }
            }
        }
    }

    private var orbGradient: RadialGradient {
        RadialGradient(
            colors: [Color.white, accent],
            center: .topLeading,
            startRadius: 4,
            endRadius: 52
        )
    }
}

// MARK: - Wave Rings

private struct TalkWaveRings: View {
    let phase: TalkModePhase
    let level: Double
    let time: TimeInterval
    let accent: Color

    var body: some View {
        ZStack {
            ForEach(0..<3, id: \.self) { idx in
                let speed = phase == .speaking ? 1.4 : phase == .listening ? 0.9 : 0.6
                let progress = (time * speed + Double(idx) * 0.28).truncatingRemainder(dividingBy: 1)
                let amplitude = phase == .speaking ? 0.95 : phase == .listening ? 0.5 + level * 0.7 : 0.35
                let scale = 0.75 + progress * amplitude + (phase == .listening ? level * 0.15 : 0)
                let alpha = phase == .speaking ? 0.72 : phase == .listening ? 0.58 + level * 0.28 : 0.4
                Circle()
                    .stroke(accent.opacity(alpha - progress * 0.3), lineWidth: 1.6)
                    .scaleEffect(scale)
                    .opacity(alpha - progress * 0.6)
            }
        }
    }
}

// MARK: - Orbit Arcs

private struct TalkOrbitArcs: View {
    let time: TimeInterval

    var body: some View {
        ZStack {
            Circle()
                .trim(from: 0.08, to: 0.26)
                .stroke(Color.white.opacity(0.88), style: StrokeStyle(lineWidth: 1.6, lineCap: .round))
                .rotationEffect(.degrees(time * 42))
            Circle()
                .trim(from: 0.62, to: 0.86)
                .stroke(Color.white.opacity(0.7), style: StrokeStyle(lineWidth: 1.4, lineCap: .round))
                .rotationEffect(.degrees(-time * 35))
        }
        .scaleEffect(1.08)
    }
}
