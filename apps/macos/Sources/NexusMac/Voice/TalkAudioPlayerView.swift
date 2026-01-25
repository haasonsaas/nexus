import Combine
import OSLog
import SwiftUI

// MARK: - Talk Audio Player View

/// Main view for audio playback controls with play/pause, progress, speed, and volume.
struct TalkAudioPlayerView: View {
    @State private var player = TalkAudioPlayer.shared
    @State private var showSpeedPicker = false
    @State private var showVolumePicker = false
    @State private var isDraggingProgress = false
    @State private var dragProgress: Double = 0

    private let logger = Logger(subsystem: "com.nexus.mac", category: "talk-audio")

    var body: some View {
        VStack(spacing: 12) {
            // Waveform visualization
            TalkAudioWaveformView(
                levels: player.audioLevels,
                isPlaying: player.state == .playing
            )
            .frame(height: 48)

            // Progress section
            progressSection

            // Controls section
            HStack(spacing: 16) {
                // Speed control
                speedControl

                Spacer()

                // Playback controls
                playbackControls

                Spacer()

                // Volume control
                volumeControl
            }
            .padding(.horizontal, 8)
        }
        .padding(16)
        .background(
            RoundedRectangle(cornerRadius: 12)
                .fill(Color(nsColor: .windowBackgroundColor))
                .shadow(color: Color.black.opacity(0.1), radius: 4, x: 0, y: 2)
        )
    }

    // MARK: - Progress Section

    private var progressSection: some View {
        VStack(spacing: 4) {
            // Progress slider
            GeometryReader { geometry in
                ZStack(alignment: .leading) {
                    // Track background
                    RoundedRectangle(cornerRadius: 2)
                        .fill(Color.gray.opacity(0.3))
                        .frame(height: 4)

                    // Progress fill
                    RoundedRectangle(cornerRadius: 2)
                        .fill(Color.accentColor)
                        .frame(width: progressWidth(in: geometry.size.width), height: 4)

                    // Draggable thumb
                    Circle()
                        .fill(Color.white)
                        .frame(width: 12, height: 12)
                        .shadow(color: Color.black.opacity(0.2), radius: 2, x: 0, y: 1)
                        .offset(x: thumbOffset(in: geometry.size.width))
                }
                .frame(height: 12)
                .contentShape(Rectangle())
                .gesture(
                    DragGesture(minimumDistance: 0)
                        .onChanged { value in
                            isDraggingProgress = true
                            let progress = max(0, min(1, value.location.x / geometry.size.width))
                            dragProgress = progress * player.duration
                        }
                        .onEnded { value in
                            let progress = max(0, min(1, value.location.x / geometry.size.width))
                            let time = progress * player.duration
                            player.seek(to: time)
                            isDraggingProgress = false
                        }
                )
            }
            .frame(height: 12)

            // Time labels
            HStack {
                Text(formatTime(isDraggingProgress ? dragProgress : player.currentTime))
                    .font(.caption)
                    .monospacedDigit()
                    .foregroundStyle(.secondary)

                Spacer()

                Text(formatTime(player.duration))
                    .font(.caption)
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
            }
        }
    }

    private func progressWidth(in totalWidth: CGFloat) -> CGFloat {
        guard player.duration > 0 else { return 0 }
        let progress = isDraggingProgress ? dragProgress : player.currentTime
        return CGFloat(progress / player.duration) * totalWidth
    }

    private func thumbOffset(in totalWidth: CGFloat) -> CGFloat {
        guard player.duration > 0 else { return -6 }
        let progress = isDraggingProgress ? dragProgress : player.currentTime
        return CGFloat(progress / player.duration) * totalWidth - 6
    }

    // MARK: - Playback Controls

    private var playbackControls: some View {
        HStack(spacing: 20) {
            // Skip backward
            Button {
                player.seekBackward(by: 10)
            } label: {
                Image(systemName: "gobackward.10")
                    .font(.system(size: 18))
            }
            .buttonStyle(.plain)
            .foregroundStyle(.primary)
            .disabled(!canSeek)

            // Play/Pause button
            Button {
                handlePlayPause()
            } label: {
                ZStack {
                    Circle()
                        .fill(Color.accentColor)
                        .frame(width: 44, height: 44)

                    if player.state == .loading {
                        ProgressView()
                            .scaleEffect(0.7)
                            .progressViewStyle(CircularProgressViewStyle(tint: .white))
                    } else {
                        Image(systemName: playPauseIcon)
                            .font(.system(size: 20, weight: .semibold))
                            .foregroundStyle(.white)
                    }
                }
            }
            .buttonStyle(.plain)
            .disabled(player.state == .loading)

            // Skip forward
            Button {
                player.seekForward(by: 10)
            } label: {
                Image(systemName: "goforward.10")
                    .font(.system(size: 18))
            }
            .buttonStyle(.plain)
            .foregroundStyle(.primary)
            .disabled(!canSeek)
        }
    }

    private var playPauseIcon: String {
        switch player.state {
        case .playing:
            return "pause.fill"
        case .paused, .idle, .finished:
            return "play.fill"
        case .loading:
            return "play.fill"
        }
    }

    private var canSeek: Bool {
        player.state == .playing || player.state == .paused
    }

    private func handlePlayPause() {
        switch player.state {
        case .playing:
            player.pause()
        case .paused:
            player.resume()
        case .idle, .finished:
            // Could restart from beginning if we had the data
            break
        case .loading:
            break
        }
    }

    // MARK: - Speed Control

    private var speedControl: some View {
        Button {
            showSpeedPicker.toggle()
        } label: {
            HStack(spacing: 2) {
                Text(formatSpeed(player.playbackSpeed))
                    .font(.caption)
                    .fontWeight(.medium)
                    .monospacedDigit()
                Image(systemName: "chevron.up.chevron.down")
                    .font(.system(size: 8))
            }
            .foregroundStyle(.secondary)
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(
                RoundedRectangle(cornerRadius: 6)
                    .fill(Color.gray.opacity(0.15))
            )
        }
        .buttonStyle(.plain)
        .popover(isPresented: $showSpeedPicker) {
            TalkAudioSpeedPicker(
                currentSpeed: player.playbackSpeed,
                onSpeedSelected: { speed in
                    player.setSpeed(speed)
                    showSpeedPicker = false
                }
            )
            .padding(8)
        }
    }

    // MARK: - Volume Control

    private var volumeControl: some View {
        Button {
            showVolumePicker.toggle()
        } label: {
            Image(systemName: volumeIcon)
                .font(.system(size: 14))
                .foregroundStyle(.secondary)
                .frame(width: 28, height: 28)
                .background(
                    RoundedRectangle(cornerRadius: 6)
                        .fill(Color.gray.opacity(0.15))
                )
        }
        .buttonStyle(.plain)
        .popover(isPresented: $showVolumePicker) {
            TalkAudioVolumeSlider(
                volume: player.volume,
                onVolumeChanged: { volume in
                    player.setVolume(volume)
                }
            )
            .padding(8)
        }
    }

    private var volumeIcon: String {
        if player.volume == 0 {
            return "speaker.slash.fill"
        } else if player.volume < 0.33 {
            return "speaker.wave.1.fill"
        } else if player.volume < 0.66 {
            return "speaker.wave.2.fill"
        } else {
            return "speaker.wave.3.fill"
        }
    }

    // MARK: - Formatting

    private func formatTime(_ time: TimeInterval) -> String {
        let totalSeconds = Int(time)
        let minutes = totalSeconds / 60
        let seconds = totalSeconds % 60
        return String(format: "%d:%02d", minutes, seconds)
    }

    private func formatSpeed(_ speed: Float) -> String {
        if speed == 1.0 {
            return "1x"
        } else if speed == floor(speed) {
            return String(format: "%.0fx", speed)
        } else {
            return String(format: "%.2gx", speed)
        }
    }
}

// MARK: - Speed Picker

struct TalkAudioSpeedPicker: View {
    let currentSpeed: Float
    let onSpeedSelected: (Float) -> Void

    private let speeds: [Float] = [0.5, 0.75, 1.0, 1.25, 1.5, 1.75, 2.0]

    var body: some View {
        VStack(spacing: 4) {
            Text("Playback Speed")
                .font(.caption)
                .fontWeight(.medium)
                .foregroundStyle(.secondary)
                .padding(.bottom, 4)

            ForEach(speeds, id: \.self) { speed in
                Button {
                    onSpeedSelected(speed)
                } label: {
                    HStack {
                        Text(formatSpeed(speed))
                            .font(.system(.body, design: .monospaced))

                        Spacer()

                        if abs(currentSpeed - speed) < 0.01 {
                            Image(systemName: "checkmark")
                                .font(.caption)
                                .fontWeight(.semibold)
                        }
                    }
                    .padding(.horizontal, 12)
                    .padding(.vertical, 6)
                    .background(
                        abs(currentSpeed - speed) < 0.01
                            ? Color.accentColor.opacity(0.15)
                            : Color.clear
                    )
                    .cornerRadius(4)
                }
                .buttonStyle(.plain)
            }
        }
        .frame(width: 140)
    }

    private func formatSpeed(_ speed: Float) -> String {
        if speed == 1.0 {
            return "1x (Normal)"
        } else if speed == floor(speed) {
            return String(format: "%.0fx", speed)
        } else {
            return String(format: "%.2gx", speed)
        }
    }
}

// MARK: - Volume Slider

struct TalkAudioVolumeSlider: View {
    let volume: Float
    let onVolumeChanged: (Float) -> Void

    var body: some View {
        VStack(spacing: 8) {
            Text("Volume")
                .font(.caption)
                .fontWeight(.medium)
                .foregroundStyle(.secondary)

            HStack(spacing: 8) {
                Image(systemName: "speaker.fill")
                    .font(.caption)
                    .foregroundStyle(.secondary)

                Slider(
                    value: Binding(
                        get: { Double(volume) },
                        set: { onVolumeChanged(Float($0)) }
                    ),
                    in: 0...1
                )
                .frame(width: 100)

                Image(systemName: "speaker.wave.3.fill")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Text("\(Int(volume * 100))%")
                .font(.caption)
                .monospacedDigit()
                .foregroundStyle(.secondary)
        }
    }
}

// MARK: - Waveform Visualization

struct TalkAudioWaveformView: View {
    let levels: [Float]
    let isPlaying: Bool

    private let barCount = 32
    private let barSpacing: CGFloat = 2

    var body: some View {
        GeometryReader { geometry in
            HStack(spacing: barSpacing) {
                ForEach(0..<barCount, id: \.self) { index in
                    waveformBar(at: index, in: geometry.size)
                }
            }
        }
    }

    private func waveformBar(at index: Int, in size: CGSize) -> some View {
        let barWidth = (size.width - CGFloat(barCount - 1) * barSpacing) / CGFloat(barCount)
        let level = levelForBar(at: index)
        let height = max(4, level * size.height)

        return RoundedRectangle(cornerRadius: 2)
            .fill(barColor(for: level))
            .frame(width: barWidth, height: height)
            .animation(.easeOut(duration: 0.1), value: level)
    }

    private func levelForBar(at index: Int) -> CGFloat {
        guard !levels.isEmpty else {
            // Show static waveform when not playing
            return staticLevel(at: index)
        }

        // Map bar index to level array
        let levelIndex = (index * levels.count) / barCount
        guard levelIndex < levels.count else {
            return staticLevel(at: index)
        }

        return CGFloat(levels[levelIndex])
    }

    private func staticLevel(at index: Int) -> CGFloat {
        // Create a simple static waveform pattern
        let center = barCount / 2
        let distance = abs(index - center)
        let normalizedDistance = CGFloat(distance) / CGFloat(center)
        return isPlaying ? 0.3 : max(0.1, 0.5 - normalizedDistance * 0.4)
    }

    private func barColor(for level: CGFloat) -> Color {
        if level > 0.8 {
            return .orange
        } else if level > 0.6 {
            return .accentColor
        } else {
            return .accentColor.opacity(0.7)
        }
    }
}

// MARK: - Compact Player View

/// A compact version of the audio player for inline use.
struct TalkAudioPlayerCompactView: View {
    @State private var player = TalkAudioPlayer.shared

    var body: some View {
        HStack(spacing: 12) {
            // Play/Pause button
            Button {
                if player.state == .playing {
                    player.pause()
                } else {
                    player.resume()
                }
            } label: {
                Image(systemName: player.state == .playing ? "pause.fill" : "play.fill")
                    .font(.system(size: 14))
                    .frame(width: 28, height: 28)
                    .background(Color.accentColor)
                    .foregroundStyle(.white)
                    .clipShape(Circle())
            }
            .buttonStyle(.plain)

            // Progress bar
            GeometryReader { geometry in
                ZStack(alignment: .leading) {
                    RoundedRectangle(cornerRadius: 2)
                        .fill(Color.gray.opacity(0.3))
                        .frame(height: 4)

                    RoundedRectangle(cornerRadius: 2)
                        .fill(Color.accentColor)
                        .frame(
                            width: player.duration > 0
                                ? CGFloat(player.currentTime / player.duration) * geometry.size.width
                                : 0,
                            height: 4
                        )
                }
                .frame(height: 4)
            }
            .frame(height: 4)

            // Time display
            Text(formatTime(player.currentTime))
                .font(.caption)
                .monospacedDigit()
                .foregroundStyle(.secondary)
                .frame(width: 40, alignment: .trailing)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(
            RoundedRectangle(cornerRadius: 8)
                .fill(Color(nsColor: .controlBackgroundColor))
        )
    }

    private func formatTime(_ time: TimeInterval) -> String {
        let totalSeconds = Int(time)
        let minutes = totalSeconds / 60
        let seconds = totalSeconds % 60
        return String(format: "%d:%02d", minutes, seconds)
    }
}

// MARK: - Mini Player View

/// A minimal player view showing just essential controls.
struct TalkAudioPlayerMiniView: View {
    @State private var player = TalkAudioPlayer.shared

    var body: some View {
        HStack(spacing: 8) {
            // State indicator
            Circle()
                .fill(stateColor)
                .frame(width: 8, height: 8)

            // Play/Pause
            Button {
                player.togglePlayPause()
            } label: {
                Image(systemName: player.state == .playing ? "pause.fill" : "play.fill")
                    .font(.system(size: 12))
            }
            .buttonStyle(.plain)

            // Stop
            Button {
                player.stop()
            } label: {
                Image(systemName: "stop.fill")
                    .font(.system(size: 10))
            }
            .buttonStyle(.plain)
            .disabled(player.state == .idle)
        }
        .padding(4)
    }

    private var stateColor: Color {
        switch player.state {
        case .idle: return .gray
        case .loading: return .yellow
        case .playing: return .green
        case .paused: return .orange
        case .finished: return .blue
        }
    }
}

// MARK: - Preview

#Preview("Full Player") {
    TalkAudioPlayerView()
        .frame(width: 320)
        .padding()
}

#Preview("Compact Player") {
    TalkAudioPlayerCompactView()
        .frame(width: 280)
        .padding()
}

#Preview("Mini Player") {
    TalkAudioPlayerMiniView()
        .padding()
}
