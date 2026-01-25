import AVFoundation
import Foundation
import OSLog
import ScreenCaptureKit

/// Service for recording screen content.
/// Enables AI agents to capture video of the screen.
@MainActor
@Observable
final class ScreenRecordService: NSObject {
    static let shared = ScreenRecordService()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "screen.record")

    private(set) var isRecording = false
    private(set) var recordingURL: URL?
    private(set) var recordingDuration: TimeInterval = 0

    private var stream: SCStream?
    private var streamOutput: ScreenStreamOutput?
    private var assetWriter: AVAssetWriter?
    private var videoInput: AVAssetWriterInput?
    private var startTime: Date?
    private var durationTimer: Timer?

    struct RecordingOptions {
        var displayID: CGDirectDisplayID?
        var captureRect: CGRect?
        var frameRate: Int = 30
        var quality: Quality = .high
        var includesCursor: Bool = true

        enum Quality {
            case low
            case medium
            case high

            var preset: String {
                switch self {
                case .low: return AVAssetExportPresetLowQuality
                case .medium: return AVAssetExportPresetMediumQuality
                case .high: return AVAssetExportPresetHighestQuality
                }
            }
        }
    }

    // MARK: - Recording

    /// Start recording the screen
    @available(macOS 12.3, *)
    func startRecording(options: RecordingOptions = RecordingOptions()) async throws {
        guard !isRecording else {
            throw ScreenRecordError.alreadyRecording
        }

        // Get shareable content
        let content = try await SCShareableContent.current
        guard let display = content.displays.first(where: {
            options.displayID == nil || $0.displayID == options.displayID
        }) else {
            throw ScreenRecordError.noDisplayAvailable
        }

        // Create stream configuration
        let config = SCStreamConfiguration()
        config.width = Int(display.width)
        config.height = Int(display.height)
        config.minimumFrameInterval = CMTime(value: 1, timescale: CMTimeScale(options.frameRate))
        config.queueDepth = 5
        config.showsCursor = options.includesCursor

        // Create content filter
        let filter = SCContentFilter(display: display, excludingWindows: [])

        // Create output URL
        let outputURL = FileManager.default.temporaryDirectory
            .appendingPathComponent("nexus_recording_\(Int(Date().timeIntervalSince1970)).mov")

        // Set up asset writer
        let writer = try AVAssetWriter(outputURL: outputURL, fileType: .mov)

        let videoSettings: [String: Any] = [
            AVVideoCodecKey: AVVideoCodecType.h264,
            AVVideoWidthKey: config.width,
            AVVideoHeightKey: config.height
        ]

        let input = AVAssetWriterInput(mediaType: .video, outputSettings: videoSettings)
        input.expectsMediaDataInRealTime = true

        guard writer.canAdd(input) else {
            throw ScreenRecordError.setupFailed("Cannot add video input")
        }
        writer.add(input)

        // Create stream output handler
        let output = ScreenStreamOutput(writerInput: input)
        streamOutput = output

        // Create and start stream
        let stream = SCStream(filter: filter, configuration: config, delegate: nil)
        try stream.addStreamOutput(output, type: .screen, sampleHandlerQueue: DispatchQueue(label: "screen.record"))

        writer.startWriting()
        writer.startSession(atSourceTime: .zero)

        try await stream.startCapture()

        self.stream = stream
        self.assetWriter = writer
        self.videoInput = input
        self.recordingURL = outputURL
        self.startTime = Date()
        self.isRecording = true

        // Start duration timer
        durationTimer = Timer.scheduledTimer(withTimeInterval: 0.1, repeats: true) { [weak self] _ in
            Task { @MainActor in
                if let start = self?.startTime {
                    self?.recordingDuration = Date().timeIntervalSince(start)
                }
            }
        }

        logger.info("screen recording started url=\(outputURL.path)")
    }

    /// Stop recording and return the video URL
    @available(macOS 12.3, *)
    func stopRecording() async throws -> URL {
        guard isRecording, let stream = stream, let writer = assetWriter, let url = recordingURL else {
            throw ScreenRecordError.notRecording
        }

        durationTimer?.invalidate()
        durationTimer = nil

        try await stream.stopCapture()

        videoInput?.markAsFinished()
        await writer.finishWriting()

        self.stream = nil
        self.streamOutput = nil
        self.assetWriter = nil
        self.videoInput = nil
        self.isRecording = false
        self.startTime = nil

        logger.info("screen recording stopped duration=\(String(format: "%.1f", self.recordingDuration))s")

        return url
    }

    /// Cancel recording without saving
    @available(macOS 12.3, *)
    func cancelRecording() async {
        guard isRecording else { return }

        durationTimer?.invalidate()
        durationTimer = nil

        if let stream = stream {
            try? await stream.stopCapture()
        }

        assetWriter?.cancelWriting()

        // Clean up temp file
        if let url = recordingURL {
            try? FileManager.default.removeItem(at: url)
        }

        self.stream = nil
        self.streamOutput = nil
        self.assetWriter = nil
        self.videoInput = nil
        self.recordingURL = nil
        self.isRecording = false
        self.startTime = nil
        self.recordingDuration = 0

        logger.info("screen recording cancelled")
    }
}

/// Stream output handler for screen recording
@available(macOS 12.3, *)
private class ScreenStreamOutput: NSObject, SCStreamOutput {
    private let writerInput: AVAssetWriterInput
    private var firstSampleTime: CMTime?

    init(writerInput: AVAssetWriterInput) {
        self.writerInput = writerInput
    }

    func stream(_ stream: SCStream, didOutputSampleBuffer sampleBuffer: CMSampleBuffer, of type: SCStreamOutputType) {
        guard type == .screen else { return }
        guard writerInput.isReadyForMoreMediaData else { return }

        let presentationTime = CMSampleBufferGetPresentationTimeStamp(sampleBuffer)

        if firstSampleTime == nil {
            firstSampleTime = presentationTime
        }

        // Adjust timing relative to first sample
        let adjustedTime = CMTimeSubtract(presentationTime, firstSampleTime!)

        if let adjustedBuffer = adjustSampleBufferTime(sampleBuffer, to: adjustedTime) {
            writerInput.append(adjustedBuffer)
        }
    }

    private func adjustSampleBufferTime(_ buffer: CMSampleBuffer, to newTime: CMTime) -> CMSampleBuffer? {
        var timing = CMSampleTimingInfo(
            duration: CMSampleBufferGetDuration(buffer),
            presentationTimeStamp: newTime,
            decodeTimeStamp: .invalid
        )

        var newBuffer: CMSampleBuffer?
        CMSampleBufferCreateCopyWithNewTiming(
            allocator: nil,
            sampleBuffer: buffer,
            sampleTimingEntryCount: 1,
            sampleTimingArray: &timing,
            sampleBufferOut: &newBuffer
        )

        return newBuffer
    }
}

enum ScreenRecordError: LocalizedError {
    case alreadyRecording
    case notRecording
    case noDisplayAvailable
    case setupFailed(String)
    case writeFailed(String)

    var errorDescription: String? {
        switch self {
        case .alreadyRecording:
            return "Already recording"
        case .notRecording:
            return "Not currently recording"
        case .noDisplayAvailable:
            return "No display available"
        case .setupFailed(let reason):
            return "Recording setup failed: \(reason)"
        case .writeFailed(let reason):
            return "Recording write failed: \(reason)"
        }
    }
}
