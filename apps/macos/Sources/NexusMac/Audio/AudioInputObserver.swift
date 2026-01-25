import CoreAudio
import Foundation
import Observation
import OSLog

// MARK: - Audio Input Device Model

/// Represents an audio input device with its properties.
struct AudioInputDevice: Identifiable, Equatable, Hashable, Sendable {
    let id: AudioObjectID
    let uid: String
    let name: String
    let manufacturer: String
    let sampleRate: Double
    let channelCount: Int
    var isDefault: Bool
    var volume: Float

    static func == (lhs: AudioInputDevice, rhs: AudioInputDevice) -> Bool {
        lhs.id == rhs.id &&
        lhs.uid == rhs.uid &&
        lhs.name == rhs.name &&
        lhs.manufacturer == rhs.manufacturer &&
        lhs.sampleRate == rhs.sampleRate &&
        lhs.channelCount == rhs.channelCount &&
        lhs.isDefault == rhs.isDefault &&
        abs(lhs.volume - rhs.volume) < 0.001
    }

    func hash(into hasher: inout Hasher) {
        hasher.combine(id)
        hasher.combine(uid)
    }
}

// MARK: - Audio Input Observer

/// Observes audio input device changes and maintains state of available devices.
/// Singleton observer that monitors CoreAudio for device changes.
@MainActor
@Observable
final class AudioInputObserver {
    static let shared = AudioInputObserver()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "audio-input")

    // MARK: - Published State

    private(set) var availableDevices: [AudioInputDevice] = []
    private(set) var defaultDevice: AudioInputDevice?
    var selectedDevice: AudioInputDevice?

    // MARK: - Private State

    private var isObserving = false
    private var devicesListenerProc: AudioObjectPropertyListenerProc?
    private var defaultInputListenerProc: AudioObjectPropertyListenerProc?
    private var devicePropertyListeners: [AudioObjectID: AudioObjectPropertyListenerProc] = [:]

    // MARK: - Initialization

    private init() {
        refreshDevices()
        startObserving()
    }

    deinit {
        // Note: Cleanup handled via stopObserving() which should be called before deallocation
    }

    // MARK: - Public Methods

    /// Sets the system default input device.
    /// - Parameter device: The device to set as default
    func setDefaultDevice(_ device: AudioInputDevice) {
        let systemObject = AudioObjectID(kAudioObjectSystemObject)
        var address = AudioObjectPropertyAddress(
            mSelector: kAudioHardwarePropertyDefaultInputDevice,
            mScope: kAudioObjectPropertyScopeGlobal,
            mElement: kAudioObjectPropertyElementMain
        )

        var deviceID = device.id
        let status = AudioObjectSetPropertyData(
            systemObject,
            &address,
            0,
            nil,
            UInt32(MemoryLayout<AudioObjectID>.size),
            &deviceID
        )

        if status == noErr {
            logger.info("set default input device: \(device.name, privacy: .public)")
        } else {
            logger.error("failed to set default input device: \(status)")
        }
    }

    /// Refreshes the list of available audio input devices.
    func refreshDevices() {
        let previousDevices = availableDevices
        let previousDefault = defaultDevice

        let deviceIDs = getInputDeviceIDs()
        var devices: [AudioInputDevice] = []
        let defaultID = getDefaultInputDeviceID()

        for deviceID in deviceIDs {
            guard Self.deviceIsAlive(deviceID) else { continue }
            guard Self.deviceHasInput(deviceID) else { continue }

            if let device = buildDevice(from: deviceID, isDefault: deviceID == defaultID) {
                devices.append(device)
            }
        }

        availableDevices = devices.sorted { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }
        defaultDevice = devices.first { $0.isDefault }

        // Update selected device if it was removed
        if let selected = selectedDevice, !devices.contains(where: { $0.id == selected.id }) {
            selectedDevice = defaultDevice
            logger.info("selected device removed, switched to default")
        }

        // Post notifications if changes occurred
        if previousDevices != availableDevices || previousDefault?.id != defaultDevice?.id {
            postDeviceChangedNotification()
            notifyVoiceWakeRuntime()
        }

        logger.debug("refreshed devices: \(devices.count) available, default=\(self.defaultDevice?.name ?? "none", privacy: .public)")
    }

    /// Gets the current volume level for a device.
    /// - Parameter device: The audio input device
    /// - Returns: Volume level from 0.0 to 1.0
    func getDeviceVolume(_ device: AudioInputDevice) -> Float {
        return Self.getInputVolume(for: device.id)
    }

    /// Sets the volume level for a device.
    /// - Parameters:
    ///   - device: The audio input device
    ///   - volume: Volume level from 0.0 to 1.0
    func setDeviceVolume(_ device: AudioInputDevice, _ volume: Float) {
        let clampedVolume = max(0.0, min(1.0, volume))
        Self.setInputVolume(for: device.id, volume: clampedVolume)

        // Update cached volume
        if let index = availableDevices.firstIndex(where: { $0.id == device.id }) {
            availableDevices[index].volume = clampedVolume
        }

        postVolumeChangedNotification(device: device, volume: clampedVolume)
        logger.debug("set volume for \(device.name, privacy: .public): \(clampedVolume)")
    }

    // MARK: - Observation Management

    /// Starts observing audio device changes.
    func startObserving() {
        guard !isObserving else { return }
        isObserving = true

        installSystemListeners()
        installDevicePropertyListeners()

        logger.info("audio input observer started")
    }

    /// Stops observing audio device changes.
    func stopObserving() {
        guard isObserving else { return }
        isObserving = false

        removeSystemListeners()
        removeDevicePropertyListeners()

        logger.info("audio input observer stopped")
    }

    // MARK: - Private: System Listeners

    private func installSystemListeners() {
        let systemObject = AudioObjectID(kAudioObjectSystemObject)

        // Devices list listener
        var devicesAddress = AudioObjectPropertyAddress(
            mSelector: kAudioHardwarePropertyDevices,
            mScope: kAudioObjectPropertyScopeGlobal,
            mElement: kAudioObjectPropertyElementMain
        )

        let devicesProc: AudioObjectPropertyListenerProc = { _, _, _, clientData in
            guard let clientData else { return noErr }
            let observer = Unmanaged<AudioInputObserver>.fromOpaque(clientData).takeUnretainedValue()
            Task { @MainActor in
                observer.handleDevicesChanged()
            }
            return noErr
        }

        let devicesStatus = AudioObjectAddPropertyListener(
            systemObject,
            &devicesAddress,
            devicesProc,
            Unmanaged.passUnretained(self).toOpaque()
        )

        if devicesStatus == noErr {
            devicesListenerProc = devicesProc
        } else {
            logger.error("failed to install devices listener: \(devicesStatus)")
        }

        // Default input device listener
        var defaultInputAddress = AudioObjectPropertyAddress(
            mSelector: kAudioHardwarePropertyDefaultInputDevice,
            mScope: kAudioObjectPropertyScopeGlobal,
            mElement: kAudioObjectPropertyElementMain
        )

        let defaultInputProc: AudioObjectPropertyListenerProc = { _, _, _, clientData in
            guard let clientData else { return noErr }
            let observer = Unmanaged<AudioInputObserver>.fromOpaque(clientData).takeUnretainedValue()
            Task { @MainActor in
                observer.handleDefaultDeviceChanged()
            }
            return noErr
        }

        let defaultStatus = AudioObjectAddPropertyListener(
            systemObject,
            &defaultInputAddress,
            defaultInputProc,
            Unmanaged.passUnretained(self).toOpaque()
        )

        if defaultStatus == noErr {
            defaultInputListenerProc = defaultInputProc
        } else {
            logger.error("failed to install default input listener: \(defaultStatus)")
        }
    }

    private func removeSystemListeners() {
        let systemObject = AudioObjectID(kAudioObjectSystemObject)

        if let devicesProc = devicesListenerProc {
            var devicesAddress = AudioObjectPropertyAddress(
                mSelector: kAudioHardwarePropertyDevices,
                mScope: kAudioObjectPropertyScopeGlobal,
                mElement: kAudioObjectPropertyElementMain
            )
            AudioObjectRemovePropertyListener(
                systemObject,
                &devicesAddress,
                devicesProc,
                Unmanaged.passUnretained(self).toOpaque()
            )
            devicesListenerProc = nil
        }

        if let defaultInputProc = defaultInputListenerProc {
            var defaultInputAddress = AudioObjectPropertyAddress(
                mSelector: kAudioHardwarePropertyDefaultInputDevice,
                mScope: kAudioObjectPropertyScopeGlobal,
                mElement: kAudioObjectPropertyElementMain
            )
            AudioObjectRemovePropertyListener(
                systemObject,
                &defaultInputAddress,
                defaultInputProc,
                Unmanaged.passUnretained(self).toOpaque()
            )
            defaultInputListenerProc = nil
        }
    }

    // MARK: - Private: Device Property Listeners

    private func installDevicePropertyListeners() {
        for device in availableDevices {
            installPropertyListener(for: device.id)
        }
    }

    private func removeDevicePropertyListeners() {
        for (deviceID, proc) in devicePropertyListeners {
            removePropertyListener(for: deviceID, proc: proc)
        }
        devicePropertyListeners.removeAll()
    }

    private func installPropertyListener(for deviceID: AudioObjectID) {
        guard devicePropertyListeners[deviceID] == nil else { return }

        // Listen for sample rate changes
        var sampleRateAddress = AudioObjectPropertyAddress(
            mSelector: kAudioDevicePropertyNominalSampleRate,
            mScope: kAudioObjectPropertyScopeGlobal,
            mElement: kAudioObjectPropertyElementMain
        )

        let proc: AudioObjectPropertyListenerProc = { inObjectID, _, _, clientData in
            guard let clientData else { return noErr }
            let observer = Unmanaged<AudioInputObserver>.fromOpaque(clientData).takeUnretainedValue()
            Task { @MainActor in
                observer.handleDevicePropertyChanged(deviceID: inObjectID)
            }
            return noErr
        }

        let status = AudioObjectAddPropertyListener(
            deviceID,
            &sampleRateAddress,
            proc,
            Unmanaged.passUnretained(self).toOpaque()
        )

        if status == noErr {
            devicePropertyListeners[deviceID] = proc
        }

        // Also listen for volume changes
        var volumeAddress = AudioObjectPropertyAddress(
            mSelector: kAudioDevicePropertyVolumeScalar,
            mScope: kAudioDevicePropertyScopeInput,
            mElement: kAudioObjectPropertyElementMain
        )

        let volumeProc: AudioObjectPropertyListenerProc = { inObjectID, _, _, clientData in
            guard let clientData else { return noErr }
            let observer = Unmanaged<AudioInputObserver>.fromOpaque(clientData).takeUnretainedValue()
            Task { @MainActor in
                observer.handleVolumeChanged(deviceID: inObjectID)
            }
            return noErr
        }

        _ = AudioObjectAddPropertyListener(
            deviceID,
            &volumeAddress,
            volumeProc,
            Unmanaged.passUnretained(self).toOpaque()
        )
    }

    private func removePropertyListener(for deviceID: AudioObjectID, proc: AudioObjectPropertyListenerProc) {
        var sampleRateAddress = AudioObjectPropertyAddress(
            mSelector: kAudioDevicePropertyNominalSampleRate,
            mScope: kAudioObjectPropertyScopeGlobal,
            mElement: kAudioObjectPropertyElementMain
        )

        AudioObjectRemovePropertyListener(
            deviceID,
            &sampleRateAddress,
            proc,
            Unmanaged.passUnretained(self).toOpaque()
        )
    }

    // MARK: - Private: Event Handlers

    private func handleDevicesChanged() {
        logger.info("audio devices changed")

        // Remove listeners for devices that no longer exist
        let currentDeviceIDs = Set(getInputDeviceIDs())
        let listenedDeviceIDs = Set(devicePropertyListeners.keys)

        for deviceID in listenedDeviceIDs.subtracting(currentDeviceIDs) {
            if let proc = devicePropertyListeners.removeValue(forKey: deviceID) {
                removePropertyListener(for: deviceID, proc: proc)
            }
        }

        refreshDevices()

        // Add listeners for new devices
        for deviceID in currentDeviceIDs.subtracting(listenedDeviceIDs) {
            if Self.deviceIsAlive(deviceID), Self.deviceHasInput(deviceID) {
                installPropertyListener(for: deviceID)
            }
        }
    }

    private func handleDefaultDeviceChanged() {
        logger.info("default audio input device changed")
        refreshDevices()
    }

    private func handleDevicePropertyChanged(deviceID: AudioObjectID) {
        logger.debug("device property changed: \(deviceID)")
        refreshDevices()
    }

    private func handleVolumeChanged(deviceID: AudioObjectID) {
        guard let device = availableDevices.first(where: { $0.id == deviceID }) else { return }
        let newVolume = Self.getInputVolume(for: deviceID)

        if let index = availableDevices.firstIndex(where: { $0.id == deviceID }) {
            availableDevices[index].volume = newVolume
        }

        postVolumeChangedNotification(device: device, volume: newVolume)
    }

    // MARK: - Private: Notifications

    private func postDeviceChangedNotification() {
        NotificationCenter.default.post(name: .audioDeviceChanged, object: self)
    }

    private func postVolumeChangedNotification(device: AudioInputDevice, volume: Float) {
        NotificationCenter.default.post(
            name: .audioDeviceVolumeChanged,
            object: self,
            userInfo: ["device": device, "volume": volume]
        )
    }

    private func notifyVoiceWakeRuntime() {
        // Notify VoiceWakeRuntime about device changes so it can reinitialize if needed
        Task {
            let state = AppStateStore.shared
            if state.voiceWakeEnabled {
                logger.info("restarting VoiceWake due to device change")
                await VoiceWakeRuntime.shared.stop()
                let config = VoiceWakeRuntime.RuntimeConfig(
                    triggers: state.voiceWakeTriggers,
                    micID: state.voiceWakeMicID.isEmpty ? state.selectedMicrophone : state.voiceWakeMicID,
                    localeID: state.voiceWakeLocaleID.isEmpty ? nil : state.voiceWakeLocaleID,
                    triggerChime: .subtle,
                    sendChime: .standard
                )
                await VoiceWakeRuntime.shared.start(with: config)
            }
        }
    }

    // MARK: - Private: Device Building

    private func buildDevice(from deviceID: AudioObjectID, isDefault: Bool) -> AudioInputDevice? {
        guard let uid = Self.deviceUID(for: deviceID) else { return nil }
        let name = Self.deviceName(for: deviceID) ?? "Unknown Device"
        let manufacturer = Self.deviceManufacturer(for: deviceID) ?? "Unknown"
        let sampleRate = Self.deviceSampleRate(for: deviceID)
        let channelCount = Self.deviceInputChannelCount(for: deviceID)
        let volume = Self.getInputVolume(for: deviceID)

        return AudioInputDevice(
            id: deviceID,
            uid: uid,
            name: name,
            manufacturer: manufacturer,
            sampleRate: sampleRate,
            channelCount: channelCount,
            isDefault: isDefault,
            volume: volume
        )
    }

    // MARK: - Private: CoreAudio Queries

    private func getInputDeviceIDs() -> [AudioObjectID] {
        let systemObject = AudioObjectID(kAudioObjectSystemObject)
        var address = AudioObjectPropertyAddress(
            mSelector: kAudioHardwarePropertyDevices,
            mScope: kAudioObjectPropertyScopeGlobal,
            mElement: kAudioObjectPropertyElementMain
        )

        var size: UInt32 = 0
        var status = AudioObjectGetPropertyDataSize(systemObject, &address, 0, nil, &size)
        guard status == noErr, size > 0 else { return [] }

        let count = Int(size) / MemoryLayout<AudioObjectID>.size
        var deviceIDs = [AudioObjectID](repeating: 0, count: count)
        status = AudioObjectGetPropertyData(systemObject, &address, 0, nil, &size, &deviceIDs)
        guard status == noErr else { return [] }

        return deviceIDs
    }

    private func getDefaultInputDeviceID() -> AudioObjectID {
        let systemObject = AudioObjectID(kAudioObjectSystemObject)
        var address = AudioObjectPropertyAddress(
            mSelector: kAudioHardwarePropertyDefaultInputDevice,
            mScope: kAudioObjectPropertyScopeGlobal,
            mElement: kAudioObjectPropertyElementMain
        )

        var deviceID = AudioObjectID(0)
        var size = UInt32(MemoryLayout<AudioObjectID>.size)
        let status = AudioObjectGetPropertyData(systemObject, &address, 0, nil, &size, &deviceID)

        return status == noErr ? deviceID : 0
    }

    // MARK: - Private Static: Device Properties

    private static func deviceUID(for deviceID: AudioObjectID) -> String? {
        var address = AudioObjectPropertyAddress(
            mSelector: kAudioDevicePropertyDeviceUID,
            mScope: kAudioObjectPropertyScopeGlobal,
            mElement: kAudioObjectPropertyElementMain
        )

        var uid: Unmanaged<CFString>?
        var size = UInt32(MemoryLayout<Unmanaged<CFString>?>.size)
        let status = AudioObjectGetPropertyData(deviceID, &address, 0, nil, &size, &uid)
        guard status == noErr, let uid else { return nil }
        return uid.takeUnretainedValue() as String
    }

    private static func deviceName(for deviceID: AudioObjectID) -> String? {
        var address = AudioObjectPropertyAddress(
            mSelector: kAudioObjectPropertyName,
            mScope: kAudioObjectPropertyScopeGlobal,
            mElement: kAudioObjectPropertyElementMain
        )

        var name: Unmanaged<CFString>?
        var size = UInt32(MemoryLayout<Unmanaged<CFString>?>.size)
        let status = AudioObjectGetPropertyData(deviceID, &address, 0, nil, &size, &name)
        guard status == noErr, let name else { return nil }
        return name.takeUnretainedValue() as String
    }

    private static func deviceManufacturer(for deviceID: AudioObjectID) -> String? {
        var address = AudioObjectPropertyAddress(
            mSelector: kAudioObjectPropertyManufacturer,
            mScope: kAudioObjectPropertyScopeGlobal,
            mElement: kAudioObjectPropertyElementMain
        )

        var manufacturer: Unmanaged<CFString>?
        var size = UInt32(MemoryLayout<Unmanaged<CFString>?>.size)
        let status = AudioObjectGetPropertyData(deviceID, &address, 0, nil, &size, &manufacturer)
        guard status == noErr, let manufacturer else { return nil }
        return manufacturer.takeUnretainedValue() as String
    }

    private static func deviceSampleRate(for deviceID: AudioObjectID) -> Double {
        var address = AudioObjectPropertyAddress(
            mSelector: kAudioDevicePropertyNominalSampleRate,
            mScope: kAudioObjectPropertyScopeGlobal,
            mElement: kAudioObjectPropertyElementMain
        )

        var sampleRate: Float64 = 0
        var size = UInt32(MemoryLayout<Float64>.size)
        let status = AudioObjectGetPropertyData(deviceID, &address, 0, nil, &size, &sampleRate)
        return status == noErr ? sampleRate : 0
    }

    private static func deviceInputChannelCount(for deviceID: AudioObjectID) -> Int {
        var address = AudioObjectPropertyAddress(
            mSelector: kAudioDevicePropertyStreamConfiguration,
            mScope: kAudioDevicePropertyScopeInput,
            mElement: kAudioObjectPropertyElementMain
        )

        var size: UInt32 = 0
        var status = AudioObjectGetPropertyDataSize(deviceID, &address, 0, nil, &size)
        guard status == noErr, size > 0 else { return 0 }

        let raw = UnsafeMutableRawPointer.allocate(
            byteCount: Int(size),
            alignment: MemoryLayout<AudioBufferList>.alignment
        )
        defer { raw.deallocate() }

        let bufferList = raw.bindMemory(to: AudioBufferList.self, capacity: 1)
        status = AudioObjectGetPropertyData(deviceID, &address, 0, nil, &size, bufferList)
        guard status == noErr else { return 0 }

        let buffers = UnsafeMutableAudioBufferListPointer(bufferList)
        return buffers.reduce(0) { $0 + Int($1.mNumberChannels) }
    }

    private static func deviceIsAlive(_ deviceID: AudioObjectID) -> Bool {
        var address = AudioObjectPropertyAddress(
            mSelector: kAudioDevicePropertyDeviceIsAlive,
            mScope: kAudioObjectPropertyScopeGlobal,
            mElement: kAudioObjectPropertyElementMain
        )

        var alive: UInt32 = 0
        var size = UInt32(MemoryLayout<UInt32>.size)
        let status = AudioObjectGetPropertyData(deviceID, &address, 0, nil, &size, &alive)
        return status == noErr && alive != 0
    }

    private static func deviceHasInput(_ deviceID: AudioObjectID) -> Bool {
        var address = AudioObjectPropertyAddress(
            mSelector: kAudioDevicePropertyStreamConfiguration,
            mScope: kAudioDevicePropertyScopeInput,
            mElement: kAudioObjectPropertyElementMain
        )

        var size: UInt32 = 0
        var status = AudioObjectGetPropertyDataSize(deviceID, &address, 0, nil, &size)
        guard status == noErr, size > 0 else { return false }

        let raw = UnsafeMutableRawPointer.allocate(
            byteCount: Int(size),
            alignment: MemoryLayout<AudioBufferList>.alignment
        )
        defer { raw.deallocate() }

        let bufferList = raw.bindMemory(to: AudioBufferList.self, capacity: 1)
        status = AudioObjectGetPropertyData(deviceID, &address, 0, nil, &size, bufferList)
        guard status == noErr else { return false }

        let buffers = UnsafeMutableAudioBufferListPointer(bufferList)
        return buffers.contains(where: { $0.mNumberChannels > 0 })
    }

    private static func getInputVolume(for deviceID: AudioObjectID) -> Float {
        var address = AudioObjectPropertyAddress(
            mSelector: kAudioDevicePropertyVolumeScalar,
            mScope: kAudioDevicePropertyScopeInput,
            mElement: kAudioObjectPropertyElementMain
        )

        // Check if volume property exists
        guard AudioObjectHasProperty(deviceID, &address) else { return 1.0 }

        var volume: Float32 = 1.0
        var size = UInt32(MemoryLayout<Float32>.size)
        let status = AudioObjectGetPropertyData(deviceID, &address, 0, nil, &size, &volume)
        return status == noErr ? volume : 1.0
    }

    private static func setInputVolume(for deviceID: AudioObjectID, volume: Float) {
        var address = AudioObjectPropertyAddress(
            mSelector: kAudioDevicePropertyVolumeScalar,
            mScope: kAudioDevicePropertyScopeInput,
            mElement: kAudioObjectPropertyElementMain
        )

        // Check if volume property is settable
        var isSettable: DarwinBoolean = false
        guard AudioObjectIsPropertySettable(deviceID, &address, &isSettable) == noErr,
              isSettable.boolValue else { return }

        var volumeValue = volume
        AudioObjectSetPropertyData(
            deviceID,
            &address,
            0,
            nil,
            UInt32(MemoryLayout<Float32>.size),
            &volumeValue
        )
    }
}

// MARK: - Notification Names

extension Notification.Name {
    static let audioDeviceChanged = Notification.Name("nexus.audio.deviceChanged")
    static let audioDeviceVolumeChanged = Notification.Name("nexus.audio.volumeChanged")
}
