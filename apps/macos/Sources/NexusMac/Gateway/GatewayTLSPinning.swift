import CryptoKit
import Foundation
import NexusMacObjC
import OSLog
import Security

// MARK: - Pinning Mode

/// TLS certificate pinning modes for gateway connections.
public enum TLSPinningMode: String, Sendable, Codable, CaseIterable {
    /// No pinning - development only, not recommended for production.
    case disabled

    /// Pin to the SHA256 hash of the public key (SPKI).
    case publicKey

    /// Pin to the SHA256 hash of the full certificate (DER).
    case certificate

    /// Pin to the root CA certificate in the chain.
    case rootCA
}

// MARK: - TLS Pin

/// A pinned certificate or public key for a specific host.
public struct TLSPin: Sendable, Codable, Equatable, Identifiable {
    public var id: String { "\(host):\(hash)" }

    /// Hostname pattern (e.g., "api.nexus.dev" or "*.nexus.dev").
    public let host: String

    /// SHA256 hash of the public key or certificate (hex-encoded).
    public let hash: String

    /// Optional expiration date for certificate rotation.
    public let expiresAt: Date?

    /// Source of the pin for tracking purposes.
    public let source: TLSPinSource

    /// Whether this is a backup pin for rotation periods.
    public let isBackup: Bool

    public init(
        host: String,
        hash: String,
        expiresAt: Date? = nil,
        source: TLSPinSource = .embedded,
        isBackup: Bool = false
    ) {
        self.host = host
        self.hash = Self.normalizeHash(hash)
        self.expiresAt = expiresAt
        self.source = source
        self.isBackup = isBackup
    }

    /// Checks if the pin has expired.
    public var isExpired: Bool {
        guard let expiresAt else { return false }
        return Date() > expiresAt
    }

    /// Checks if the pin matches the given hostname.
    public func matches(hostname: String) -> Bool {
        if host == hostname { return true }
        if host.hasPrefix("*.") {
            let suffix = String(host.dropFirst(2))
            let parts = hostname.split(separator: ".")
            if parts.count >= 2 {
                let hostSuffix = parts.dropFirst().joined(separator: ".")
                return hostSuffix == suffix
            }
        }
        return false
    }

    /// Normalizes a hash string by removing prefixes and converting to lowercase hex.
    private static func normalizeHash(_ raw: String) -> String {
        let stripped = raw.replacingOccurrences(
            of: #"(?i)^sha-?256\s*:?\s*"#,
            with: "",
            options: .regularExpression)
        return stripped.lowercased().filter(\.isHexDigit)
    }
}

// MARK: - Pin Source

/// Source of a TLS pin.
public enum TLSPinSource: String, Sendable, Codable {
    /// Embedded in app bundle.
    case embedded

    /// Fetched from trusted remote source.
    case remote

    /// User-provided custom pin.
    case custom

    /// Trust-on-first-use (TOFU).
    case tofu
}

// MARK: - Pinning Result

/// Result of TLS certificate validation.
public enum TLSPinningResult: Sendable {
    case success(matchedPin: TLSPin)
    case successBackup(matchedPin: TLSPin)
    case failedNoPinMatch(host: String, receivedHash: String)
    case failedExpiredPin(pin: TLSPin)
    case failedChainValidation(error: Error)
    case disabled
}

// MARK: - Pinning Configuration

/// Configuration for the TLS pinning system.
public struct TLSPinningConfiguration: Sendable {
    public let mode: TLSPinningMode
    public let pins: [TLSPin]
    public let enforceExpiration: Bool
    public let gracePeriodDays: Int
    public let allowTOFU: Bool

    public init(
        mode: TLSPinningMode = .publicKey,
        pins: [TLSPin] = [],
        enforceExpiration: Bool = true,
        gracePeriodDays: Int = 7,
        allowTOFU: Bool = false
    ) {
        self.mode = mode
        self.pins = pins
        self.enforceExpiration = enforceExpiration
        self.gracePeriodDays = gracePeriodDays
        self.allowTOFU = allowTOFU
    }
}

// MARK: - TLS Pin Store

/// Persistent storage for TLS pins.
public enum GatewayTLSPinStore {
    private static let suiteName = "com.nexus.shared"
    private static let keyPrefix = "gateway.tls.pin."
    private static let pinsKey = "gateway.tls.pins"

    private static var defaults: UserDefaults {
        UserDefaults(suiteName: suiteName) ?? .standard
    }

    /// Loads all stored pins.
    public static func loadPins() -> [TLSPin] {
        guard let data = defaults.data(forKey: pinsKey),
              let pins = try? JSONDecoder().decode([TLSPin].self, from: data)
        else {
            return []
        }
        return pins
    }

    /// Saves pins to persistent storage.
    public static func savePins(_ pins: [TLSPin]) {
        guard let data = try? JSONEncoder().encode(pins) else { return }
        defaults.set(data, forKey: pinsKey)
    }

    /// Loads a TOFU fingerprint for a specific host.
    public static func loadTOFUFingerprint(host: String) -> String? {
        let key = keyPrefix + "tofu." + host
        let raw = defaults.string(forKey: key)?.trimmingCharacters(in: .whitespacesAndNewlines)
        return raw?.isEmpty == false ? raw : nil
    }

    /// Saves a TOFU fingerprint for a host.
    public static func saveTOFUFingerprint(_ fingerprint: String, host: String) {
        let key = keyPrefix + "tofu." + host
        defaults.set(fingerprint, forKey: key)
    }

    /// Removes a TOFU fingerprint for a host.
    public static func removeTOFUFingerprint(host: String) {
        let key = keyPrefix + "tofu." + host
        defaults.removeObject(forKey: key)
    }
}

// MARK: - Gateway TLS Pinning Manager

/// Manages TLS certificate pinning for gateway connections.
public final class GatewayTLSPinningManager: @unchecked Sendable {
    public static let shared = GatewayTLSPinningManager()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "tls-pinning")
    private let lock = NSLock()

    private var _configuration: TLSPinningConfiguration
    private var _backupPins: [TLSPin] = []

    public var configuration: TLSPinningConfiguration {
        lock.lock()
        defer { lock.unlock() }
        return _configuration
    }

    private init() {
        // Load embedded pins and stored pins
        let storedPins = GatewayTLSPinStore.loadPins()
        _configuration = TLSPinningConfiguration(
            mode: .publicKey,
            pins: storedPins,
            enforceExpiration: true,
            gracePeriodDays: 7,
            allowTOFU: false
        )
    }

    // MARK: - Configuration

    /// Configures the pinning system with a mode and pins.
    public func configurePinning(mode: TLSPinningMode, pins: [TLSPin]) {
        lock.lock()
        defer { lock.unlock() }

        _configuration = TLSPinningConfiguration(
            mode: mode,
            pins: pins,
            enforceExpiration: _configuration.enforceExpiration,
            gracePeriodDays: _configuration.gracePeriodDays,
            allowTOFU: _configuration.allowTOFU
        )

        // Persist non-embedded pins
        let persistablePins = pins.filter { $0.source != .embedded }
        GatewayTLSPinStore.savePins(persistablePins)

        logger.info("Configured TLS pinning: mode=\(mode.rawValue, privacy: .public), pins=\(pins.count)")
    }

    /// Adds a backup pin for certificate rotation.
    public func addBackupPin(_ pin: TLSPin) {
        lock.lock()
        defer { lock.unlock() }

        let backupPin = TLSPin(
            host: pin.host,
            hash: pin.hash,
            expiresAt: pin.expiresAt,
            source: pin.source,
            isBackup: true
        )
        _backupPins.removeAll { $0.host == pin.host && $0.hash == pin.hash }
        _backupPins.append(backupPin)

        logger.info("Added backup pin for host: \(pin.host, privacy: .public)")
    }

    /// Removes all expired pins (both primary and backup).
    public func removeExpiredPins() {
        lock.lock()
        defer { lock.unlock() }

        let now = Date()
        let gracePeriod = TimeInterval(_configuration.gracePeriodDays * 24 * 60 * 60)

        let activePins = _configuration.pins.filter { pin in
            guard let expiresAt = pin.expiresAt else { return true }
            return now < expiresAt.addingTimeInterval(gracePeriod)
        }

        let removedCount = _configuration.pins.count - activePins.count
        if removedCount > 0 {
            _configuration = TLSPinningConfiguration(
                mode: _configuration.mode,
                pins: activePins,
                enforceExpiration: _configuration.enforceExpiration,
                gracePeriodDays: _configuration.gracePeriodDays,
                allowTOFU: _configuration.allowTOFU
            )

            let persistablePins = activePins.filter { $0.source != .embedded }
            GatewayTLSPinStore.savePins(persistablePins)

            logger.info("Removed \(removedCount) expired pins")
        }

        _backupPins.removeAll { pin in
            guard let expiresAt = pin.expiresAt else { return false }
            return now > expiresAt.addingTimeInterval(gracePeriod)
        }
    }

    // MARK: - Certificate Validation

    /// Validates a certificate against configured pins.
    public func validateCertificate(_ certificate: SecCertificate, for host: String) -> Bool {
        let result = validateCertificateWithResult(certificate, for: host)
        switch result {
        case .success, .successBackup, .disabled:
            return true
        case .failedNoPinMatch, .failedExpiredPin, .failedChainValidation:
            return false
        }
    }

    /// Validates a certificate and returns detailed result.
    public func validateCertificateWithResult(
        _ certificate: SecCertificate,
        for host: String
    ) -> TLSPinningResult {
        lock.lock()
        let config = _configuration
        let backups = _backupPins
        lock.unlock()

        if config.mode == .disabled {
            logger.debug("TLS pinning disabled, allowing connection to \(host, privacy: .public)")
            return .disabled
        }

        // Extract hash based on pinning mode
        let hash: String?
        switch config.mode {
        case .disabled:
            return .disabled
        case .publicKey:
            hash = extractPublicKeyHash(from: certificate)
        case .certificate:
            hash = extractCertificateHash(from: certificate)
        case .rootCA:
            // For root CA mode, we need the full chain - handled in trust validation
            hash = extractCertificateHash(from: certificate)
        }

        guard let receivedHash = hash else {
            logger.error("Failed to extract hash from certificate for \(host, privacy: .public)")
            return .failedChainValidation(
                error: NSError(
                    domain: "GatewayTLSPinning",
                    code: 1,
                    userInfo: [NSLocalizedDescriptionKey: "Failed to extract certificate hash"]))
        }

        // Find matching pins for this host
        let hostPins = config.pins.filter { $0.matches(hostname: host) }
        let hostBackups = backups.filter { $0.matches(hostname: host) }

        // Check primary pins first
        for pin in hostPins {
            if pin.hash == receivedHash {
                if pin.isExpired && config.enforceExpiration {
                    logger.warning(
                        "Pin expired for \(host, privacy: .public), hash: \(receivedHash.prefix(16), privacy: .public)...")
                    return .failedExpiredPin(pin: pin)
                }
                logger.info("Certificate validated for \(host, privacy: .public) (primary pin)")
                return .success(matchedPin: pin)
            }
        }

        // Check backup pins
        for pin in hostBackups {
            if pin.hash == receivedHash {
                if pin.isExpired && config.enforceExpiration {
                    logger.warning(
                        "Backup pin expired for \(host, privacy: .public), hash: \(receivedHash.prefix(16), privacy: .public)..."
                    )
                    return .failedExpiredPin(pin: pin)
                }
                logger.info("Certificate validated for \(host, privacy: .public) (backup pin)")
                return .successBackup(matchedPin: pin)
            }
        }

        // TOFU handling
        if config.allowTOFU && hostPins.isEmpty {
            if let storedFingerprint = GatewayTLSPinStore.loadTOFUFingerprint(host: host) {
                if storedFingerprint == receivedHash {
                    let tofuPin = TLSPin(host: host, hash: receivedHash, source: .tofu)
                    logger.info("Certificate validated for \(host, privacy: .public) (TOFU)")
                    return .success(matchedPin: tofuPin)
                } else {
                    logger.error(
                        "TOFU mismatch for \(host, privacy: .public): expected \(storedFingerprint.prefix(16), privacy: .public)..., got \(receivedHash.prefix(16), privacy: .public)..."
                    )
                }
            } else {
                // First connection - trust on first use
                GatewayTLSPinStore.saveTOFUFingerprint(receivedHash, host: host)
                let tofuPin = TLSPin(host: host, hash: receivedHash, source: .tofu)
                logger.info("TOFU: Trusting first certificate for \(host, privacy: .public)")
                return .success(matchedPin: tofuPin)
            }
        }

        logger.error(
            "Certificate pinning failed for \(host, privacy: .public): no matching pin found. Received hash: \(receivedHash.prefix(16), privacy: .public)..."
        )
        return .failedNoPinMatch(host: host, receivedHash: receivedHash)
    }

    // MARK: - Hash Extraction

    /// Extracts the SHA256 hash of the public key (SPKI) from a certificate.
    public func extractPublicKeyHash(from certificate: SecCertificate) -> String? {
        guard let publicKey = SecCertificateCopyKey(certificate) else {
            logger.error("Failed to extract public key from certificate")
            return nil
        }

        var error: Unmanaged<CFError>?
        guard let publicKeyData = SecKeyCopyExternalRepresentation(publicKey, &error) as Data? else {
            logger.error("Failed to get public key data: \(error?.takeRetainedValue().localizedDescription ?? "unknown")")
            return nil
        }

        // For RSA keys, we need to add the ASN.1 header for SPKI
        // For EC keys, the data is already in the correct format
        let keyType = SecKeyGetKeyType(publicKey)
        let spkiData: Data

        if keyType == kSecAttrKeyTypeRSA as String {
            // Add ASN.1 header for RSA SPKI
            spkiData = addRSASPKIHeader(to: publicKeyData)
        } else {
            // EC keys - use as-is with a standard header
            spkiData = addECSPKIHeader(to: publicKeyData)
        }

        return sha256Hex(spkiData)
    }

    /// Extracts the SHA256 hash of the full certificate (DER encoded).
    public func extractCertificateHash(from certificate: SecCertificate) -> String? {
        let data = SecCertificateCopyData(certificate) as Data
        return sha256Hex(data)
    }

    // MARK: - Trust Validation

    /// Validates a server trust object against configured pins.
    public func validateServerTrust(
        _ trust: SecTrust,
        for host: String
    ) -> TLSPinningResult {
        lock.lock()
        let config = _configuration
        lock.unlock()

        if config.mode == .disabled {
            return .disabled
        }

        // Get the certificate chain
        guard let chain = SecTrustCopyCertificateChain(trust) as? [SecCertificate],
              !chain.isEmpty
        else {
            logger.error("Failed to get certificate chain for \(host, privacy: .public)")
            return .failedChainValidation(
                error: NSError(
                    domain: "GatewayTLSPinning",
                    code: 2,
                    userInfo: [NSLocalizedDescriptionKey: "Empty certificate chain"]))
        }

        // For root CA mode, validate against the root certificate
        if config.mode == .rootCA {
            guard let rootCert = chain.last else {
                return .failedChainValidation(
                    error: NSError(
                        domain: "GatewayTLSPinning",
                        code: 3,
                        userInfo: [NSLocalizedDescriptionKey: "No root certificate in chain"]))
            }
            return validateCertificateWithResult(rootCert, for: host)
        }

        // For other modes, validate the leaf certificate
        guard let leafCert = chain.first else {
            return .failedChainValidation(
                error: NSError(
                    domain: "GatewayTLSPinning",
                    code: 4,
                    userInfo: [NSLocalizedDescriptionKey: "No leaf certificate in chain"]))
        }

        return validateCertificateWithResult(leafCert, for: host)
    }

    // MARK: - Private Helpers

    private func sha256Hex(_ data: Data) -> String {
        let digest = SHA256.hash(data: data)
        return digest.map { String(format: "%02x", $0) }.joined()
    }

    private func addRSASPKIHeader(to keyData: Data) -> Data {
        // ASN.1 header for RSA SPKI
        let header: [UInt8] = [
            0x30, 0x82, 0x01, 0x22,  // SEQUENCE
            0x30, 0x0D,              // SEQUENCE
            0x06, 0x09,              // OID
            0x2A, 0x86, 0x48, 0x86, 0xF7, 0x0D, 0x01, 0x01, 0x01,  // rsaEncryption
            0x05, 0x00,              // NULL
            0x03, 0x82, 0x01, 0x0F,  // BIT STRING
            0x00,
        ]
        var spkiData = Data(header)
        spkiData.append(keyData)
        return spkiData
    }

    private func addECSPKIHeader(to keyData: Data) -> Data {
        // For EC keys, the external representation is already the public key point
        // We need to wrap it in SPKI format
        // This is a simplified version - in production, you'd determine the curve
        let header: [UInt8] = [
            0x30, 0x59,              // SEQUENCE (89 bytes for P-256)
            0x30, 0x13,              // SEQUENCE
            0x06, 0x07,              // OID
            0x2A, 0x86, 0x48, 0xCE, 0x3D, 0x02, 0x01,  // ecPublicKey
            0x06, 0x08,              // OID
            0x2A, 0x86, 0x48, 0xCE, 0x3D, 0x03, 0x01, 0x07,  // prime256v1
            0x03, 0x42,              // BIT STRING
            0x00,
        ]
        var spkiData = Data(header)
        spkiData.append(keyData)
        return spkiData
    }
}

// MARK: - Helper Extension

private extension SecKey {
    static func getKeyType(_ key: SecKey) -> String? {
        guard let attributes = SecKeyCopyAttributes(key) as? [String: Any],
              let keyType = attributes[kSecAttrKeyType as String] as? String
        else {
            return nil
        }
        return keyType
    }
}

private func SecKeyGetKeyType(_ key: SecKey) -> String? {
    return SecKey.getKeyType(key)
}

// MARK: - URLSession Delegate

/// URLSession delegate that implements TLS certificate pinning for gateway connections.
public final class GatewayTLSPinningSession: NSObject, @unchecked Sendable {
    private let logger = Logger(subsystem: "com.nexus.mac", category: "tls-pinning")
    private let pinningManager: GatewayTLSPinningManager
    private let sessionDelegate = GatewayTLSPinningSessionDelegate()

    /// Creates a URLSession configured with TLS pinning.
    public private(set) lazy var session: URLSession = {
        let config = URLSessionConfiguration.default
        config.waitsForConnectivity = true
        config.timeoutIntervalForRequest = 30
        config.timeoutIntervalForResource = 60
        return URLSession(configuration: config, delegate: sessionDelegate, delegateQueue: nil)
    }()

    public init(pinningManager: GatewayTLSPinningManager = .shared) {
        self.pinningManager = pinningManager
        super.init()
        sessionDelegate.challengeHandler = { [weak self] session, challenge, completionHandler in
            guard let self else {
                completionHandler(.performDefaultHandling, nil)
                return
            }
            self.handleChallenge(session: session, challenge: challenge, completionHandler: completionHandler)
        }
    }

    /// Creates a WebSocket task with TLS pinning.
    public func webSocketTask(with url: URL) -> URLSessionWebSocketTask {
        let task = session.webSocketTask(with: url)
        task.maximumMessageSize = 16 * 1024 * 1024
        return task
    }

    /// Creates a WebSocket task with a URLRequest and TLS pinning.
    public func webSocketTask(with request: URLRequest) -> URLSessionWebSocketTask {
        let task = session.webSocketTask(with: request)
        task.maximumMessageSize = 16 * 1024 * 1024
        return task
    }

    /// Creates a data task with TLS pinning.
    public func dataTask(with request: URLRequest) async throws -> (Data, URLResponse) {
        return try await session.data(for: request)
    }

    private func handleChallenge(
        session _: URLSession,
        challenge: URLAuthenticationChallenge,
        completionHandler: @escaping (URLSession.AuthChallengeDisposition, URLCredential?) -> Void
    ) {
        let finish = completionHandler

        guard challenge.protectionSpace.authenticationMethod == NSURLAuthenticationMethodServerTrust,
              let trust = challenge.protectionSpace.serverTrust
        else {
            finish(.performDefaultHandling, nil)
            return
        }

        let host = challenge.protectionSpace.host
        let result = pinningManager.validateServerTrust(trust, for: host)

        switch result {
        case .success(let pin):
            logger.debug("TLS pinning succeeded for \(host, privacy: .public) with pin: \(pin.hash.prefix(16), privacy: .public)...")
            finish(.useCredential, URLCredential(trust: trust))

        case .successBackup(let pin):
            logger.info("TLS pinning succeeded with backup pin for \(host, privacy: .public): \(pin.hash.prefix(16), privacy: .public)...")
            finish(.useCredential, URLCredential(trust: trust))

        case .disabled:
            // Perform standard trust evaluation when pinning is disabled
            let trustValid = SecTrustEvaluateWithError(trust, nil)
            if trustValid {
                finish(.useCredential, URLCredential(trust: trust))
            } else {
                logger.warning("Standard trust evaluation failed for \(host, privacy: .public)")
                finish(.cancelAuthenticationChallenge, nil)
            }

        case .failedNoPinMatch(let failedHost, let receivedHash):
            logger.error(
                "TLS pinning FAILED for \(failedHost, privacy: .public): no matching pin. Hash: \(receivedHash.prefix(32), privacy: .public)..."
            )
            finish(.cancelAuthenticationChallenge, nil)

        case .failedExpiredPin(let pin):
            logger.error("TLS pinning FAILED for \(host, privacy: .public): pin expired at \(pin.expiresAt?.description ?? "unknown", privacy: .public)")
            finish(.cancelAuthenticationChallenge, nil)

        case .failedChainValidation(let error):
            logger.error("TLS pinning FAILED for \(host, privacy: .public): chain validation error - \(error.localizedDescription, privacy: .public)")
            finish(.cancelAuthenticationChallenge, nil)
        }
    }
}

// MARK: - Embedded Pins Loader

/// Loads embedded TLS pins from the app bundle.
public enum GatewayEmbeddedPins {
    /// Loads pins from a JSON file in the app bundle.
    public static func loadFromBundle(filename: String = "TLSPins.json") -> [TLSPin] {
        guard let url = Bundle.main.url(forResource: filename.replacingOccurrences(of: ".json", with: ""), withExtension: "json"),
              let data = try? Data(contentsOf: url),
              let pins = try? JSONDecoder().decode([TLSPin].self, from: data)
        else {
            return []
        }
        return pins.map { pin in
            TLSPin(
                host: pin.host,
                hash: pin.hash,
                expiresAt: pin.expiresAt,
                source: .embedded,
                isBackup: pin.isBackup
            )
        }
    }

    /// Returns hardcoded pins for known Nexus gateway hosts.
    public static var defaultPins: [TLSPin] {
        // These would be replaced with actual production pins
        return [
            // Example pins - replace with actual gateway certificate hashes
            // TLSPin(
            //     host: "gateway.nexus.dev",
            //     hash: "sha256:abc123...",
            //     expiresAt: Calendar.current.date(byAdding: .year, value: 1, to: Date()),
            //     source: .embedded
            // ),
        ]
    }
}

// MARK: - Remote Pins Fetcher

/// Fetches TLS pins from a trusted remote source.
public actor GatewayRemotePinsFetcher {
    private let logger = Logger(subsystem: "com.nexus.mac", category: "tls-pinning")
    private let trustAnchorURL: URL
    private var lastFetchDate: Date?
    private let minimumFetchInterval: TimeInterval = 3600  // 1 hour

    public init(trustAnchorURL: URL) {
        self.trustAnchorURL = trustAnchorURL
    }

    /// Fetches pins from the remote source.
    public func fetchPins() async throws -> [TLSPin] {
        // Check rate limiting
        if let lastFetch = lastFetchDate,
           Date().timeIntervalSince(lastFetch) < minimumFetchInterval
        {
            logger.debug("Skipping remote pin fetch - rate limited")
            return []
        }

        logger.info("Fetching remote TLS pins from \(self.trustAnchorURL.absoluteString, privacy: .public)")

        // Use a basic URLSession for the trust anchor (no pinning on this request)
        let (data, response) = try await URLSession.shared.data(from: trustAnchorURL)

        guard let httpResponse = response as? HTTPURLResponse,
              httpResponse.statusCode == 200
        else {
            throw NSError(
                domain: "GatewayTLSPinning",
                code: 100,
                userInfo: [NSLocalizedDescriptionKey: "Failed to fetch remote pins"])
        }

        lastFetchDate = Date()

        let pins = try JSONDecoder().decode([TLSPin].self, from: data)
        logger.info("Fetched \(pins.count) remote TLS pins")

        return pins.map { pin in
            TLSPin(
                host: pin.host,
                hash: pin.hash,
                expiresAt: pin.expiresAt,
                source: .remote,
                isBackup: pin.isBackup
            )
        }
    }
}
