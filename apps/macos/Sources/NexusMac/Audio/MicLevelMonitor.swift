import AVFoundation
import OSLog
import SwiftUI

/// Actor for monitoring microphone input levels with smoothed output.
/// Used for VoiceWake/TalkMode audio level visualization.
actor MicLevelMonitor {
    static let shared = MicLevelMonitor()
    private let logger = Logger(subsystem: "com.nexus.mac", category: "audio.meter")
    private var engine: AVAudioEngine?
    private var update: (@Sendable (Double) -> Void)?
    private var running = false
    private var smoothedLevel: Double = 0

    /// Starts monitoring microphone levels with a callback for level updates.
    /// - Parameter onLevel: Callback receiving normalized level values (0.0-1.0)
    /// - Throws: Error if no audio input is available
    func start(onLevel: @Sendable @escaping (Double) -> Void) async throws {
        self.update = onLevel
        if self.running { return }
        self.logger.info("mic level monitor start")
        let engine = AVAudioEngine()
        self.engine = engine
        let input = engine.inputNode
        let format = input.outputFormat(forBus: 0)
        guard format.channelCount > 0, format.sampleRate > 0 else {
            self.engine = nil
            throw NSError(
                domain: "MicLevelMonitor",
                code: 1,
                userInfo: [NSLocalizedDescriptionKey: "No audio input available"])
        }
        input.removeTap(onBus: 0)
        input.installTap(onBus: 0, bufferSize: 512, format: format) { [weak self] buffer, _ in
            guard let self else { return }
            let level = Self.normalizedLevel(from: buffer)
            Task { await self.push(level: level) }
        }
        engine.prepare()
        try engine.start()
        self.running = true
    }

    /// Stops monitoring and releases audio resources.
    func stop() {
        guard self.running else { return }
        if let engine {
            engine.inputNode.removeTap(onBus: 0)
            engine.stop()
        }
        self.engine = nil
        self.running = false
        self.smoothedLevel = 0
    }

    /// Pushes a new level value through smoothing and invokes the callback.
    private func push(level: Double) {
        self.smoothedLevel = (self.smoothedLevel * 0.45) + (level * 0.55)
        guard let update else { return }
        let value = self.smoothedLevel
        Task { @MainActor in update(value) }
    }

    /// Calculates normalized level (0-1) from audio buffer using RMS and dB scale.
    private static func normalizedLevel(from buffer: AVAudioPCMBuffer) -> Double {
        guard let channel = buffer.floatChannelData?[0] else { return 0 }
        let frameCount = Int(buffer.frameLength)
        guard frameCount > 0 else { return 0 }
        var sum: Float = 0
        for i in 0..<frameCount {
            let s = channel[i]
            sum += s * s
        }
        let rms = sqrt(sum / Float(frameCount) + 1e-12)
        let db = 20 * log10(Double(rms))
        let normalized = max(0, min(1, (db + 50) / 50))
        return normalized
    }
}

/// Horizontal bar view displaying microphone level with color-coded segments.
struct MicLevelBar: View {
    /// Current level value (0.0-1.0)
    let level: Double
    /// Number of segments in the bar
    let segments: Int = 12

    var body: some View {
        HStack(spacing: 3) {
            ForEach(0..<self.segments, id: \.self) { idx in
                let fill = self.level * Double(self.segments) > Double(idx)
                RoundedRectangle(cornerRadius: 2)
                    .fill(fill ? self.segmentColor(for: idx) : Color.gray.opacity(0.35))
                    .frame(width: 14, height: 10)
            }
        }
        .padding(4)
        .background(
            RoundedRectangle(cornerRadius: 6)
                .stroke(Color.gray.opacity(0.25), lineWidth: 1))
    }

    /// Returns the color for a segment based on its position.
    /// Green for low levels, yellow for medium, red for high.
    private func segmentColor(for idx: Int) -> Color {
        let fraction = Double(idx + 1) / Double(self.segments)
        if fraction < 0.65 { return .green }
        if fraction < 0.85 { return .yellow }
        return .red
    }
}
