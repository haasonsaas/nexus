import AppKit
import AVFoundation
import Foundation
import OSLog
import ScreenCaptureKit
import Speech

/// Permission types supported by the PermissionManager.
enum PermissionType: String, CaseIterable, Sendable {
    case microphone
    case speechRecognition
    case accessibility
    case screenRecording
    case camera
}

/// Permission status for a single permission type.
struct PermissionStatus: Sendable {
    let type: PermissionType
    let isGranted: Bool
    let canRequest: Bool
}

/// Observable permission manager for system permissions.
/// Provides reactive permission status and request APIs.
@MainActor
@Observable
final class PermissionManager {
    static let shared = PermissionManager()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "permissions")

    // MARK: - Observable State

    private(set) var microphoneGranted = false
    private(set) var speechRecognitionGranted = false
    private(set) var accessibilityGranted = false
    private(set) var screenRecordingGranted = false
    private(set) var cameraGranted = false

    private var refreshTask: Task<Void, Never>?

    // MARK: - Computed Properties

    /// True if all voice wake permissions are granted
    var voiceWakePermissionsGranted: Bool {
        microphoneGranted && speechRecognitionGranted
    }

    /// True if all computer use permissions are granted
    var computerUsePermissionsGranted: Bool {
        accessibilityGranted && screenRecordingGranted
    }

    /// All permissions as a dictionary
    var allStatuses: [PermissionType: Bool] {
        [
            .microphone: microphoneGranted,
            .speechRecognition: speechRecognitionGranted,
            .accessibility: accessibilityGranted,
            .screenRecording: screenRecordingGranted,
            .camera: cameraGranted,
        ]
    }

    // MARK: - Initialization

    private init() {
        refreshAllStatuses()
        startPeriodicRefresh()
    }

    deinit {
        refreshTask?.cancel()
    }

    // MARK: - Status Checks

    /// Refresh all permission statuses
    func refreshAllStatuses() {
        microphoneGranted = checkMicrophoneStatus()
        speechRecognitionGranted = checkSpeechRecognitionStatus()
        accessibilityGranted = checkAccessibilityStatus()
        screenRecordingGranted = checkScreenRecordingStatus()
        cameraGranted = checkCameraStatus()

        logger.debug("permissions refreshed: mic=\(self.microphoneGranted) speech=\(self.speechRecognitionGranted) a11y=\(self.accessibilityGranted) screen=\(self.screenRecordingGranted) camera=\(self.cameraGranted)")
    }

    /// Check status for a specific permission type
    func status(for type: PermissionType) -> Bool {
        switch type {
        case .microphone: return microphoneGranted
        case .speechRecognition: return speechRecognitionGranted
        case .accessibility: return accessibilityGranted
        case .screenRecording: return screenRecordingGranted
        case .camera: return cameraGranted
        }
    }

    /// Get detailed status for a permission type
    func detailedStatus(for type: PermissionType) -> PermissionStatus {
        let granted = status(for: type)
        let canRequest: Bool

        switch type {
        case .microphone:
            canRequest = AVCaptureDevice.authorizationStatus(for: .audio) == .notDetermined
        case .speechRecognition:
            canRequest = SFSpeechRecognizer.authorizationStatus() == .notDetermined
        case .camera:
            canRequest = AVCaptureDevice.authorizationStatus(for: .video) == .notDetermined
        case .accessibility, .screenRecording:
            // These require manual System Settings action
            canRequest = false
        }

        return PermissionStatus(type: type, isGranted: granted, canRequest: canRequest)
    }

    // MARK: - Permission Requests

    /// Request microphone access
    func requestMicrophoneAccess() async -> Bool {
        let status = AVCaptureDevice.authorizationStatus(for: .audio)

        switch status {
        case .authorized:
            microphoneGranted = true
            return true
        case .notDetermined:
            let granted = await AVCaptureDevice.requestAccess(for: .audio)
            microphoneGranted = granted
            return granted
        case .denied, .restricted:
            return false
        @unknown default:
            return false
        }
    }

    /// Request speech recognition access
    func requestSpeechRecognitionAccess() async -> Bool {
        let status = SFSpeechRecognizer.authorizationStatus()

        switch status {
        case .authorized:
            speechRecognitionGranted = true
            return true
        case .notDetermined:
            let granted = await withCheckedContinuation { continuation in
                SFSpeechRecognizer.requestAuthorization { newStatus in
                    continuation.resume(returning: newStatus == .authorized)
                }
            }
            speechRecognitionGranted = granted
            return granted
        case .denied, .restricted:
            return false
        @unknown default:
            return false
        }
    }

    /// Request camera access
    func requestCameraAccess() async -> Bool {
        let status = AVCaptureDevice.authorizationStatus(for: .video)

        switch status {
        case .authorized:
            cameraGranted = true
            return true
        case .notDetermined:
            let granted = await AVCaptureDevice.requestAccess(for: .video)
            cameraGranted = granted
            return granted
        case .denied, .restricted:
            return false
        @unknown default:
            return false
        }
    }

    /// Request all voice wake permissions
    func requestVoiceWakePermissions() async -> Bool {
        let micGranted = await requestMicrophoneAccess()
        let speechGranted = await requestSpeechRecognitionAccess()
        return micGranted && speechGranted
    }

    /// Prompt for accessibility permission (opens System Settings)
    func promptAccessibilityPermission() {
        let options = [kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: true]
        _ = AXIsProcessTrustedWithOptions(options as CFDictionary)
        refreshAllStatuses()
    }

    // MARK: - System Settings

    /// Open System Settings to the appropriate privacy pane
    func openSystemSettings(for type: PermissionType) {
        let urlStrings: [String]

        switch type {
        case .microphone:
            urlStrings = [
                "x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone",
                "x-apple.systempreferences:com.apple.preference.security",
            ]
        case .speechRecognition:
            urlStrings = [
                "x-apple.systempreferences:com.apple.preference.security?Privacy_SpeechRecognition",
                "x-apple.systempreferences:com.apple.preference.security",
            ]
        case .accessibility:
            urlStrings = [
                "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility",
                "x-apple.systempreferences:com.apple.preference.security",
            ]
        case .screenRecording:
            urlStrings = [
                "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture",
                "x-apple.systempreferences:com.apple.preference.security",
            ]
        case .camera:
            urlStrings = [
                "x-apple.systempreferences:com.apple.preference.security?Privacy_Camera",
                "x-apple.systempreferences:com.apple.preference.security",
            ]
        }

        for candidate in urlStrings {
            if let url = URL(string: candidate), NSWorkspace.shared.open(url) {
                logger.debug("opened system settings for \(type.rawValue)")
                return
            }
        }

        logger.warning("failed to open system settings for \(type.rawValue)")
    }

    // MARK: - Private Methods

    private func checkMicrophoneStatus() -> Bool {
        AVCaptureDevice.authorizationStatus(for: .audio) == .authorized
    }

    private func checkSpeechRecognitionStatus() -> Bool {
        SFSpeechRecognizer.authorizationStatus() == .authorized
    }

    private func checkAccessibilityStatus() -> Bool {
        AXIsProcessTrusted()
    }

    private func checkScreenRecordingStatus() -> Bool {
        // Check if we have screen recording permission by attempting to get shareable content
        // This is a heuristic - there's no direct API to check screen recording permission
        if #available(macOS 12.3, *) {
            // On newer macOS, check via ScreenCaptureKit
            var hasPermission = false
            let semaphore = DispatchSemaphore(value: 0)

            Task {
                do {
                    _ = try await SCShareableContent.excludingDesktopWindows(false, onScreenWindowsOnly: true)
                    hasPermission = true
                } catch {
                    hasPermission = false
                }
                semaphore.signal()
            }

            _ = semaphore.wait(timeout: .now() + 1)
            return hasPermission
        } else {
            // Fallback: check if CGWindowListCopyWindowInfo works
            let windowList = CGWindowListCopyWindowInfo([.optionOnScreenOnly], kCGNullWindowID)
            return windowList != nil
        }
    }

    private func checkCameraStatus() -> Bool {
        AVCaptureDevice.authorizationStatus(for: .video) == .authorized
    }

    private func startPeriodicRefresh() {
        refreshTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(5))
                await self?.refreshAllStatuses()
            }
        }
    }
}

// MARK: - Static Convenience Methods

extension PermissionManager {
    /// Quick check for microphone permission (static convenience)
    static func microphonePermissionGranted() -> Bool {
        shared.microphoneGranted
    }

    /// Quick check for speech recognition permission (static convenience)
    static func speechRecognitionPermissionGranted() -> Bool {
        shared.speechRecognitionGranted
    }

    /// Quick check for voice wake permissions (static convenience)
    static func voiceWakePermissionsGranted() -> Bool {
        shared.voiceWakePermissionsGranted
    }

    /// Open system preferences (static convenience)
    static func openSystemPreferences(for type: PermissionType) {
        shared.openSystemSettings(for: type)
    }
}
