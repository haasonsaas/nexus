import SwiftUI

// MARK: - Voice Talk Mode Phase

/// Talk mode phases for Voice overlay
enum VoiceTalkPhase: String, Sendable {
    case idle
    case listening
    case thinking
    case speaking
    case paused
}

// MARK: - Main Overlay View

/// Main talk mode overlay view with orb
struct VoiceTalkOverlayView: View {
    @State private var talkMode = VoiceTalkModeRuntime.shared
    @State private var isHovering = false
    @State private var isDragging = false

    var body: some View {
        ZStack {
            // Background blur
            VisualEffectView(material: .hudWindow, blendingMode: .behindWindow)
                .clipShape(Circle())

            // Orb
            VoiceTalkOrbView(
                phase: talkMode.phase,
                audioLevel: talkMode.audioLevel
            )
            .frame(width: 96, height: 96)

            // Close button on hover
            if isHovering && !isDragging {
                VStack {
                    HStack {
                        Spacer()
                        Button {
                            talkMode.stop()
                        } label: {
                            Image(systemName: "xmark.circle.fill")
                                .font(.system(size: 20))
                                .foregroundStyle(.secondary)
                        }
                        .buttonStyle(.plain)
                        .padding(8)
                    }
                    Spacer()
                }
                .transition(.opacity)
            }
        }
        .frame(width: 120, height: 120)
        .onHover { hovering in
            withAnimation(.easeOut(duration: 0.15)) {
                isHovering = hovering
            }
        }
        .gesture(
            DragGesture()
                .onChanged { _ in
                    isDragging = true
                }
                .onEnded { _ in
                    isDragging = false
                }
        )
        .onTapGesture(count: 2) {
            // Double tap to pause/resume
            if talkMode.phase == .paused {
                talkMode.resume()
            } else {
                talkMode.pause()
            }
        }
        .onTapGesture(count: 1) {
            // Single tap to toggle mute
            talkMode.toggleMute()
        }
    }
}

// MARK: - Animated Orb View

/// Animated orb showing talk mode state
struct VoiceTalkOrbView: View {
    let phase: VoiceTalkPhase
    let audioLevel: Float

    @State private var pulseScale: CGFloat = 1.0
    @State private var rotationAngle: Double = 0

    var body: some View {
        ZStack {
            // Outer pulse ring
            if phase == .listening || phase == .speaking {
                Circle()
                    .stroke(ringColor.opacity(0.3), lineWidth: 2)
                    .scaleEffect(pulseScale)
            }

            // Main orb
            Circle()
                .fill(
                    RadialGradient(
                        colors: gradientColors,
                        center: .center,
                        startRadius: 0,
                        endRadius: 48
                    )
                )
                .shadow(color: shadowColor.opacity(0.5), radius: 8)

            // Inner glow based on audio level
            Circle()
                .fill(innerGlowColor.opacity(Double(audioLevel) * 0.5))
                .scaleEffect(0.6 + CGFloat(audioLevel) * 0.2)
                .blur(radius: 4)

            // Phase icon
            phaseIcon
                .font(.system(size: 32, weight: .medium))
                .foregroundStyle(.white)
        }
        .onChange(of: phase) { _, newPhase in
            updateAnimations(for: newPhase)
        }
        .onAppear {
            updateAnimations(for: phase)
        }
    }

    @ViewBuilder
    private var phaseIcon: some View {
        switch phase {
        case .idle:
            Image(systemName: "waveform")
        case .listening:
            Image(systemName: "mic.fill")
        case .thinking:
            ProgressView()
                .progressViewStyle(.circular)
                .scaleEffect(0.8)
        case .speaking:
            Image(systemName: "speaker.wave.2.fill")
        case .paused:
            Image(systemName: "pause.fill")
        }
    }

    private var gradientColors: [Color] {
        switch phase {
        case .idle:
            return [.gray.opacity(0.8), .gray.opacity(0.4)]
        case .listening:
            return [.blue, .blue.opacity(0.6)]
        case .thinking:
            return [.purple, .purple.opacity(0.6)]
        case .speaking:
            return [.green, .green.opacity(0.6)]
        case .paused:
            return [.orange, .orange.opacity(0.6)]
        }
    }

    private var ringColor: Color {
        switch phase {
        case .listening: return .blue
        case .speaking: return .green
        default: return .clear
        }
    }

    private var shadowColor: Color {
        switch phase {
        case .listening: return .blue
        case .thinking: return .purple
        case .speaking: return .green
        default: return .gray
        }
    }

    private var innerGlowColor: Color {
        switch phase {
        case .listening: return .white
        case .speaking: return .green
        default: return .clear
        }
    }

    private func updateAnimations(for phase: VoiceTalkPhase) {
        withAnimation(.easeInOut(duration: 1.3).repeatForever(autoreverses: true)) {
            pulseScale = phase == .listening || phase == .speaking ? 1.15 : 1.0
        }
    }
}

// MARK: - Preview

#Preview {
    VoiceTalkOverlayView()
        .frame(width: 200, height: 200)
        .background(Color.black.opacity(0.5))
}
