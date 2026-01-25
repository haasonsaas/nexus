import AppKit
import CryptoKit
import Darwin
import Foundation
import OSLog

/// Service managing the exec approvals IPC socket server.
@MainActor
@Observable
final class ExecApprovalsService {
    static let shared = ExecApprovalsService()
    private init() {}

    private let logger = Logger(subsystem: "com.nexus.mac", category: "exec-approvals.service")
    private var server: ExecApprovalsSocketServer?

    private(set) var isRunning = false
    private(set) var pendingRequests: [ExecApprovalPromptRequest] = []

    // MARK: - Lifecycle

    func start() {
        guard server == nil else { return }
        let approvals = ExecApprovalsStore.resolve(agentId: nil)
        let server = ExecApprovalsSocketServer(
            socketPath: approvals.socketPath,
            token: approvals.token,
            onPrompt: { [weak self] request in
                await self?.handlePrompt(request) ?? .deny
            }
        )
        server.start()
        self.server = server
        isRunning = true
        logger.info("exec approvals socket server started at \(approvals.socketPath, privacy: .public)")
    }

    func stop() {
        server?.stop()
        server = nil
        isRunning = false
        logger.info("exec approvals socket server stopped")
    }

    // MARK: - Prompt Handling

    private func handlePrompt(_ request: ExecApprovalPromptRequest) async -> ExecApprovalDecision {
        pendingRequests.append(request)
        defer {
            pendingRequests.removeAll { $0.command == request.command }
        }
        return await ExecApprovalsPromptPresenter.prompt(request)
    }
}

/// Presents the exec approval prompt to the user.
enum ExecApprovalsPromptPresenter {
    @MainActor
    static func prompt(_ request: ExecApprovalPromptRequest) async -> ExecApprovalDecision {
        NSApp.activate(ignoringOtherApps: true)

        let alert = NSAlert()
        alert.alertStyle = .warning
        alert.messageText = "Allow this command?"
        alert.informativeText = "An agent wants to execute a shell command. Review the details below."
        alert.accessoryView = buildAccessoryView(request)

        alert.addButton(withTitle: "Allow Once")
        alert.addButton(withTitle: "Always Allow")
        alert.addButton(withTitle: "Deny")

        if alert.buttons.indices.contains(2) {
            alert.buttons[2].hasDestructiveAction = true
        }

        let response = alert.runModal()
        switch response {
        case .alertFirstButtonReturn:
            return .allowOnce
        case .alertSecondButtonReturn:
            return .allowAlways
        default:
            return .deny
        }
    }

    @MainActor
    private static func buildAccessoryView(_ request: ExecApprovalPromptRequest) -> NSView {
        let stack = NSStackView()
        stack.orientation = .vertical
        stack.spacing = 8
        stack.alignment = .leading

        // Command section
        let commandTitle = NSTextField(labelWithString: "Command")
        commandTitle.font = NSFont.boldSystemFont(ofSize: NSFont.systemFontSize)
        stack.addArrangedSubview(commandTitle)

        let commandText = NSTextView()
        commandText.isEditable = false
        commandText.isSelectable = true
        commandText.drawsBackground = true
        commandText.backgroundColor = NSColor.textBackgroundColor
        commandText.font = NSFont.monospacedSystemFont(ofSize: NSFont.systemFontSize, weight: .regular)
        commandText.string = request.command
        commandText.textContainerInset = NSSize(width: 6, height: 6)
        commandText.textContainer?.lineFragmentPadding = 0
        commandText.textContainer?.widthTracksTextView = true
        commandText.isHorizontallyResizable = false
        commandText.isVerticallyResizable = false

        let commandScroll = NSScrollView()
        commandScroll.borderType = .lineBorder
        commandScroll.hasVerticalScroller = false
        commandScroll.hasHorizontalScroller = false
        commandScroll.documentView = commandText
        commandScroll.translatesAutoresizingMaskIntoConstraints = false
        commandScroll.widthAnchor.constraint(lessThanOrEqualToConstant: 440).isActive = true
        commandScroll.heightAnchor.constraint(greaterThanOrEqualToConstant: 56).isActive = true
        stack.addArrangedSubview(commandScroll)

        // Context section
        let contextTitle = NSTextField(labelWithString: "Context")
        contextTitle.font = NSFont.boldSystemFont(ofSize: NSFont.systemFontSize)
        stack.addArrangedSubview(contextTitle)

        let contextStack = NSStackView()
        contextStack.orientation = .vertical
        contextStack.spacing = 4
        contextStack.alignment = .leading

        let trimmedCwd = request.cwd?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if !trimmedCwd.isEmpty {
            addDetailRow(title: "Working directory", value: trimmedCwd, to: contextStack)
        }

        let trimmedAgent = request.agentId?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if !trimmedAgent.isEmpty {
            addDetailRow(title: "Agent", value: trimmedAgent, to: contextStack)
        }

        let trimmedPath = request.resolvedPath?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if !trimmedPath.isEmpty {
            addDetailRow(title: "Executable", value: trimmedPath, to: contextStack)
        }

        let trimmedHost = request.host?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if !trimmedHost.isEmpty {
            addDetailRow(title: "Host", value: trimmedHost, to: contextStack)
        }

        if let security = request.security?.trimmingCharacters(in: .whitespacesAndNewlines), !security.isEmpty {
            addDetailRow(title: "Security", value: security, to: contextStack)
        }

        if let ask = request.ask?.trimmingCharacters(in: .whitespacesAndNewlines), !ask.isEmpty {
            addDetailRow(title: "Ask mode", value: ask, to: contextStack)
        }

        if contextStack.arrangedSubviews.isEmpty {
            let empty = NSTextField(labelWithString: "No additional context provided.")
            empty.textColor = NSColor.secondaryLabelColor
            empty.font = NSFont.systemFont(ofSize: NSFont.smallSystemFontSize)
            contextStack.addArrangedSubview(empty)
        }

        stack.addArrangedSubview(contextStack)

        // Footer
        let footer = NSTextField(labelWithString: "This command will run on your local machine.")
        footer.textColor = NSColor.secondaryLabelColor
        footer.font = NSFont.systemFont(ofSize: NSFont.smallSystemFontSize)
        stack.addArrangedSubview(footer)

        return stack
    }

    @MainActor
    private static func addDetailRow(title: String, value: String, to stack: NSStackView) {
        let row = NSStackView()
        row.orientation = .horizontal
        row.spacing = 6
        row.alignment = .firstBaseline

        let titleLabel = NSTextField(labelWithString: "\(title):")
        titleLabel.font = NSFont.systemFont(ofSize: NSFont.smallSystemFontSize, weight: .semibold)
        titleLabel.textColor = NSColor.secondaryLabelColor

        let valueLabel = NSTextField(labelWithString: value)
        valueLabel.font = NSFont.systemFont(ofSize: NSFont.smallSystemFontSize)
        valueLabel.lineBreakMode = .byTruncatingMiddle
        valueLabel.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)

        row.addArrangedSubview(titleLabel)
        row.addArrangedSubview(valueLabel)
        stack.addArrangedSubview(row)
    }
}

/// Unix socket server for exec approval IPC.
private final class ExecApprovalsSocketServer: @unchecked Sendable {
    private let logger = Logger(subsystem: "com.nexus.mac", category: "exec-approvals.socket")
    private let socketPath: String
    private let token: String
    private let onPrompt: @Sendable (ExecApprovalPromptRequest) async -> ExecApprovalDecision
    private var socketFD: Int32 = -1
    private var acceptTask: Task<Void, Never>?
    private var isRunning = false

    init(
        socketPath: String,
        token: String,
        onPrompt: @escaping @Sendable (ExecApprovalPromptRequest) async -> ExecApprovalDecision
    ) {
        self.socketPath = socketPath
        self.token = token
        self.onPrompt = onPrompt
    }

    func start() {
        guard !isRunning else { return }
        isRunning = true
        acceptTask = Task.detached { [weak self] in
            await self?.runAcceptLoop()
        }
    }

    func stop() {
        isRunning = false
        acceptTask?.cancel()
        acceptTask = nil
        if socketFD >= 0 {
            close(socketFD)
            socketFD = -1
        }
        if !socketPath.isEmpty {
            unlink(socketPath)
        }
    }

    private func runAcceptLoop() async {
        let fd = openSocket()
        guard fd >= 0 else {
            isRunning = false
            return
        }
        socketFD = fd

        while isRunning {
            var addr = sockaddr_un()
            var len = socklen_t(MemoryLayout.size(ofValue: addr))
            let client = withUnsafeMutablePointer(to: &addr) { ptr in
                ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) { rebound in
                    accept(fd, rebound, &len)
                }
            }
            if client < 0 {
                if errno == EINTR { continue }
                break
            }
            Task.detached { [weak self] in
                await self?.handleClient(fd: client)
            }
        }
    }

    private func openSocket() -> Int32 {
        let fd = socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else {
            logger.error("exec approvals socket create failed")
            return -1
        }

        unlink(socketPath)

        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        let maxLen = MemoryLayout.size(ofValue: addr.sun_path)

        if socketPath.utf8.count >= maxLen {
            logger.error("exec approvals socket path too long")
            close(fd)
            return -1
        }

        socketPath.withCString { cstr in
            withUnsafeMutablePointer(to: &addr.sun_path) { ptr in
                let raw = UnsafeMutableRawPointer(ptr).assumingMemoryBound(to: Int8.self)
                memset(raw, 0, maxLen)
                strncpy(raw, cstr, maxLen - 1)
            }
        }

        let size = socklen_t(MemoryLayout.size(ofValue: addr))
        let result = withUnsafePointer(to: &addr) { ptr in
            ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) { rebound in
                bind(fd, rebound, size)
            }
        }

        if result != 0 {
            logger.error("exec approvals socket bind failed")
            close(fd)
            return -1
        }

        if listen(fd, 16) != 0 {
            logger.error("exec approvals socket listen failed")
            close(fd)
            return -1
        }

        chmod(socketPath, 0o600)
        logger.info("exec approvals socket listening at \(socketPath, privacy: .public)")
        return fd
    }

    private func handleClient(fd: Int32) async {
        let handle = FileHandle(fileDescriptor: fd, closeOnDealloc: true)
        do {
            guard isAllowedPeer(fd: fd) else {
                try sendResponse(handle: handle, id: UUID().uuidString, decision: .deny)
                return
            }

            guard let line = try readLine(from: handle, maxBytes: 256_000),
                  let data = line.data(using: .utf8)
            else {
                return
            }

            let request = try JSONDecoder().decode(SocketRequest.self, from: data)
            guard request.token == token else {
                try sendResponse(handle: handle, id: request.id, decision: .deny)
                return
            }

            let decision = await onPrompt(request.request)
            try sendResponse(handle: handle, id: request.id, decision: decision)
        } catch {
            logger.error("exec approvals socket handling failed: \(error.localizedDescription, privacy: .public)")
        }
    }

    private func readLine(from handle: FileHandle, maxBytes: Int) throws -> String? {
        var buffer = Data()
        while buffer.count < maxBytes {
            let chunk = try handle.read(upToCount: 4096) ?? Data()
            if chunk.isEmpty { break }
            buffer.append(chunk)
            if buffer.contains(0x0A) { break }
        }
        guard let newlineIndex = buffer.firstIndex(of: 0x0A) else {
            guard !buffer.isEmpty else { return nil }
            return String(data: buffer, encoding: .utf8)
        }
        let lineData = buffer.subdata(in: 0 ..< newlineIndex)
        return String(data: lineData, encoding: .utf8)
    }

    private func sendResponse(
        handle: FileHandle,
        id: String,
        decision: ExecApprovalDecision
    ) throws {
        let response = SocketResponse(type: "decision", id: id, decision: decision)
        let data = try JSONEncoder().encode(response)
        var payload = data
        payload.append(0x0A)
        try handle.write(contentsOf: payload)
    }

    private func isAllowedPeer(fd: Int32) -> Bool {
        var uid = uid_t(0)
        var gid = gid_t(0)
        if getpeereid(fd, &uid, &gid) != 0 {
            return false
        }
        return uid == geteuid()
    }

    // MARK: - Socket Protocol

    private struct SocketRequest: Codable {
        var type: String
        var token: String
        var id: String
        var request: ExecApprovalPromptRequest
    }

    private struct SocketResponse: Codable {
        var type: String
        var id: String
        var decision: ExecApprovalDecision
    }
}
