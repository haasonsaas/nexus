import Foundation

// MARK: - Protocol Version

public enum BridgeProtocol {
    public static let version: UInt8 = 1
    public static let magicBytes: [UInt8] = [0x4E, 0x58, 0x42, 0x52] // "NXBR"
}

// MARK: - Frame Type

public enum BridgeFrameType: UInt8, Sendable {
    case screen = 0x01
    case touch = 0x02
    case command = 0x03
    case status = 0x04
    case auth = 0x05
    case ping = 0x06
    case pong = 0x07
    case error = 0xFF
}

// MARK: - BridgeFrame Protocol

public protocol BridgeFrame: Sendable {
    static var frameType: BridgeFrameType { get }
    func encode() throws -> Data
    static func decode(from data: Data) throws -> Self
}

// MARK: - Frame Errors

public enum BridgeFrameError: Error, Sendable {
    case invalidMagic
    case unsupportedVersion(UInt8)
    case invalidFrameType(UInt8)
    case insufficientData
    case checksumMismatch
    case decodingFailed(String)
    case encodingFailed(String)
}

// MARK: - Frame Header

public struct BridgeFrameHeader: Sendable {
    public let version: UInt8
    public let frameType: BridgeFrameType
    public let payloadLength: UInt32
    public let timestamp: UInt64
    public let checksum: UInt32

    public static let size = 20 // 4 magic + 1 version + 1 type + 4 length + 8 timestamp + 4 checksum - 2 padding

    public init(frameType: BridgeFrameType, payloadLength: UInt32, timestamp: UInt64 = 0, checksum: UInt32 = 0) {
        self.version = BridgeProtocol.version
        self.frameType = frameType
        self.payloadLength = payloadLength
        self.timestamp = timestamp != 0 ? timestamp : UInt64(Date().timeIntervalSince1970 * 1000)
        self.checksum = checksum
    }

    public func encode() -> Data {
        var data = Data()
        data.append(contentsOf: BridgeProtocol.magicBytes)
        data.append(version)
        data.append(frameType.rawValue)
        data.append(contentsOf: withUnsafeBytes(of: payloadLength.bigEndian) { Array($0) })
        data.append(contentsOf: withUnsafeBytes(of: timestamp.bigEndian) { Array($0) })
        data.append(contentsOf: withUnsafeBytes(of: checksum.bigEndian) { Array($0) })
        return data
    }

    public static func decode(from data: Data) throws -> BridgeFrameHeader {
        guard data.count >= size else { throw BridgeFrameError.insufficientData }

        let magic = Array(data[0..<4])
        guard magic == BridgeProtocol.magicBytes else { throw BridgeFrameError.invalidMagic }

        let version = data[4]
        guard version == BridgeProtocol.version else { throw BridgeFrameError.unsupportedVersion(version) }

        let typeRaw = data[5]
        guard let frameType = BridgeFrameType(rawValue: typeRaw) else {
            throw BridgeFrameError.invalidFrameType(typeRaw)
        }

        let payloadLength = data[6..<10].withUnsafeBytes { $0.load(as: UInt32.self).bigEndian }
        let timestamp = data[10..<18].withUnsafeBytes { $0.load(as: UInt64.self).bigEndian }
        let checksum = data[18..<22].withUnsafeBytes { $0.load(as: UInt32.self).bigEndian }

        return BridgeFrameHeader(frameType: frameType, payloadLength: payloadLength, timestamp: timestamp, checksum: checksum)
    }
}

// MARK: - ScreenFrame

public struct ScreenFrame: BridgeFrame {
    public static let frameType: BridgeFrameType = .screen

    public let displayId: UInt32
    public let width: UInt16
    public let height: UInt16
    public let format: ImageFormat
    public let imageData: Data
    public let sequenceNumber: UInt32

    public enum ImageFormat: UInt8, Sendable {
        case jpeg = 0x01
        case png = 0x02
        case heic = 0x03
        case raw = 0x04
    }

    public init(displayId: UInt32, width: UInt16, height: UInt16, format: ImageFormat, imageData: Data, sequenceNumber: UInt32) {
        self.displayId = displayId
        self.width = width
        self.height = height
        self.format = format
        self.imageData = imageData
        self.sequenceNumber = sequenceNumber
    }

    public func encode() throws -> Data {
        var payload = Data()
        payload.append(contentsOf: withUnsafeBytes(of: displayId.bigEndian) { Array($0) })
        payload.append(contentsOf: withUnsafeBytes(of: width.bigEndian) { Array($0) })
        payload.append(contentsOf: withUnsafeBytes(of: height.bigEndian) { Array($0) })
        payload.append(format.rawValue)
        payload.append(contentsOf: withUnsafeBytes(of: sequenceNumber.bigEndian) { Array($0) })
        payload.append(contentsOf: withUnsafeBytes(of: UInt32(imageData.count).bigEndian) { Array($0) })
        payload.append(imageData)

        let checksum = BridgeFrameUtils.crc32(payload)
        let header = BridgeFrameHeader(frameType: Self.frameType, payloadLength: UInt32(payload.count), checksum: checksum)
        return header.encode() + payload
    }

    public static func decode(from data: Data) throws -> ScreenFrame {
        let header = try BridgeFrameHeader.decode(from: data)
        guard header.frameType == frameType else { throw BridgeFrameError.invalidFrameType(header.frameType.rawValue) }

        let payloadStart = BridgeFrameHeader.size
        guard data.count >= payloadStart + 17 else { throw BridgeFrameError.insufficientData }

        let payload = data[payloadStart...]
        let displayId = payload[payloadStart..<payloadStart+4].withUnsafeBytes { $0.load(as: UInt32.self).bigEndian }
        let width = payload[payloadStart+4..<payloadStart+6].withUnsafeBytes { $0.load(as: UInt16.self).bigEndian }
        let height = payload[payloadStart+6..<payloadStart+8].withUnsafeBytes { $0.load(as: UInt16.self).bigEndian }

        guard let format = ImageFormat(rawValue: payload[payloadStart+8]) else {
            throw BridgeFrameError.decodingFailed("Invalid image format")
        }

        let sequenceNumber = payload[payloadStart+9..<payloadStart+13].withUnsafeBytes { $0.load(as: UInt32.self).bigEndian }
        let imageLength = payload[payloadStart+13..<payloadStart+17].withUnsafeBytes { $0.load(as: UInt32.self).bigEndian }

        let imageStart = payloadStart + 17
        guard data.count >= imageStart + Int(imageLength) else { throw BridgeFrameError.insufficientData }

        let imageData = Data(data[imageStart..<imageStart+Int(imageLength)])

        return ScreenFrame(displayId: displayId, width: width, height: height, format: format, imageData: imageData, sequenceNumber: sequenceNumber)
    }
}

// MARK: - TouchFrame

public struct TouchFrame: BridgeFrame {
    public static let frameType: BridgeFrameType = .touch

    public let touchId: UInt32
    public let action: TouchAction
    public let x: Float
    public let y: Float
    public let pressure: Float
    public let timestamp: UInt64

    public enum TouchAction: UInt8, Sendable {
        case down = 0x01
        case move = 0x02
        case up = 0x03
        case cancel = 0x04
        case scroll = 0x05
        case pinch = 0x06
        case rotate = 0x07
    }

    public init(touchId: UInt32, action: TouchAction, x: Float, y: Float, pressure: Float = 1.0, timestamp: UInt64 = 0) {
        self.touchId = touchId
        self.action = action
        self.x = x
        self.y = y
        self.pressure = pressure
        self.timestamp = timestamp != 0 ? timestamp : UInt64(Date().timeIntervalSince1970 * 1000)
    }

    public func encode() throws -> Data {
        var payload = Data()
        payload.append(contentsOf: withUnsafeBytes(of: touchId.bigEndian) { Array($0) })
        payload.append(action.rawValue)
        payload.append(contentsOf: withUnsafeBytes(of: x.bitPattern.bigEndian) { Array($0) })
        payload.append(contentsOf: withUnsafeBytes(of: y.bitPattern.bigEndian) { Array($0) })
        payload.append(contentsOf: withUnsafeBytes(of: pressure.bitPattern.bigEndian) { Array($0) })
        payload.append(contentsOf: withUnsafeBytes(of: timestamp.bigEndian) { Array($0) })

        let checksum = BridgeFrameUtils.crc32(payload)
        let header = BridgeFrameHeader(frameType: Self.frameType, payloadLength: UInt32(payload.count), checksum: checksum)
        return header.encode() + payload
    }

    public static func decode(from data: Data) throws -> TouchFrame {
        let header = try BridgeFrameHeader.decode(from: data)
        guard header.frameType == frameType else { throw BridgeFrameError.invalidFrameType(header.frameType.rawValue) }

        let payloadStart = BridgeFrameHeader.size
        guard data.count >= payloadStart + 25 else { throw BridgeFrameError.insufficientData }

        let touchId = data[payloadStart..<payloadStart+4].withUnsafeBytes { $0.load(as: UInt32.self).bigEndian }

        guard let action = TouchAction(rawValue: data[payloadStart+4]) else {
            throw BridgeFrameError.decodingFailed("Invalid touch action")
        }

        let xBits = data[payloadStart+5..<payloadStart+9].withUnsafeBytes { $0.load(as: UInt32.self).bigEndian }
        let yBits = data[payloadStart+9..<payloadStart+13].withUnsafeBytes { $0.load(as: UInt32.self).bigEndian }
        let pressureBits = data[payloadStart+13..<payloadStart+17].withUnsafeBytes { $0.load(as: UInt32.self).bigEndian }
        let timestamp = data[payloadStart+17..<payloadStart+25].withUnsafeBytes { $0.load(as: UInt64.self).bigEndian }

        return TouchFrame(
            touchId: touchId,
            action: action,
            x: Float(bitPattern: xBits),
            y: Float(bitPattern: yBits),
            pressure: Float(bitPattern: pressureBits),
            timestamp: timestamp
        )
    }
}

// MARK: - CommandFrame

public struct CommandFrame: BridgeFrame {
    public static let frameType: BridgeFrameType = .command

    public let commandId: String
    public let command: String
    public let paramsJSON: String?
    public let responseId: String?

    public init(commandId: String = UUID().uuidString, command: String, paramsJSON: String? = nil, responseId: String? = nil) {
        self.commandId = commandId
        self.command = command
        self.paramsJSON = paramsJSON
        self.responseId = responseId
    }

    public func encode() throws -> Data {
        let container = CommandContainer(commandId: commandId, command: command, paramsJSON: paramsJSON, responseId: responseId)
        let jsonData = try JSONEncoder().encode(container)

        let checksum = BridgeFrameUtils.crc32(jsonData)
        let header = BridgeFrameHeader(frameType: Self.frameType, payloadLength: UInt32(jsonData.count), checksum: checksum)
        return header.encode() + jsonData
    }

    public static func decode(from data: Data) throws -> CommandFrame {
        let header = try BridgeFrameHeader.decode(from: data)
        guard header.frameType == frameType else { throw BridgeFrameError.invalidFrameType(header.frameType.rawValue) }

        let payloadStart = BridgeFrameHeader.size
        let payloadData = data[payloadStart...]

        let container = try JSONDecoder().decode(CommandContainer.self, from: Data(payloadData))
        return CommandFrame(commandId: container.commandId, command: container.command, paramsJSON: container.paramsJSON, responseId: container.responseId)
    }

    private struct CommandContainer: Codable {
        let commandId: String
        let command: String
        let paramsJSON: String?
        let responseId: String?
    }
}

// MARK: - StatusFrame

public struct StatusFrame: BridgeFrame {
    public static let frameType: BridgeFrameType = .status

    public let peerId: String
    public let status: PeerStatus
    public let message: String?
    public let metadata: [String: String]?

    public enum PeerStatus: String, Codable, Sendable {
        case connecting
        case connected
        case ready
        case busy
        case paused
        case disconnecting
        case disconnected
        case error
    }

    public init(peerId: String, status: PeerStatus, message: String? = nil, metadata: [String: String]? = nil) {
        self.peerId = peerId
        self.status = status
        self.message = message
        self.metadata = metadata
    }

    public func encode() throws -> Data {
        let container = StatusContainer(peerId: peerId, status: status, message: message, metadata: metadata)
        let jsonData = try JSONEncoder().encode(container)

        let checksum = BridgeFrameUtils.crc32(jsonData)
        let header = BridgeFrameHeader(frameType: Self.frameType, payloadLength: UInt32(jsonData.count), checksum: checksum)
        return header.encode() + jsonData
    }

    public static func decode(from data: Data) throws -> StatusFrame {
        let header = try BridgeFrameHeader.decode(from: data)
        guard header.frameType == frameType else { throw BridgeFrameError.invalidFrameType(header.frameType.rawValue) }

        let payloadStart = BridgeFrameHeader.size
        let payloadData = data[payloadStart...]

        let container = try JSONDecoder().decode(StatusContainer.self, from: Data(payloadData))
        return StatusFrame(peerId: container.peerId, status: container.status, message: container.message, metadata: container.metadata)
    }

    private struct StatusContainer: Codable {
        let peerId: String
        let status: PeerStatus
        let message: String?
        let metadata: [String: String]?
    }
}

// MARK: - AuthFrame

public struct AuthFrame: BridgeFrame {
    public static let frameType: BridgeFrameType = .auth

    public let phase: AuthPhase
    public let peerId: String
    public let displayName: String?
    public let platform: String?
    public let version: String?
    public let token: String?
    public let publicKey: Data?
    public let challenge: Data?
    public let signature: Data?
    public let capabilities: [String]?

    public enum AuthPhase: String, Codable, Sendable {
        case hello
        case helloOk
        case challenge
        case response
        case verified
        case rejected
    }

    public init(
        phase: AuthPhase,
        peerId: String,
        displayName: String? = nil,
        platform: String? = nil,
        version: String? = nil,
        token: String? = nil,
        publicKey: Data? = nil,
        challenge: Data? = nil,
        signature: Data? = nil,
        capabilities: [String]? = nil
    ) {
        self.phase = phase
        self.peerId = peerId
        self.displayName = displayName
        self.platform = platform
        self.version = version
        self.token = token
        self.publicKey = publicKey
        self.challenge = challenge
        self.signature = signature
        self.capabilities = capabilities
    }

    public func encode() throws -> Data {
        let container = AuthContainer(
            phase: phase,
            peerId: peerId,
            displayName: displayName,
            platform: platform,
            version: version,
            token: token,
            publicKey: publicKey?.base64EncodedString(),
            challenge: challenge?.base64EncodedString(),
            signature: signature?.base64EncodedString(),
            capabilities: capabilities
        )
        let jsonData = try JSONEncoder().encode(container)

        let checksum = BridgeFrameUtils.crc32(jsonData)
        let header = BridgeFrameHeader(frameType: Self.frameType, payloadLength: UInt32(jsonData.count), checksum: checksum)
        return header.encode() + jsonData
    }

    public static func decode(from data: Data) throws -> AuthFrame {
        let header = try BridgeFrameHeader.decode(from: data)
        guard header.frameType == frameType else { throw BridgeFrameError.invalidFrameType(header.frameType.rawValue) }

        let payloadStart = BridgeFrameHeader.size
        let payloadData = data[payloadStart...]

        let container = try JSONDecoder().decode(AuthContainer.self, from: Data(payloadData))
        return AuthFrame(
            phase: container.phase,
            peerId: container.peerId,
            displayName: container.displayName,
            platform: container.platform,
            version: container.version,
            token: container.token,
            publicKey: container.publicKey.flatMap { Data(base64Encoded: $0) },
            challenge: container.challenge.flatMap { Data(base64Encoded: $0) },
            signature: container.signature.flatMap { Data(base64Encoded: $0) },
            capabilities: container.capabilities
        )
    }

    private struct AuthContainer: Codable {
        let phase: AuthPhase
        let peerId: String
        let displayName: String?
        let platform: String?
        let version: String?
        let token: String?
        let publicKey: String?
        let challenge: String?
        let signature: String?
        let capabilities: [String]?
    }
}

// MARK: - PingFrame & PongFrame

public struct PingFrame: BridgeFrame {
    public static let frameType: BridgeFrameType = .ping

    public let pingId: String
    public let timestamp: UInt64

    public init(pingId: String = UUID().uuidString, timestamp: UInt64 = 0) {
        self.pingId = pingId
        self.timestamp = timestamp != 0 ? timestamp : UInt64(Date().timeIntervalSince1970 * 1000)
    }

    public func encode() throws -> Data {
        var payload = Data()
        let idData = pingId.data(using: .utf8) ?? Data()
        payload.append(UInt8(idData.count))
        payload.append(idData)
        payload.append(contentsOf: withUnsafeBytes(of: timestamp.bigEndian) { Array($0) })

        let checksum = BridgeFrameUtils.crc32(payload)
        let header = BridgeFrameHeader(frameType: Self.frameType, payloadLength: UInt32(payload.count), checksum: checksum)
        return header.encode() + payload
    }

    public static func decode(from data: Data) throws -> PingFrame {
        let header = try BridgeFrameHeader.decode(from: data)
        guard header.frameType == frameType else { throw BridgeFrameError.invalidFrameType(header.frameType.rawValue) }

        let payloadStart = BridgeFrameHeader.size
        guard data.count > payloadStart else { throw BridgeFrameError.insufficientData }

        let idLength = Int(data[payloadStart])
        guard data.count >= payloadStart + 1 + idLength + 8 else { throw BridgeFrameError.insufficientData }

        let idData = data[payloadStart+1..<payloadStart+1+idLength]
        guard let pingId = String(data: idData, encoding: .utf8) else {
            throw BridgeFrameError.decodingFailed("Invalid ping ID encoding")
        }

        let timestampStart = payloadStart + 1 + idLength
        let timestamp = data[timestampStart..<timestampStart+8].withUnsafeBytes { $0.load(as: UInt64.self).bigEndian }

        return PingFrame(pingId: pingId, timestamp: timestamp)
    }
}

public struct PongFrame: BridgeFrame {
    public static let frameType: BridgeFrameType = .pong

    public let pingId: String
    public let timestamp: UInt64

    public init(pingId: String, timestamp: UInt64 = 0) {
        self.pingId = pingId
        self.timestamp = timestamp != 0 ? timestamp : UInt64(Date().timeIntervalSince1970 * 1000)
    }

    public func encode() throws -> Data {
        var payload = Data()
        let idData = pingId.data(using: .utf8) ?? Data()
        payload.append(UInt8(idData.count))
        payload.append(idData)
        payload.append(contentsOf: withUnsafeBytes(of: timestamp.bigEndian) { Array($0) })

        let checksum = BridgeFrameUtils.crc32(payload)
        let header = BridgeFrameHeader(frameType: Self.frameType, payloadLength: UInt32(payload.count), checksum: checksum)
        return header.encode() + payload
    }

    public static func decode(from data: Data) throws -> PongFrame {
        let header = try BridgeFrameHeader.decode(from: data)
        guard header.frameType == frameType else { throw BridgeFrameError.invalidFrameType(header.frameType.rawValue) }

        let payloadStart = BridgeFrameHeader.size
        guard data.count > payloadStart else { throw BridgeFrameError.insufficientData }

        let idLength = Int(data[payloadStart])
        guard data.count >= payloadStart + 1 + idLength + 8 else { throw BridgeFrameError.insufficientData }

        let idData = data[payloadStart+1..<payloadStart+1+idLength]
        guard let pingId = String(data: idData, encoding: .utf8) else {
            throw BridgeFrameError.decodingFailed("Invalid ping ID encoding")
        }

        let timestampStart = payloadStart + 1 + idLength
        let timestamp = data[timestampStart..<timestampStart+8].withUnsafeBytes { $0.load(as: UInt64.self).bigEndian }

        return PongFrame(pingId: pingId, timestamp: timestamp)
    }
}

// MARK: - ErrorFrame

public struct ErrorFrame: BridgeFrame {
    public static let frameType: BridgeFrameType = .error

    public let code: String
    public let message: String
    public let details: String?

    public init(code: String, message: String, details: String? = nil) {
        self.code = code
        self.message = message
        self.details = details
    }

    public func encode() throws -> Data {
        let container = ErrorContainer(code: code, message: message, details: details)
        let jsonData = try JSONEncoder().encode(container)

        let checksum = BridgeFrameUtils.crc32(jsonData)
        let header = BridgeFrameHeader(frameType: Self.frameType, payloadLength: UInt32(jsonData.count), checksum: checksum)
        return header.encode() + jsonData
    }

    public static func decode(from data: Data) throws -> ErrorFrame {
        let header = try BridgeFrameHeader.decode(from: data)
        guard header.frameType == frameType else { throw BridgeFrameError.invalidFrameType(header.frameType.rawValue) }

        let payloadStart = BridgeFrameHeader.size
        let payloadData = data[payloadStart...]

        let container = try JSONDecoder().decode(ErrorContainer.self, from: Data(payloadData))
        return ErrorFrame(code: container.code, message: container.message, details: container.details)
    }

    private struct ErrorContainer: Codable {
        let code: String
        let message: String
        let details: String?
    }
}

// MARK: - Frame Utilities

public enum BridgeFrameUtils {
    /// CRC32 checksum calculation
    public static func crc32(_ data: Data) -> UInt32 {
        var crc: UInt32 = 0xFFFFFFFF
        for byte in data {
            crc ^= UInt32(byte)
            for _ in 0..<8 {
                crc = (crc >> 1) ^ (crc & 1 != 0 ? 0xEDB88320 : 0)
            }
        }
        return ~crc
    }

    /// Validate frame checksum
    public static func validateChecksum(header: BridgeFrameHeader, payload: Data) -> Bool {
        let computed = crc32(payload)
        return computed == header.checksum
    }

    /// Parse frame type from raw data without full decode
    public static func peekFrameType(from data: Data) throws -> BridgeFrameType {
        guard data.count >= 6 else { throw BridgeFrameError.insufficientData }

        let magic = Array(data[0..<4])
        guard magic == BridgeProtocol.magicBytes else { throw BridgeFrameError.invalidMagic }

        let typeRaw = data[5]
        guard let frameType = BridgeFrameType(rawValue: typeRaw) else {
            throw BridgeFrameError.invalidFrameType(typeRaw)
        }

        return frameType
    }

    /// Decode any frame from raw data
    public static func decodeAnyFrame(from data: Data) throws -> any BridgeFrame {
        let frameType = try peekFrameType(from: data)

        switch frameType {
        case .screen: return try ScreenFrame.decode(from: data)
        case .touch: return try TouchFrame.decode(from: data)
        case .command: return try CommandFrame.decode(from: data)
        case .status: return try StatusFrame.decode(from: data)
        case .auth: return try AuthFrame.decode(from: data)
        case .ping: return try PingFrame.decode(from: data)
        case .pong: return try PongFrame.decode(from: data)
        case .error: return try ErrorFrame.decode(from: data)
        }
    }
}
