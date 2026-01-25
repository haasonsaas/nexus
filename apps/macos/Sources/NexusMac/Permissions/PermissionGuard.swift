import AppKit
import AVFoundation
import Contacts
import CoreLocation
import EventKit
import Foundation
import OSLog
import Photos
import ScreenCaptureKit

/// Permission types that can be guarded
enum GuardedPermission: String, CaseIterable, Sendable {
    case screenRecording
    case camera
    case microphone
    case accessibility
    case location
    case contacts
    case calendar
    case photos

    /// Human-readable display name
    var displayName: String {
        switch self {
        case .screenRecording: return "Screen Recording"
        case .camera: return "Camera"
        case .microphone: return "Microphone"
        case .accessibility: return "Accessibility"
        case .location: return "Location"
        case .contacts: return "Contacts"
        case .calendar: return "Calendar"
        case .photos: return "Photos"
        }
    }

    /// Description of what the permission enables
    var permissionDescription: String {
        switch self {
        case .screenRecording:
            return "Allows Nexus to capture screen content for AI analysis"
        case .camera:
            return "Allows Nexus to capture images from your camera"
        case .microphone:
            return "Allows Nexus to record audio for voice commands"
        case .accessibility:
            return "Allows Nexus to control your computer and read UI elements"
        case .location:
            return "Allows Nexus to access your location for context"
        case .contacts:
            return "Allows Nexus to access your contacts"
        case .calendar:
            return "Allows Nexus to access your calendar events"
        case .photos:
            return "Allows Nexus to access your photo library"
        }
    }
}

/// Result of a permission check
enum PermissionCheckResult: String, Sendable {
    case granted
    case denied
    case notDetermined
    case restricted

    /// Whether the permission allows the operation to proceed
    var allowsOperation: Bool {
        self == .granted
    }

    /// Whether the permission can be requested programmatically
    var canRequest: Bool {
        self == .notDetermined
    }
}

/// Guards operations that require specific permissions.
/// Provides a unified API for checking, requesting, and guarding permission-gated operations.
@MainActor
@Observable
final class PermissionGuard {
    static let shared = PermissionGuard()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "permission-guard")

    // MARK: - Observable State

    /// Cached permission states for reactive UI updates
    private(set) var permissionStates: [GuardedPermission: PermissionCheckResult] = [:]

    /// Last time permissions were checked
    private(set) var lastCheckedAt: Date?

    /// Whether a permission check is currently in progress
    private(set) var isChecking = false

    // MARK: - Private State

    private var locationManager: CLLocationManager?
    private var locationDelegate: LocationDelegate?
    private var refreshTask: Task<Void, Never>?

    // MARK: - Computed Properties

    /// All required permissions are granted
    var allPermissionsGranted: Bool {
        GuardedPermission.allCases.allSatisfy { permission in
            permissionStates[permission] == .granted
        }
    }

    /// Permissions that are denied or restricted
    var deniedPermissions: [GuardedPermission] {
        GuardedPermission.allCases.filter { permission in
            let state = permissionStates[permission]
            return state == .denied || state == .restricted
        }
    }

    /// Permissions that have not been determined yet
    var undeterminedPermissions: [GuardedPermission] {
        GuardedPermission.allCases.filter { permission in
            permissionStates[permission] == .notDetermined
        }
    }

    // MARK: - Initialization

    private init() {
        logger.debug("permission guard initialized")

        // Initialize with unknown states
        for permission in GuardedPermission.allCases {
            permissionStates[permission] = .notDetermined
        }

        // Setup location manager
        setupLocationManager()

        // Start periodic refresh
        startPeriodicRefresh()
    }

    deinit {
        refreshTask?.cancel()
    }

    // MARK: - Checking

    /// Check the current status of a permission
    func check(_ permission: GuardedPermission) async -> PermissionCheckResult {
        let result: PermissionCheckResult

        switch permission {
        case .screenRecording:
            result = await checkScreenRecording()
        case .camera:
            result = checkCamera()
        case .microphone:
            result = checkMicrophone()
        case .accessibility:
            result = checkAccessibility()
        case .location:
            result = checkLocation()
        case .contacts:
            result = checkContacts()
        case .calendar:
            result = checkCalendar()
        case .photos:
            result = checkPhotos()
        }

        permissionStates[permission] = result
        logger.debug("permission check: \(permission.rawValue) = \(result.rawValue)")
        return result
    }

    /// Check all permissions and update cached states
    func checkAll() async {
        guard !isChecking else {
            logger.debug("permission check already in progress, skipping")
            return
        }

        isChecking = true
        defer {
            isChecking = false
            lastCheckedAt = Date()
        }

        logger.info("checking all permissions")

        for permission in GuardedPermission.allCases {
            _ = await check(permission)
        }

        logger.info("permission check complete: \(self.permissionStates.filter { $0.value == .granted }.count)/\(GuardedPermission.allCases.count) granted")
    }

    /// Check multiple permissions at once
    func check(_ permissions: [GuardedPermission]) async -> [GuardedPermission: PermissionCheckResult] {
        var results: [GuardedPermission: PermissionCheckResult] = [:]

        for permission in permissions {
            results[permission] = await check(permission)
        }

        return results
    }

    // MARK: - Guarded Execution

    /// Execute an operation if the required permission is granted
    /// - Parameters:
    ///   - permission: The permission required for the operation
    ///   - onDenied: Optional fallback closure called when permission is denied
    ///   - operation: The operation to execute if permission is granted
    /// - Returns: The result of the operation or fallback
    func execute<T>(
        requiring permission: GuardedPermission,
        onDenied: (() -> T)? = nil,
        operation: () async throws -> T
    ) async throws -> T {
        let status = await check(permission)

        switch status {
        case .granted:
            logger.debug("executing guarded operation for \(permission.rawValue)")
            return try await operation()

        case .denied, .restricted:
            logger.warning("permission denied for \(permission.rawValue), status=\(status.rawValue)")
            if let fallback = onDenied {
                return fallback()
            }
            throw PermissionError.denied(permission)

        case .notDetermined:
            logger.info("permission not determined for \(permission.rawValue), requesting")
            let granted = await request(permission)
            if granted {
                return try await operation()
            }
            throw PermissionError.notDetermined(permission)
        }
    }

    /// Execute an operation requiring multiple permissions
    /// - Parameters:
    ///   - permissions: The permissions required for the operation
    ///   - operation: The operation to execute if all permissions are granted
    /// - Returns: The result of the operation
    func executeMultiple<T>(
        requiring permissions: [GuardedPermission],
        operation: () async throws -> T
    ) async throws -> T {
        var missingPermissions: [GuardedPermission] = []

        for permission in permissions {
            let status = await check(permission)
            if status != .granted {
                missingPermissions.append(permission)
            }
        }

        guard missingPermissions.isEmpty else {
            logger.warning("multiple permissions denied: \(missingPermissions.map(\.rawValue).joined(separator: ", "))")
            throw PermissionError.multiplePermissionsDenied(missingPermissions)
        }

        logger.debug("executing guarded operation requiring \(permissions.count) permissions")
        return try await operation()
    }

    /// Execute an operation with a timeout if permission is granted
    func executeWithTimeout<T>(
        requiring permission: GuardedPermission,
        timeout: Duration = .seconds(30),
        operation: () async throws -> T
    ) async throws -> T {
        let status = await check(permission)

        guard status == .granted else {
            throw PermissionError.denied(permission)
        }

        return try await withThrowingTaskGroup(of: T.self) { group in
            group.addTask {
                try await operation()
            }

            group.addTask {
                try await Task.sleep(for: timeout)
                throw PermissionError.timeout(permission)
            }

            let result = try await group.next()!
            group.cancelAll()
            return result
        }
    }

    // MARK: - Requesting

    /// Request a permission, returning whether it was granted
    @discardableResult
    func request(_ permission: GuardedPermission) async -> Bool {
        logger.info("requesting permission: \(permission.rawValue)")

        let granted: Bool

        switch permission {
        case .camera:
            granted = await requestCamera()
        case .microphone:
            granted = await requestMicrophone()
        case .location:
            granted = await requestLocation()
        case .contacts:
            granted = await requestContacts()
        case .calendar:
            granted = await requestCalendar()
        case .photos:
            granted = await requestPhotos()
        case .screenRecording:
            // Screen recording requires manual System Settings action
            openSystemPreferences(for: .screenRecording)
            granted = false
        case .accessibility:
            // Accessibility requires manual System Settings action
            promptAccessibilityPermission()
            granted = false
        }

        // Update cached state
        _ = await check(permission)

        logger.info("permission request result: \(permission.rawValue) = \(granted)")
        return granted
    }

    /// Request multiple permissions in sequence
    func request(_ permissions: [GuardedPermission]) async -> [GuardedPermission: Bool] {
        var results: [GuardedPermission: Bool] = [:]

        for permission in permissions {
            results[permission] = await request(permission)
        }

        return results
    }

    // MARK: - Individual Permission Checks

    private func checkScreenRecording() async -> PermissionCheckResult {
        // Screen recording permission can only be checked by attempting capture
        do {
            let content = try await SCShareableContent.excludingDesktopWindows(false, onScreenWindowsOnly: true)
            // If we can get content and displays are available, permission is granted
            return content.displays.isEmpty ? .denied : .granted
        } catch let error as NSError {
            // Error domain SCStreamErrorDomain with code -3801 means permission denied
            if error.domain == "com.apple.ScreenCaptureKit.SCStreamErrorDomain" {
                return .denied
            }
            logger.debug("screen recording check error: \(error.localizedDescription)")
            return .denied
        }
    }

    private func checkCamera() -> PermissionCheckResult {
        switch AVCaptureDevice.authorizationStatus(for: .video) {
        case .authorized:
            return .granted
        case .denied:
            return .denied
        case .notDetermined:
            return .notDetermined
        case .restricted:
            return .restricted
        @unknown default:
            return .notDetermined
        }
    }

    private func checkMicrophone() -> PermissionCheckResult {
        switch AVCaptureDevice.authorizationStatus(for: .audio) {
        case .authorized:
            return .granted
        case .denied:
            return .denied
        case .notDetermined:
            return .notDetermined
        case .restricted:
            return .restricted
        @unknown default:
            return .notDetermined
        }
    }

    private func checkAccessibility() -> PermissionCheckResult {
        let trusted = AXIsProcessTrusted()
        return trusted ? .granted : .denied
    }

    private func checkLocation() -> PermissionCheckResult {
        let status = locationManager?.authorizationStatus ?? .notDetermined

        switch status {
        case .authorizedAlways, .authorized:
            return .granted
        case .denied:
            return .denied
        case .notDetermined:
            return .notDetermined
        case .restricted:
            return .restricted
        @unknown default:
            return .notDetermined
        }
    }

    private func checkContacts() -> PermissionCheckResult {
        let status = CNContactStore.authorizationStatus(for: .contacts)

        switch status {
        case .authorized:
            return .granted
        case .denied:
            return .denied
        case .notDetermined:
            return .notDetermined
        case .restricted:
            return .restricted
        case .limited:
            return .granted // Limited access is still some access
        @unknown default:
            return .notDetermined
        }
    }

    private func checkCalendar() -> PermissionCheckResult {
        let status = EKEventStore.authorizationStatus(for: .event)

        switch status {
        case .authorized, .fullAccess, .writeOnly:
            return .granted
        case .denied:
            return .denied
        case .notDetermined:
            return .notDetermined
        case .restricted:
            return .restricted
        @unknown default:
            return .notDetermined
        }
    }

    private func checkPhotos() -> PermissionCheckResult {
        let status = PHPhotoLibrary.authorizationStatus(for: .readWrite)

        switch status {
        case .authorized:
            return .granted
        case .denied:
            return .denied
        case .notDetermined:
            return .notDetermined
        case .restricted:
            return .restricted
        case .limited:
            return .granted // Limited access is still some access
        @unknown default:
            return .notDetermined
        }
    }

    // MARK: - Request Implementations

    private func requestCamera() async -> Bool {
        let status = AVCaptureDevice.authorizationStatus(for: .video)

        if status == .notDetermined {
            return await AVCaptureDevice.requestAccess(for: .video)
        }

        return status == .authorized
    }

    private func requestMicrophone() async -> Bool {
        let status = AVCaptureDevice.authorizationStatus(for: .audio)

        if status == .notDetermined {
            return await AVCaptureDevice.requestAccess(for: .audio)
        }

        return status == .authorized
    }

    private func requestLocation() async -> Bool {
        let status = locationManager?.authorizationStatus ?? .notDetermined

        guard status == .notDetermined else {
            return status == .authorizedAlways || status == .authorized
        }

        // Request authorization
        locationManager?.requestWhenInUseAuthorization()

        // Wait for response with timeout
        return await withCheckedContinuation { continuation in
            locationDelegate?.authorizationCallback = { newStatus in
                continuation.resume(returning: newStatus == .authorizedAlways || newStatus == .authorized)
            }

            // Timeout after 30 seconds
            Task {
                try? await Task.sleep(for: .seconds(30))
                self.locationDelegate?.authorizationCallback?(self.locationManager?.authorizationStatus ?? .notDetermined)
            }
        }
    }

    private func requestContacts() async -> Bool {
        let store = CNContactStore()

        do {
            return try await store.requestAccess(for: .contacts)
        } catch {
            logger.error("contacts permission request failed: \(error.localizedDescription)")
            return false
        }
    }

    private func requestCalendar() async -> Bool {
        let store = EKEventStore()

        do {
            return try await store.requestFullAccessToEvents()
        } catch {
            logger.error("calendar permission request failed: \(error.localizedDescription)")
            return false
        }
    }

    private func requestPhotos() async -> Bool {
        let status = await PHPhotoLibrary.requestAuthorization(for: .readWrite)
        return status == .authorized || status == .limited
    }

    private func promptAccessibilityPermission() {
        let options = [kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: true] as CFDictionary
        _ = AXIsProcessTrustedWithOptions(options)
    }

    // MARK: - System Preferences

    /// Open System Preferences to the appropriate privacy pane
    func openSystemPreferences(for permission: GuardedPermission) {
        let urlStrings: [String]

        switch permission {
        case .screenRecording:
            urlStrings = [
                "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture",
                "x-apple.systempreferences:com.apple.preference.security",
            ]
        case .accessibility:
            urlStrings = [
                "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility",
                "x-apple.systempreferences:com.apple.preference.security",
            ]
        case .camera:
            urlStrings = [
                "x-apple.systempreferences:com.apple.preference.security?Privacy_Camera",
                "x-apple.systempreferences:com.apple.preference.security",
            ]
        case .microphone:
            urlStrings = [
                "x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone",
                "x-apple.systempreferences:com.apple.preference.security",
            ]
        case .location:
            urlStrings = [
                "x-apple.systempreferences:com.apple.preference.security?Privacy_LocationServices",
                "x-apple.systempreferences:com.apple.preference.security",
            ]
        case .contacts:
            urlStrings = [
                "x-apple.systempreferences:com.apple.preference.security?Privacy_Contacts",
                "x-apple.systempreferences:com.apple.preference.security",
            ]
        case .calendar:
            urlStrings = [
                "x-apple.systempreferences:com.apple.preference.security?Privacy_Calendars",
                "x-apple.systempreferences:com.apple.preference.security",
            ]
        case .photos:
            urlStrings = [
                "x-apple.systempreferences:com.apple.preference.security?Privacy_Photos",
                "x-apple.systempreferences:com.apple.preference.security",
            ]
        }

        for candidate in urlStrings {
            if let url = URL(string: candidate), NSWorkspace.shared.open(url) {
                logger.info("opened system preferences for \(permission.rawValue)")
                return
            }
        }

        logger.warning("failed to open system preferences for \(permission.rawValue)")
    }

    // MARK: - Private Setup

    private func setupLocationManager() {
        locationManager = CLLocationManager()
        locationDelegate = LocationDelegate()
        locationManager?.delegate = locationDelegate
    }

    private func startPeriodicRefresh() {
        refreshTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(10))
                await self?.checkAll()
            }
        }
    }
}

// MARK: - Location Delegate

private class LocationDelegate: NSObject, CLLocationManagerDelegate {
    var authorizationCallback: ((CLAuthorizationStatus) -> Void)?

    func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        authorizationCallback?(manager.authorizationStatus)
        authorizationCallback = nil
    }
}

// MARK: - Permission Error

/// Errors that can occur during permission-guarded operations
enum PermissionError: LocalizedError {
    case denied(GuardedPermission)
    case notDetermined(GuardedPermission)
    case restricted(GuardedPermission)
    case multiplePermissionsDenied([GuardedPermission])
    case timeout(GuardedPermission)

    var errorDescription: String? {
        switch self {
        case .denied(let permission):
            return "\(permission.displayName) permission denied. Please grant access in System Settings."
        case .notDetermined(let permission):
            return "\(permission.displayName) permission not determined. Please grant access when prompted."
        case .restricted(let permission):
            return "\(permission.displayName) permission restricted by system policy."
        case .multiplePermissionsDenied(let permissions):
            let names = permissions.map(\.displayName).joined(separator: ", ")
            return "Multiple permissions denied: \(names)"
        case .timeout(let permission):
            return "\(permission.displayName) operation timed out."
        }
    }

    var recoverySuggestion: String? {
        switch self {
        case .denied(let permission), .restricted(let permission):
            return "Open System Settings > Privacy & Security > \(permission.displayName) and grant access to Nexus."
        case .notDetermined:
            return "Please grant access when the permission prompt appears."
        case .multiplePermissionsDenied(let permissions):
            let names = permissions.map(\.displayName).joined(separator: ", ")
            return "Open System Settings and grant access for: \(names)"
        case .timeout:
            return "Try the operation again."
        }
    }

    var permission: GuardedPermission? {
        switch self {
        case .denied(let p), .notDetermined(let p), .restricted(let p), .timeout(let p):
            return p
        case .multiplePermissionsDenied:
            return nil
        }
    }
}

// MARK: - Static Convenience

extension PermissionGuard {
    /// Quick check if a permission is granted
    static func isGranted(_ permission: GuardedPermission) async -> Bool {
        await shared.check(permission) == .granted
    }

    /// Quick execution of a guarded operation
    static func execute<T>(
        requiring permission: GuardedPermission,
        operation: () async throws -> T
    ) async throws -> T {
        try await shared.execute(requiring: permission, operation: operation)
    }

    /// Open system preferences for a permission
    static func openSettings(for permission: GuardedPermission) {
        shared.openSystemPreferences(for: permission)
    }
}
