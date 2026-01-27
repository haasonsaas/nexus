import AppKit
import Foundation
import IOKit
import IOKit.pwr_mgt
import IOKit.ps
import OSLog

/// Deep integration with macOS system features.
/// Provides power management, display control, and system status.
@MainActor
@Observable
final class SystemIntegration {
    static let shared = SystemIntegration()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "system")

    private(set) var systemStatus: SystemStatus = SystemStatus()
    private var sleepAssertionID: IOPMAssertionID = 0
    private var updateTimer: Timer?

    struct SystemStatus {
        var isOnBattery: Bool = false
        var batteryLevel: Int?
        var isCharging: Bool = false
        var cpuUsage: Double = 0
        var memoryUsage: Double = 0
        var thermalState: ThermalState = .nominal
        var isDisplayAsleep: Bool = false
        var uptime: TimeInterval = 0

        enum ThermalState: String {
            case nominal
            case fair
            case serious
            case critical
        }
    }

    // MARK: - Monitoring

    /// Start monitoring system status
    func startMonitoring() {
        updateSystemStatus()
        updateTimer = Timer.scheduledTimer(withTimeInterval: 30, repeats: true) { [weak self] _ in
            Task { @MainActor in
                self?.updateSystemStatus()
            }
        }
        logger.info("system monitoring started")
    }

    /// Stop monitoring
    func stopMonitoring() {
        updateTimer?.invalidate()
        updateTimer = nil
    }

    private func updateSystemStatus() {
        systemStatus.isOnBattery = isOnBattery()
        systemStatus.batteryLevel = getBatteryLevel()
        systemStatus.isCharging = isCharging()
        systemStatus.cpuUsage = getCPUUsage()
        systemStatus.memoryUsage = getMemoryUsage()
        systemStatus.thermalState = getThermalState()
        systemStatus.uptime = ProcessInfo.processInfo.systemUptime
    }

    // MARK: - Power Management

    /// Prevent system sleep
    func preventSleep(reason: String) -> Bool {
        let result = IOPMAssertionCreateWithName(
            kIOPMAssertPreventUserIdleSystemSleep as CFString,
            IOPMAssertionLevel(kIOPMAssertionLevelOn),
            reason as CFString,
            &sleepAssertionID
        )

        if result == kIOReturnSuccess {
            logger.info("sleep prevention enabled: \(reason)")
            return true
        }
        return false
    }

    /// Allow system to sleep again
    func allowSleep() {
        if sleepAssertionID != 0 {
            IOPMAssertionRelease(sleepAssertionID)
            sleepAssertionID = 0
            logger.info("sleep prevention disabled")
        }
    }

    /// Put display to sleep
    func sleepDisplay() {
        let port = IORegistryEntryFromPath(kIOMainPortDefault, "IOService:/IOResources/IODisplayWrangler")
        if port != 0 {
            IORegistryEntrySetCFProperty(port, "IORequestIdle" as CFString, true as CFBoolean)
            IOObjectRelease(port)
            logger.info("display sleep triggered")
        }
    }

    /// Wake display
    func wakeDisplay() {
        // Moving mouse wakes display
        let point = NSEvent.mouseLocation
        CGWarpMouseCursorPosition(CGPoint(x: point.x + 1, y: point.y))
        CGWarpMouseCursorPosition(point)
        logger.info("display wake triggered")
    }

    // MARK: - Display Control

    /// Get current display brightness
    func getDisplayBrightness() -> Float? {
        var brightness: Float = 0
        var iterator: io_iterator_t = 0

        let result = IOServiceGetMatchingServices(kIOMainPortDefault, IOServiceMatching("IODisplayConnect"), &iterator)
        guard result == kIOReturnSuccess else { return nil }

        let service = IOIteratorNext(iterator)
        IOObjectRelease(iterator)
        guard service != 0 else { return nil }

        IODisplayGetFloatParameter(service, 0, kIODisplayBrightnessKey as CFString, &brightness)
        IOObjectRelease(service)

        return brightness
    }

    /// Set display brightness
    func setDisplayBrightness(_ brightness: Float) {
        var iterator: io_iterator_t = 0

        let result = IOServiceGetMatchingServices(kIOMainPortDefault, IOServiceMatching("IODisplayConnect"), &iterator)
        guard result == kIOReturnSuccess else { return }

        let service = IOIteratorNext(iterator)
        IOObjectRelease(iterator)
        guard service != 0 else { return }

        IODisplaySetFloatParameter(service, 0, kIODisplayBrightnessKey as CFString, brightness)
        IOObjectRelease(service)
        logger.debug("display brightness set to \(brightness)")
    }

    // MARK: - System Information

    /// Get battery level (0-100)
    func getBatteryLevel() -> Int? {
        let snapshot = IOPSCopyPowerSourcesInfo().takeRetainedValue()
        let sources = IOPSCopyPowerSourcesList(snapshot).takeRetainedValue() as [CFTypeRef]

        for source in sources {
            if let description = IOPSGetPowerSourceDescription(snapshot, source).takeUnretainedValue() as? [String: Any] {
                if let capacity = description[kIOPSCurrentCapacityKey] as? Int {
                    return capacity
                }
            }
        }
        return nil
    }

    /// Check if on battery power
    func isOnBattery() -> Bool {
        let snapshot = IOPSCopyPowerSourcesInfo().takeRetainedValue()
        let type = IOPSGetProvidingPowerSourceType(snapshot).takeRetainedValue() as String
        return type == kIOPSBatteryPowerValue
    }

    /// Check if charging
    func isCharging() -> Bool {
        let snapshot = IOPSCopyPowerSourcesInfo().takeRetainedValue()
        let sources = IOPSCopyPowerSourcesList(snapshot).takeRetainedValue() as [CFTypeRef]

        for source in sources {
            if let description = IOPSGetPowerSourceDescription(snapshot, source).takeUnretainedValue() as? [String: Any] {
                if let isCharging = description[kIOPSIsChargingKey] as? Bool {
                    return isCharging
                }
            }
        }
        return false
    }

    /// Get CPU usage percentage
    func getCPUUsage() -> Double {
        var cpuInfo: processor_info_array_t!
        var numCpuInfo: mach_msg_type_number_t = 0
        var numCPUs: natural_t = 0
        var numCPUsU: UInt32 = 0
        var sizeOfNumCPUs: Int = MemoryLayout<UInt32>.size

        let status = sysctlbyname("hw.ncpu", &numCPUsU, &sizeOfNumCPUs, nil, 0)
        if status != 0 {
            return 0
        }
        numCPUs = numCPUsU

        var numCPUsReal: natural_t = 0
        let err = host_processor_info(mach_host_self(), PROCESSOR_CPU_LOAD_INFO, &numCPUsReal, &cpuInfo, &numCpuInfo)
        guard err == KERN_SUCCESS else { return 0 }

        var usage: Double = 0

        let cpuLoad = cpuInfo!.withMemoryRebound(to: processor_cpu_load_info.self, capacity: Int(numCPUs)) { $0 }

        for i in 0..<Int(numCPUs) {
            let user = Double(cpuLoad[i].cpu_ticks.0)
            let system = Double(cpuLoad[i].cpu_ticks.1)
            let idle = Double(cpuLoad[i].cpu_ticks.2)
            let nice = Double(cpuLoad[i].cpu_ticks.3)

            let totalTicks = user + system + idle + nice
            if totalTicks > 0 {
                usage += (user + system + nice) / totalTicks
            }
        }

        vm_deallocate(mach_task_self_, vm_address_t(bitPattern: cpuInfo), vm_size_t(numCpuInfo))

        return usage / Double(numCPUs) * 100
    }

    /// Get memory usage percentage
    func getMemoryUsage() -> Double {
        var pageSize: vm_size_t = 0
        host_page_size(mach_host_self(), &pageSize)

        var vmStats = vm_statistics_data_t()
        var count = mach_msg_type_number_t(MemoryLayout<vm_statistics_data_t>.size / MemoryLayout<integer_t>.size)

        let result = withUnsafeMutablePointer(to: &vmStats) {
            $0.withMemoryRebound(to: integer_t.self, capacity: Int(count)) {
                host_statistics(mach_host_self(), HOST_VM_INFO, $0, &count)
            }
        }

        guard result == KERN_SUCCESS else { return 0 }

        let activePages = Double(vmStats.active_count)
        let wirePages = Double(vmStats.wire_count)
        let freePages = Double(vmStats.free_count)
        let inactivePages = Double(vmStats.inactive_count)

        let usedMemory = (activePages + wirePages) * Double(pageSize)
        let totalMemory = (activePages + wirePages + freePages + inactivePages) * Double(pageSize)

        return (usedMemory / totalMemory) * 100
    }

    /// Get thermal state
    func getThermalState() -> SystemStatus.ThermalState {
        let state = ProcessInfo.processInfo.thermalState
        switch state {
        case .nominal: return .nominal
        case .fair: return .fair
        case .serious: return .serious
        case .critical: return .critical
        @unknown default: return .nominal
        }
    }

    // MARK: - Appearance

    /// Check if dark mode is enabled
    func isDarkMode() -> Bool {
        NSApp.effectiveAppearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua
    }

    /// Toggle system dark mode
    func toggleDarkMode() {
        let script = """
        tell application "System Events"
            tell appearance preferences
                set dark mode to not dark mode
            end tell
        end tell
        """

        var error: NSDictionary?
        if let appleScript = NSAppleScript(source: script) {
            appleScript.executeAndReturnError(&error)
            if error == nil {
                logger.info("dark mode toggled")
            }
        }
    }
}
