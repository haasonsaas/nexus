import Foundation
import IOKit

/// Provides stable instance identification for this macOS installation.
/// Used by PresenceReporter and other identity-related features.
enum InstanceIdentity {
    /// Stable UUID for this installation (persisted in UserDefaults)
    static var instanceId: String {
        let key = "NexusInstanceId"
        if let existing = UserDefaults.standard.string(forKey: key) {
            return existing
        }
        let newId = UUID().uuidString
        UserDefaults.standard.set(newId, forKey: key)
        return newId
    }

    /// Human-readable display name (computer name)
    static var displayName: String {
        Host.current().localizedName ?? ProcessInfo.processInfo.hostName
    }

    /// Mac model identifier (e.g., "MacBookPro18,1")
    static var modelIdentifier: String? {
        var size: size_t = 0
        sysctlbyname("hw.model", nil, &size, nil, 0)
        guard size > 0 else { return nil }
        var model = [CChar](repeating: 0, count: size)
        sysctlbyname("hw.model", &model, &size, nil, 0)
        return String(cString: model)
    }

    /// Serial number (if accessible, may be empty on newer macOS)
    static var serialNumber: String? {
        let service = IOServiceGetMatchingService(kIOMainPortDefault,
            IOServiceMatching("IOPlatformExpertDevice"))
        defer { IOObjectRelease(service) }
        guard let property = IORegistryEntryCreateCFProperty(service,
            "IOPlatformSerialNumber" as CFString, kCFAllocatorDefault, 0) else {
            return nil
        }
        return property.takeRetainedValue() as? String
    }
}
