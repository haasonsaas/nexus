import AppKit
import CoreGraphics
import Foundation
import OSLog
import ScreenCaptureKit

/// Enhanced screen capture service for computer use agents.
/// Provides high-quality screen capture with annotation support.
@MainActor
final class ScreenCaptureService {
    static let shared = ScreenCaptureService()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "screen.capture")

    struct CaptureOptions {
        var scale: CGFloat = 1.0
        var includesCursor: Bool = true
        var captureRect: CGRect?
        var displayID: CGDirectDisplayID?
        var format: ImageFormat = .png

        enum ImageFormat {
            case png
            case jpeg(quality: CGFloat)
        }
    }

    struct CaptureResult {
        let image: NSImage
        let data: Data
        let bounds: CGRect
        let timestamp: Date
    }

    /// Capture the entire screen or a specific region
    func capture(options: CaptureOptions = CaptureOptions()) async throws -> CaptureResult {
        let displayID = options.displayID ?? CGMainDisplayID()

        guard let image = captureDisplay(displayID, options: options) else {
            throw CaptureError.captureFailed
        }

        let data = try encodeImage(image, format: options.format)
        let bounds = CGDisplayBounds(displayID)

        logger.debug("screen captured size=\(Int(bounds.width))x\(Int(bounds.height))")

        return CaptureResult(
            image: image,
            data: data,
            bounds: bounds,
            timestamp: Date()
        )
    }

    /// Capture a specific region of the screen
    func captureRegion(rect: CGRect, options: CaptureOptions = CaptureOptions()) async throws -> CaptureResult {
        var regionOptions = options
        regionOptions.captureRect = rect

        let displayID = regionOptions.displayID ?? CGMainDisplayID()

        guard let cgImage = CGDisplayCreateImage(displayID, rect: rect) else {
            throw CaptureError.captureFailed
        }

        let size = NSSize(width: rect.width, height: rect.height)
        let image = NSImage(cgImage: cgImage, size: size)
        let data = try encodeImage(image, format: regionOptions.format)

        logger.debug("region captured at (\(Int(rect.origin.x)),\(Int(rect.origin.y))) size=\(Int(rect.width))x\(Int(rect.height))")

        return CaptureResult(
            image: image,
            data: data,
            bounds: rect,
            timestamp: Date()
        )
    }

    /// Capture a specific window
    func captureWindow(windowID: CGWindowID, options: CaptureOptions = CaptureOptions()) async throws -> CaptureResult {
        let shareableContent = try await SCShareableContent.current
        guard let window = shareableContent.windows.first(where: { $0.windowID == windowID }) else {
            throw CaptureError.windowNotFound
        }

        let rect = window.frame
        let filter = SCContentFilter(desktopIndependentWindow: window)
        let scale = CGFloat(filter.pointPixelScale)

        let config = SCStreamConfiguration()
        config.width = max(1, Int(rect.width * scale))
        config.height = max(1, Int(rect.height * scale))
        config.showsCursor = options.includesCursor
        config.scalesToFit = false
        config.capturesAudio = false
        config.ignoreShadowsSingleWindow = true

        let cgImage = try await captureWindowImage(filter: filter, config: config)
        let image = NSImage(cgImage: cgImage, size: NSSize(width: rect.width, height: rect.height))
        let data = try encodeImage(image, format: options.format)

        logger.debug("window captured id=\(windowID) size=\(Int(rect.width))x\(Int(rect.height))")

        return CaptureResult(
            image: image,
            data: data,
            bounds: rect,
            timestamp: Date()
        )
    }

    /// List all windows suitable for capture
    func listWindows() -> [WindowInfo] {
        guard let windowList = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] else {
            return []
        }

        return windowList.compactMap { info -> WindowInfo? in
            guard let windowID = info[kCGWindowNumber as String] as? CGWindowID,
                  let ownerName = info[kCGWindowOwnerName as String] as? String,
                  let bounds = info[kCGWindowBounds as String] as? [String: CGFloat],
                  let width = bounds["Width"],
                  let height = bounds["Height"],
                  width > 10, height > 10 else {
                return nil
            }

            return WindowInfo(
                windowID: windowID,
                ownerName: ownerName,
                title: info[kCGWindowName as String] as? String,
                bounds: CGRect(
                    x: bounds["X"] ?? 0,
                    y: bounds["Y"] ?? 0,
                    width: width,
                    height: height
                ),
                layer: info[kCGWindowLayer as String] as? Int ?? 0
            )
        }
    }

    /// List available displays
    func listDisplays() -> [DisplayInfo] {
        var displayCount: UInt32 = 0
        CGGetActiveDisplayList(0, nil, &displayCount)

        var displays = [CGDirectDisplayID](repeating: 0, count: Int(displayCount))
        CGGetActiveDisplayList(displayCount, &displays, &displayCount)

        return displays.enumerated().map { index, displayID in
            let bounds = CGDisplayBounds(displayID)
            let isMain = CGDisplayIsMain(displayID) != 0

            return DisplayInfo(
                displayID: displayID,
                bounds: bounds,
                isMain: isMain,
                index: index
            )
        }
    }

    // MARK: - Private

    private func captureDisplay(_ displayID: CGDirectDisplayID, options: CaptureOptions) -> NSImage? {
        let bounds = options.captureRect ?? CGDisplayBounds(displayID)

        guard let cgImage = CGDisplayCreateImage(displayID, rect: bounds) else {
            return nil
        }

        var size = NSSize(width: bounds.width, height: bounds.height)
        if options.scale != 1.0 {
            size.width *= options.scale
            size.height *= options.scale
        }

        return NSImage(cgImage: cgImage, size: size)
    }

    private func captureWindowImage(filter: SCContentFilter, config: SCStreamConfiguration) async throws -> CGImage {
        try await withCheckedThrowingContinuation { continuation in
            SCScreenshotManager.captureImage(contentFilter: filter, configuration: config) { image, error in
                if let error {
                    continuation.resume(throwing: error)
                    return
                }
                guard let image else {
                    continuation.resume(throwing: CaptureError.captureFailed)
                    return
                }
                continuation.resume(returning: image)
            }
        }
    }

    private func encodeImage(_ image: NSImage, format: CaptureOptions.ImageFormat) throws -> Data {
        guard let tiffData = image.tiffRepresentation,
              let bitmap = NSBitmapImageRep(data: tiffData) else {
            throw CaptureError.encodingFailed
        }

        let data: Data?
        switch format {
        case .png:
            data = bitmap.representation(using: .png, properties: [:])
        case .jpeg(let quality):
            data = bitmap.representation(using: .jpeg, properties: [.compressionFactor: quality])
        }

        guard let imageData = data else {
            throw CaptureError.encodingFailed
        }

        return imageData
    }
}

struct WindowInfo: Identifiable {
    let windowID: CGWindowID
    let ownerName: String
    let title: String?
    let bounds: CGRect
    let layer: Int

    var id: CGWindowID { windowID }

    var displayTitle: String {
        if let title, !title.isEmpty {
            return "\(ownerName) - \(title)"
        }
        return ownerName
    }
}

struct DisplayInfo: Identifiable {
    let displayID: CGDirectDisplayID
    let bounds: CGRect
    let isMain: Bool
    let index: Int

    var id: CGDirectDisplayID { displayID }

    var displayName: String {
        isMain ? "Main Display" : "Display \(index + 1)"
    }
}

enum CaptureError: LocalizedError {
    case captureFailed
    case windowNotFound
    case encodingFailed
    case permissionDenied

    var errorDescription: String? {
        switch self {
        case .captureFailed:
            return "Failed to capture screen"
        case .windowNotFound:
            return "Window not found"
        case .encodingFailed:
            return "Failed to encode image"
        case .permissionDenied:
            return "Screen recording permission not granted"
        }
    }
}
