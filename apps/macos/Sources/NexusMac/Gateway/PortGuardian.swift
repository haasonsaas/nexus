import Foundation
#if canImport(Darwin)
import Darwin
#endif

/// Describes processes listening on network ports using lsof.
actor PortGuardian {
    static let shared = PortGuardian()

    struct Descriptor: Sendable {
        let pid: Int32
        let command: String
        let executablePath: String?
    }

    /// Returns a descriptor for the process listening on the given port, if any.
    func describe(port: Int) async -> Descriptor? {
        let listeners = await self.listeners(on: port)
        guard let listener = listeners.first else { return nil }
        let path = Self.executablePath(for: listener.pid)
        return Descriptor(pid: listener.pid, command: listener.command, executablePath: path)
    }

    // MARK: - Internals

    private struct Listener {
        let pid: Int32
        let command: String
    }

    private func listeners(on port: Int) async -> [Listener] {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/sbin/lsof")
        process.arguments = ["-nP", "-iTCP:\(port)", "-sTCP:LISTEN", "-Fpcn"]

        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = Pipe()

        do {
            try process.run()
            process.waitUntilExit()
            let data = pipe.fileHandleForReading.readDataToEndOfFile()
            let text = String(data: data, encoding: .utf8) ?? ""
            return Self.parseListeners(from: text)
        } catch {
            return []
        }
    }

    private static func parseListeners(from text: String) -> [Listener] {
        var listeners: [Listener] = []
        var currentPid: Int32?
        var currentCmd: String?

        func flush() {
            if let pid = currentPid, let cmd = currentCmd {
                listeners.append(Listener(pid: pid, command: cmd))
            }
            currentPid = nil
            currentCmd = nil
        }

        for line in text.split(separator: "\n") {
            guard let prefix = line.first else { continue }
            let value = String(line.dropFirst())
            switch prefix {
            case "p":
                flush()
                currentPid = Int32(value)
            case "c":
                currentCmd = value
            default:
                continue
            }
        }
        flush()
        return listeners
    }

    private static func executablePath(for pid: Int32) -> String? {
        #if canImport(Darwin)
        var buffer = [CChar](repeating: 0, count: Int(PATH_MAX))
        let length = proc_pidpath(pid, &buffer, UInt32(buffer.count))
        guard length > 0 else { return nil }
        let trimmed = buffer.prefix { $0 != 0 }
        let bytes = trimmed.map { UInt8(bitPattern: $0) }
        return String(bytes: bytes, encoding: .utf8)
        #else
        return nil
        #endif
    }
}
