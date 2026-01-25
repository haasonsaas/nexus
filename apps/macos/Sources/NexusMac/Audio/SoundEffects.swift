import AppKit
import AVFoundation
import Foundation
import OSLog

/// Types of sound effects available in the application.
enum SoundEffect: String, CaseIterable, Codable, Sendable {
    /// Wake word detection triggered
    case voiceWakeTrigger = "voice_wake_trigger"
    /// Voice message being sent
    case voiceWakeSend = "voice_wake_send"
    /// New message received
    case messageReceived = "message_received"
    /// Message sent confirmation
    case messageSent = "message_sent"
    /// Error occurred
    case error = "error"
    /// Operation completed successfully
    case success = "success"
    /// UI interaction click
    case click = "click"
    /// General notification
    case notification = "notification"
    /// Gateway connection established
    case connectionEstablished = "connection_established"
    /// Gateway connection lost
    case connectionLost = "connection_lost"

    /// Display name for UI
    var displayName: String {
        switch self {
        case .voiceWakeTrigger: "Voice Wake Trigger"
        case .voiceWakeSend: "Voice Wake Send"
        case .messageReceived: "Message Received"
        case .messageSent: "Message Sent"
        case .error: "Error"
        case .success: "Success"
        case .click: "Click"
        case .notification: "Notification"
        case .connectionEstablished: "Connection Established"
        case .connectionLost: "Connection Lost"
        }
    }

    /// Default system sound name for this effect
    var defaultSoundName: String {
        switch self {
        case .voiceWakeTrigger: "Glass"
        case .voiceWakeSend: "Tink"
        case .messageReceived: "Ping"
        case .messageSent: "Pop"
        case .error: "Basso"
        case .success: "Hero"
        case .click: "Tink"
        case .notification: "Ping"
        case .connectionEstablished: "Submarine"
        case .connectionLost: "Sosumi"
        }
    }

    /// Default volume for this effect type (0.0 - 1.0)
    var defaultVolume: Float {
        switch self {
        case .voiceWakeTrigger: 0.7
        case .voiceWakeSend: 0.5
        case .messageReceived: 0.6
        case .messageSent: 0.4
        case .error: 0.8
        case .success: 0.6
        case .click: 0.3
        case .notification: 0.6
        case .connectionEstablished: 0.5
        case .connectionLost: 0.7
        }
    }

    /// UserDefaults key prefix for this effect
    var prefsKeyPrefix: String {
        "SoundEffects.\(self.rawValue)"
    }
}

/// Catalog of available system and custom sounds.
enum SoundCatalog {
    private static let logger = Logger(subsystem: "com.nexus.mac", category: "sound-effects")

    /// Supported audio file extensions
    static let allowedExtensions: Set<String> = [
        "aif", "aiff", "caf", "wav", "m4a", "mp3",
    ]

    /// All discoverable system sound names, with "Glass" pinned first.
    static var systemOptions: [String] {
        var names = Set(Self.discoveredSoundMap.keys).union(Self.fallbackNames)
        names.remove("Glass")
        let sorted = names.sorted { $0.localizedCaseInsensitiveCompare($1) == .orderedAscending }
        return ["Glass"] + sorted
    }

    /// Returns the URL for a sound by name
    static func url(for name: String) -> URL? {
        self.discoveredSoundMap[name]
    }

    // MARK: - Private

    private static let fallbackNames: [String] = [
        "Glass",
        "Ping",
        "Pop",
        "Frog",
        "Submarine",
        "Funk",
        "Tink",
        "Basso",
        "Blow",
        "Bottle",
        "Hero",
        "Morse",
        "Purr",
        "Sosumi",
        "Mail Sent",
        "New Mail",
    ]

    private static let searchRoots: [URL] = [
        FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Library/Sounds"),
        URL(fileURLWithPath: "/Library/Sounds"),
        URL(fileURLWithPath: "/System/Library/Sounds"),
        URL(fileURLWithPath: "/System/Applications/Mail.app/Contents/Resources"),
    ]

    private static let discoveredSoundMap: [String: URL] = {
        var map: [String: URL] = [:]
        for root in Self.searchRoots {
            guard
                let contents = try? FileManager.default.contentsOfDirectory(
                    at: root,
                    includingPropertiesForKeys: nil,
                    options: [.skipsHiddenFiles])
            else { continue }

            for url in contents where Self.allowedExtensions.contains(url.pathExtension.lowercased()) {
                let name = url.deletingPathExtension().lastPathComponent
                // Preserve the first match in priority order
                if map[name] == nil {
                    map[name] = url
                }
            }
        }
        logger.debug("Discovered \(map.count) sounds")
        return map
    }()
}

/// Singleton service for playing sound effects throughout the application.
/// Supports system sounds, custom sounds, per-effect volume control, and respects system sound settings.
@MainActor
final class SoundEffects {
    /// Shared singleton instance
    static let shared = SoundEffects()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "sound-effects")
    private let defaults = UserDefaults.standard

    /// Preloaded sound cache for instant playback
    private var soundCache: [SoundEffect: NSSound] = [:]

    /// Custom sound URLs from user directory (keyed by effect raw value)
    private var customSoundURLs: [SoundEffect: URL] = [:]

    /// Security-scoped bookmark data for custom sounds
    private var customSoundBookmarks: [SoundEffect: Data] = [:]

    /// Currently playing sound (retained to prevent deallocation)
    private var currentSound: NSSound?

    /// Queue for async sound operations
    private let soundQueue = DispatchQueue(label: "com.nexus.mac.sound-effects", qos: .userInteractive)

    // MARK: - Initialization

    private init() {
        loadCustomSoundBookmarks()
    }

    // MARK: - Public API

    /// Plays a sound effect asynchronously.
    /// Respects enabled state, volume settings, and system sound preferences.
    /// - Parameter effect: The sound effect to play
    func play(_ effect: SoundEffect) {
        guard isEnabled(effect) else {
            logger.debug("Sound effect disabled: \(effect.displayName)")
            return
        }

        guard respectsSystemSoundSettings() else {
            logger.debug("System sounds disabled")
            return
        }

        let volume = getVolume(effect)
        guard volume > 0 else {
            logger.debug("Sound volume is zero: \(effect.displayName)")
            return
        }

        soundQueue.async { [weak self] in
            guard let self else { return }

            Task { @MainActor in
                self.playSound(for: effect, volume: volume)
            }
        }
    }

    /// Sets whether a specific sound effect is enabled.
    /// - Parameters:
    ///   - effect: The sound effect to configure
    ///   - enabled: Whether the effect should play
    func setEnabled(_ effect: SoundEffect, _ enabled: Bool) {
        let key = "\(effect.prefsKeyPrefix).enabled"
        defaults.set(enabled, forKey: key)
        logger.info("Sound effect \(effect.displayName) enabled: \(enabled)")
    }

    /// Returns whether a specific sound effect is enabled.
    /// - Parameter effect: The sound effect to check
    /// - Returns: True if the effect is enabled (defaults to true)
    func isEnabled(_ effect: SoundEffect) -> Bool {
        let key = "\(effect.prefsKeyPrefix).enabled"
        // Default to enabled if not explicitly set
        if defaults.object(forKey: key) == nil {
            return true
        }
        return defaults.bool(forKey: key)
    }

    /// Sets the volume for a specific sound effect.
    /// - Parameters:
    ///   - effect: The sound effect to configure
    ///   - volume: Volume level (0.0 to 1.0)
    func setVolume(_ effect: SoundEffect, _ volume: Float) {
        let clampedVolume = max(0.0, min(1.0, volume))
        let key = "\(effect.prefsKeyPrefix).volume"
        defaults.set(clampedVolume, forKey: key)
        logger.info("Sound effect \(effect.displayName) volume: \(clampedVolume)")
    }

    /// Returns the volume for a specific sound effect.
    /// - Parameter effect: The sound effect to check
    /// - Returns: Volume level (0.0 to 1.0), defaults to effect's default volume
    func getVolume(_ effect: SoundEffect) -> Float {
        let key = "\(effect.prefsKeyPrefix).volume"
        if defaults.object(forKey: key) == nil {
            return effect.defaultVolume
        }
        return defaults.float(forKey: key)
    }

    /// Sets a custom sound for a specific effect from a file URL.
    /// Creates a security-scoped bookmark for persistent access.
    /// - Parameters:
    ///   - effect: The sound effect to customize
    ///   - url: URL to the custom sound file (nil to reset to default)
    func setCustomSound(_ effect: SoundEffect, url: URL?) {
        if let url {
            // Create security-scoped bookmark
            do {
                let bookmark = try url.bookmarkData(
                    options: [.withSecurityScope],
                    includingResourceValuesForKeys: nil,
                    relativeTo: nil)
                customSoundBookmarks[effect] = bookmark
                customSoundURLs[effect] = url

                let key = "\(effect.prefsKeyPrefix).customSoundBookmark"
                defaults.set(bookmark, forKey: key)

                // Invalidate cache for this effect
                soundCache.removeValue(forKey: effect)

                logger.info("Set custom sound for \(effect.displayName): \(url.lastPathComponent)")
            } catch {
                logger.error("Failed to create bookmark for custom sound: \(error.localizedDescription)")
            }
        } else {
            // Remove custom sound
            customSoundBookmarks.removeValue(forKey: effect)
            customSoundURLs.removeValue(forKey: effect)
            soundCache.removeValue(forKey: effect)

            let key = "\(effect.prefsKeyPrefix).customSoundBookmark"
            defaults.removeObject(forKey: key)

            logger.info("Removed custom sound for \(effect.displayName)")
        }
    }

    /// Returns the custom sound URL for an effect, if set.
    /// - Parameter effect: The sound effect to check
    /// - Returns: URL to custom sound file, or nil if using default
    func getCustomSoundURL(_ effect: SoundEffect) -> URL? {
        customSoundURLs[effect]
    }

    /// Returns the name of the sound that will play for an effect.
    /// - Parameter effect: The sound effect to check
    /// - Returns: Sound name (custom file name or system sound name)
    func getSoundName(_ effect: SoundEffect) -> String {
        if let customURL = customSoundURLs[effect] {
            return customURL.deletingPathExtension().lastPathComponent
        }
        return getSystemSoundName(effect)
    }

    /// Sets the system sound name for an effect.
    /// - Parameters:
    ///   - effect: The sound effect to configure
    ///   - name: Name of the system sound
    func setSystemSoundName(_ effect: SoundEffect, _ name: String) {
        let key = "\(effect.prefsKeyPrefix).systemSoundName"
        defaults.set(name, forKey: key)
        soundCache.removeValue(forKey: effect)
        logger.info("Set system sound for \(effect.displayName): \(name)")
    }

    /// Returns the system sound name for an effect.
    /// - Parameter effect: The sound effect to check
    /// - Returns: System sound name
    func getSystemSoundName(_ effect: SoundEffect) -> String {
        let key = "\(effect.prefsKeyPrefix).systemSoundName"
        return defaults.string(forKey: key) ?? effect.defaultSoundName
    }

    /// Preloads all sound effects for instant playback.
    /// Call this at application launch for best performance.
    func preloadAll() {
        logger.info("Preloading all sound effects")
        for effect in SoundEffect.allCases {
            _ = loadSound(for: effect)
        }
        logger.info("Preloaded \(self.soundCache.count) sounds")
    }

    /// Clears the sound cache, freeing memory.
    func clearCache() {
        soundCache.removeAll()
        logger.debug("Sound cache cleared")
    }

    /// Returns all available system sound names.
    var availableSystemSounds: [String] {
        SoundCatalog.systemOptions
    }

    // MARK: - Private

    private func playSound(for effect: SoundEffect, volume: Float) {
        guard let sound = loadSound(for: effect) else {
            logger.warning("Could not load sound for effect: \(effect.displayName)")
            return
        }

        // Create a copy to allow overlapping playback
        guard let soundCopy = sound.copy() as? NSSound else {
            logger.warning("Could not copy sound for effect: \(effect.displayName)")
            return
        }

        soundCopy.volume = volume
        soundCopy.stop()

        if soundCopy.play() {
            currentSound = soundCopy
            logger.debug("Playing sound effect: \(effect.displayName) at volume \(volume)")
        } else {
            logger.warning("Failed to play sound effect: \(effect.displayName)")
        }
    }

    private func loadSound(for effect: SoundEffect) -> NSSound? {
        // Check cache first
        if let cached = soundCache[effect] {
            return cached
        }

        var sound: NSSound?

        // Try custom sound first
        if let bookmark = customSoundBookmarks[effect] {
            sound = loadSoundFromBookmark(bookmark)
        }

        // Fall back to system sound
        if sound == nil {
            let soundName = getSystemSoundName(effect)
            sound = loadSystemSound(named: soundName)
        }

        // Cache the loaded sound
        if let sound {
            soundCache[effect] = sound
        }

        return sound
    }

    private func loadSystemSound(named name: String) -> NSSound? {
        // Try NSSound's built-in lookup
        if let sound = NSSound(named: NSSound.Name(name)) {
            return sound
        }

        // Try our discovered sound map
        if let url = SoundCatalog.url(for: name) {
            return NSSound(contentsOf: url, byReference: false)
        }

        logger.warning("System sound not found: \(name)")
        return nil
    }

    private func loadSoundFromBookmark(_ bookmark: Data) -> NSSound? {
        var stale = false
        guard
            let url = try? URL(
                resolvingBookmarkData: bookmark,
                options: [.withoutUI, .withSecurityScope],
                bookmarkDataIsStale: &stale)
        else {
            logger.warning("Failed to resolve sound bookmark")
            return nil
        }

        let scoped = url.startAccessingSecurityScopedResource()
        defer {
            if scoped { url.stopAccessingSecurityScopedResource() }
        }

        return NSSound(contentsOf: url, byReference: false)
    }

    private func loadCustomSoundBookmarks() {
        for effect in SoundEffect.allCases {
            let key = "\(effect.prefsKeyPrefix).customSoundBookmark"
            if let bookmark = defaults.data(forKey: key) {
                customSoundBookmarks[effect] = bookmark

                // Resolve bookmark to get URL
                var stale = false
                if let url = try? URL(
                    resolvingBookmarkData: bookmark,
                    options: [.withoutUI, .withSecurityScope],
                    bookmarkDataIsStale: &stale)
                {
                    customSoundURLs[effect] = url
                }
            }
        }
    }

    /// Checks if system sound effects are enabled in System Preferences.
    /// Respects the "Play sound effects through" setting.
    private func respectsSystemSoundSettings() -> Bool {
        // Check if system UI sounds are enabled
        // Note: This checks the "Play user interface sound effects" preference
        let soundEnabled = defaults.bool(forKey: "com.apple.sound.uiaudio.enabled")
        // Default to true if the key doesn't exist (it won't on most systems)
        if defaults.object(forKey: "com.apple.sound.uiaudio.enabled") == nil {
            return true
        }
        return soundEnabled
    }
}

// MARK: - Convenience Extensions

extension SoundEffects {
    /// Plays the voice wake trigger sound.
    func playVoiceWakeTrigger() {
        play(.voiceWakeTrigger)
    }

    /// Plays the voice wake send sound.
    func playVoiceWakeSend() {
        play(.voiceWakeSend)
    }

    /// Plays the message received sound.
    func playMessageReceived() {
        play(.messageReceived)
    }

    /// Plays the message sent sound.
    func playMessageSent() {
        play(.messageSent)
    }

    /// Plays the error sound.
    func playError() {
        play(.error)
    }

    /// Plays the success sound.
    func playSuccess() {
        play(.success)
    }

    /// Plays the click sound.
    func playClick() {
        play(.click)
    }

    /// Plays the notification sound.
    func playNotification() {
        play(.notification)
    }

    /// Plays the connection established sound.
    func playConnectionEstablished() {
        play(.connectionEstablished)
    }

    /// Plays the connection lost sound.
    func playConnectionLost() {
        play(.connectionLost)
    }
}
