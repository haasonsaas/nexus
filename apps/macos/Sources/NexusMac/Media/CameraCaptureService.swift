import AVFoundation
import AppKit
import Foundation
import OSLog

/// Service for capturing camera input.
/// Enables AI agents to see through the Mac's camera.
@MainActor
@Observable
final class CameraCaptureService: NSObject {
    static let shared = CameraCaptureService()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "camera")

    private(set) var isCapturing = false
    private(set) var hasPermission = false
    private(set) var availableCameras: [CameraDevice] = []
    private(set) var selectedCamera: CameraDevice?

    private var captureSession: AVCaptureSession?
    private var videoOutput: AVCaptureVideoDataOutput?
    private var lastFrame: NSImage?

    struct CameraDevice: Identifiable, Equatable {
        let id: String
        let name: String
        let position: AVCaptureDevice.Position
        let isBuiltIn: Bool
    }

    struct CaptureResult {
        let image: NSImage
        let data: Data
        let timestamp: Date
    }

    override init() {
        super.init()
        Task {
            await checkPermission()
            await refreshCameraList()
        }
    }

    // MARK: - Permission

    /// Check camera permission status
    func checkPermission() async -> Bool {
        let status = AVCaptureDevice.authorizationStatus(for: .video)

        switch status {
        case .authorized:
            hasPermission = true
            return true
        case .notDetermined:
            hasPermission = await AVCaptureDevice.requestAccess(for: .video)
            return hasPermission
        default:
            hasPermission = false
            return false
        }
    }

    // MARK: - Camera Management

    /// Refresh list of available cameras
    func refreshCameraList() async {
        let discoverySession = AVCaptureDevice.DiscoverySession(
            deviceTypes: [.builtInWideAngleCamera, .externalUnknown],
            mediaType: .video,
            position: .unspecified
        )

        availableCameras = discoverySession.devices.map { device in
            CameraDevice(
                id: device.uniqueID,
                name: device.localizedName,
                position: device.position,
                isBuiltIn: device.deviceType == .builtInWideAngleCamera
            )
        }

        // Select default camera
        if selectedCamera == nil {
            selectedCamera = availableCameras.first
        }

        logger.debug("found \(self.availableCameras.count) cameras")
    }

    /// Select a camera by ID
    func selectCamera(id: String) {
        selectedCamera = availableCameras.first { $0.id == id }
    }

    // MARK: - Capture

    /// Start camera capture session
    func startCapture() async throws {
        guard hasPermission else {
            throw CameraError.permissionDenied
        }

        guard let camera = selectedCamera,
              let device = AVCaptureDevice(uniqueID: camera.id) else {
            throw CameraError.noCameraAvailable
        }

        let session = AVCaptureSession()
        session.sessionPreset = .high

        // Add input
        let input = try AVCaptureDeviceInput(device: device)
        guard session.canAddInput(input) else {
            throw CameraError.setupFailed("Cannot add camera input")
        }
        session.addInput(input)

        // Add output
        let output = AVCaptureVideoDataOutput()
        output.videoSettings = [kCVPixelBufferPixelFormatTypeKey as String: kCVPixelFormatType_32BGRA]
        output.setSampleBufferDelegate(self, queue: DispatchQueue(label: "camera.capture"))

        guard session.canAddOutput(output) else {
            throw CameraError.setupFailed("Cannot add video output")
        }
        session.addOutput(output)

        captureSession = session
        videoOutput = output

        session.startRunning()
        isCapturing = true

        logger.info("camera capture started device=\(camera.name)")
    }

    /// Stop camera capture session
    func stopCapture() {
        captureSession?.stopRunning()
        captureSession = nil
        videoOutput = nil
        isCapturing = false
        logger.info("camera capture stopped")
    }

    /// Capture a single frame
    func captureFrame() async throws -> CaptureResult {
        // If not capturing, start temporarily
        let wasCapturing = isCapturing
        if !wasCapturing {
            try await startCapture()
            // Wait for frame
            try await Task.sleep(nanoseconds: 500_000_000)
        }

        guard let image = lastFrame else {
            if !wasCapturing {
                stopCapture()
            }
            throw CameraError.captureFailedError
        }

        let data = try encodeImage(image)

        if !wasCapturing {
            stopCapture()
        }

        return CaptureResult(
            image: image,
            data: data,
            timestamp: Date()
        )
    }

    // MARK: - Private

    private func encodeImage(_ image: NSImage) throws -> Data {
        guard let tiffData = image.tiffRepresentation,
              let bitmap = NSBitmapImageRep(data: tiffData),
              let pngData = bitmap.representation(using: .png, properties: [:]) else {
            throw CameraError.encodingFailed
        }
        return pngData
    }
}

extension CameraCaptureService: AVCaptureVideoDataOutputSampleBufferDelegate {
    nonisolated func captureOutput(
        _ output: AVCaptureOutput,
        didOutput sampleBuffer: CMSampleBuffer,
        from connection: AVCaptureConnection
    ) {
        guard let pixelBuffer = CMSampleBufferGetImageBuffer(sampleBuffer) else { return }

        let ciImage = CIImage(cvPixelBuffer: pixelBuffer)
        let rep = NSCIImageRep(ciImage: ciImage)
        let nsImage = NSImage(size: rep.size)
        nsImage.addRepresentation(rep)

        Task { @MainActor in
            self.lastFrame = nsImage
        }
    }
}

enum CameraError: LocalizedError {
    case permissionDenied
    case noCameraAvailable
    case setupFailed(String)
    case captureFailedError
    case encodingFailed

    var errorDescription: String? {
        switch self {
        case .permissionDenied:
            return "Camera permission denied"
        case .noCameraAvailable:
            return "No camera available"
        case .setupFailed(let reason):
            return "Camera setup failed: \(reason)"
        case .captureFailedError:
            return "Failed to capture frame"
        case .encodingFailed:
            return "Failed to encode image"
        }
    }
}
