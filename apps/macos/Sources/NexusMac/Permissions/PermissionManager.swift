import AppKit
import AVFoundation
import Foundation
import Speech

/// Permission types supported by the PermissionManager.
enum PermissionType: CaseIterable, Sendable {
    case microphone
    case speechRecognition
    case accessibility
}

/// Enum-based permission manager for voice features (VoiceWake and TalkMode).
enum PermissionManager {
    // MARK: - Permission Status Checks

    /// Returns true if microphone access has been granted.
    static func microphonePermissionGranted() -> Bool {
        AVCaptureDevice.authorizationStatus(for: .audio) == .authorized
    }

    /// Returns true if speech recognition permission has been granted.
    static func speechRecognitionPermissionGranted() -> Bool {
        SFSpeechRecognizer.authorizationStatus() == .authorized
    }

    /// Returns true if both microphone and speech recognition permissions are granted.
    /// Required for VoiceWake and TalkMode features.
    static func voiceWakePermissionsGranted() -> Bool {
        microphonePermissionGranted() && speechRecognitionPermissionGranted()
    }

    // MARK: - Permission Requests

    /// Requests microphone access. Returns true if granted.
    /// If permission was previously denied, this will not show a prompt.
    static func requestMicrophoneAccess() async -> Bool {
        let status = AVCaptureDevice.authorizationStatus(for: .audio)

        switch status {
        case .authorized:
            return true
        case .notDetermined:
            return await AVCaptureDevice.requestAccess(for: .audio)
        case .denied, .restricted:
            return false
        @unknown default:
            return false
        }
    }

    /// Requests speech recognition permission. Returns true if granted.
    /// If permission was previously denied, this will not show a prompt.
    static func requestSpeechRecognitionAccess() async -> Bool {
        let status = SFSpeechRecognizer.authorizationStatus()

        switch status {
        case .authorized:
            return true
        case .notDetermined:
            return await withCheckedContinuation { continuation in
                SFSpeechRecognizer.requestAuthorization { newStatus in
                    continuation.resume(returning: newStatus == .authorized)
                }
            }
        case .denied, .restricted:
            return false
        @unknown default:
            return false
        }
    }

    /// Requests both microphone and speech recognition permissions.
    /// Returns true only if both are granted.
    static func requestVoiceWakePermissions() async -> Bool {
        let micGranted = await requestMicrophoneAccess()
        let speechGranted = await requestSpeechRecognitionAccess()
        return micGranted && speechGranted
    }

    // MARK: - System Preferences

    /// Opens System Preferences/Settings to the appropriate privacy pane for the given permission type.
    @MainActor
    static func openSystemPreferences(for permissionType: PermissionType) {
        let urlStrings: [String]

        switch permissionType {
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
        }

        for candidate in urlStrings {
            if let url = URL(string: candidate), NSWorkspace.shared.open(url) {
                return
            }
        }
    }

    // MARK: - Status

    /// Returns the current authorization status for a permission type.
    static func status(for permissionType: PermissionType) -> Bool {
        switch permissionType {
        case .microphone:
            return microphonePermissionGranted()
        case .speechRecognition:
            return speechRecognitionPermissionGranted()
        case .accessibility:
            return AXIsProcessTrusted()
        }
    }

    /// Returns a dictionary of all permission statuses.
    static func allStatuses() -> [PermissionType: Bool] {
        var results: [PermissionType: Bool] = [:]
        for permissionType in PermissionType.allCases {
            results[permissionType] = status(for: permissionType)
        }
        return results
    }
}
