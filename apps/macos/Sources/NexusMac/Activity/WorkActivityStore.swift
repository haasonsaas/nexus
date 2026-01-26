import Foundation
import Observation

// MARK: - Supporting Types

enum WorkSessionRole: Equatable {
    case main
    case other
}

enum WorkToolKind: String, Codable, Equatable {
    case bash
    case read
    case write
    case edit
    case attach
    case other
}

enum WorkActivityKind: Codable, Equatable {
    case job
    case tool(WorkToolKind)
}

enum WorkIconState: Equatable {
    case idle
    case workingMain(WorkActivityKind)
    case workingOther(WorkActivityKind)
    case overridden(WorkActivityKind)

    enum BadgeProminence: Equatable {
        case primary
        case secondary
        case overridden
    }

    var badgeSymbolName: String {
        switch self.activity {
        case .tool(.bash): "chevron.left.slash.chevron.right"
        case .tool(.read): "doc"
        case .tool(.write): "pencil"
        case .tool(.edit): "pencil.tip"
        case .tool(.attach): "paperclip"
        case .tool(.other), .job: "gearshape.fill"
        }
    }

    var badgeProminence: BadgeProminence? {
        switch self {
        case .idle: nil
        case .workingMain: .primary
        case .workingOther: .secondary
        case .overridden: .overridden
        }
    }

    var isWorking: Bool {
        switch self {
        case .idle: false
        default: true
        }
    }

    private var activity: WorkActivityKind {
        switch self {
        case let .workingMain(kind),
             let .workingOther(kind),
             let .overridden(kind):
            kind
        case .idle:
            .job
        }
    }
}

enum IconOverrideSelection: String, CaseIterable, Identifiable {
    case system
    case idle
    case mainBash, mainRead, mainWrite, mainEdit, mainOther
    case otherBash, otherRead, otherWrite, otherEdit, otherOther

    var id: String { self.rawValue }

    var label: String {
        switch self {
        case .system: "System (auto)"
        case .idle: "Idle"
        case .mainBash: "Working main - bash"
        case .mainRead: "Working main - read"
        case .mainWrite: "Working main - write"
        case .mainEdit: "Working main - edit"
        case .mainOther: "Working main - other"
        case .otherBash: "Working other - bash"
        case .otherRead: "Working other - read"
        case .otherWrite: "Working other - write"
        case .otherEdit: "Working other - edit"
        case .otherOther: "Working other - other"
        }
    }

    func toIconState() -> WorkIconState {
        let map: (WorkToolKind) -> WorkActivityKind = { .tool($0) }
        switch self {
        case .system: return .idle
        case .idle: return .idle
        case .mainBash: return .workingMain(map(.bash))
        case .mainRead: return .workingMain(map(.read))
        case .mainWrite: return .workingMain(map(.write))
        case .mainEdit: return .workingMain(map(.edit))
        case .mainOther: return .workingMain(map(.other))
        case .otherBash: return .workingOther(map(.bash))
        case .otherRead: return .workingOther(map(.read))
        case .otherWrite: return .workingOther(map(.write))
        case .otherEdit: return .workingOther(map(.edit))
        case .otherOther: return .workingOther(map(.other))
        }
    }
}

// MARK: - WorkActivityStore

@MainActor
@Observable
final class WorkActivityStore {
    static let shared = WorkActivityStore()

    struct Activity: Equatable {
        let sessionKey: String
        let role: WorkSessionRole
        let kind: WorkActivityKind
        let label: String
        let startedAt: Date
        var lastUpdate: Date
    }

    private(set) var current: Activity?
    private(set) var iconState: WorkIconState = .idle
    private(set) var lastToolLabel: String?
    private(set) var lastToolUpdatedAt: Date?

    private var jobs: [String: Activity] = [:]
    private var tools: [String: Activity] = [:]
    private var currentSessionKey: String?
    private var toolSeqBySession: [String: Int] = [:]

    private var mainSessionKeyStorage = "main"
    private let toolResultGrace: TimeInterval = 2.0

    var mainSessionKey: String { self.mainSessionKeyStorage }

    // MARK: - Public Methods

    func handleJob(sessionKey: String, state: String) {
        let isStart = state.lowercased() == "started" || state.lowercased() == "streaming"
        if isStart {
            let activity = Activity(
                sessionKey: sessionKey,
                role: self.role(for: sessionKey),
                kind: .job,
                label: "job",
                startedAt: Date(),
                lastUpdate: Date())
            self.setJobActive(activity)
        } else {
            // Job ended (done/error/aborted/etc). Clear everything for this session.
            self.clearTool(sessionKey: sessionKey)
            self.clearJob(sessionKey: sessionKey)
        }
    }

    func handleTool(
        sessionKey: String,
        phase: String,
        name: String?,
        meta: String?,
        args: [String: Any]?)
    {
        let toolKind = Self.mapToolKind(name)
        let label = Self.buildLabel(name: name, meta: meta, args: args)
        if phase.lowercased() == "start" {
            self.lastToolLabel = label
            self.lastToolUpdatedAt = Date()
            self.toolSeqBySession[sessionKey, default: 0] += 1
            let activity = Activity(
                sessionKey: sessionKey,
                role: self.role(for: sessionKey),
                kind: .tool(toolKind),
                label: label,
                startedAt: Date(),
                lastUpdate: Date())
            self.setToolActive(activity)
        } else {
            // Delay removal slightly to avoid flicker on rapid result/start bursts.
            let key = sessionKey
            let seq = self.toolSeqBySession[key, default: 0]
            Task { [weak self] in
                let nsDelay = UInt64((self?.toolResultGrace ?? 0) * 1_000_000_000)
                try? await Task.sleep(nanoseconds: nsDelay)
                await MainActor.run {
                    guard let self else { return }
                    guard self.toolSeqBySession[key, default: 0] == seq else { return }
                    self.lastToolUpdatedAt = Date()
                    self.clearTool(sessionKey: key)
                }
            }
        }
    }

    func setMainSessionKey(_ sessionKey: String) {
        let trimmed = sessionKey.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        guard trimmed != self.mainSessionKeyStorage else { return }
        self.mainSessionKeyStorage = trimmed
        if let current = self.currentSessionKey, !self.isActive(sessionKey: current) {
            self.pickNextSession()
        }
        self.refreshDerivedState()
    }

    func resolveIconState(override selection: IconOverrideSelection) {
        switch selection {
        case .system:
            self.iconState = self.deriveIconState()
        case .idle:
            self.iconState = .idle
        default:
            let base = selection.toIconState()
            switch base {
            case let .workingMain(kind),
                 let .workingOther(kind):
                self.iconState = .overridden(kind)
            case let .overridden(kind):
                self.iconState = .overridden(kind)
            case .idle:
                self.iconState = .idle
            }
        }
    }

    // MARK: - Private Methods

    private func setJobActive(_ activity: Activity) {
        self.jobs[activity.sessionKey] = activity
        // Main session preempts immediately.
        if activity.role == .main {
            self.currentSessionKey = activity.sessionKey
        } else if self.currentSessionKey == nil || !self.isActive(sessionKey: self.currentSessionKey!) {
            self.currentSessionKey = activity.sessionKey
        }
        self.refreshDerivedState()
    }

    private func setToolActive(_ activity: Activity) {
        self.tools[activity.sessionKey] = activity
        // Main session preempts immediately.
        if activity.role == .main {
            self.currentSessionKey = activity.sessionKey
        } else if self.currentSessionKey == nil || !self.isActive(sessionKey: self.currentSessionKey!) {
            self.currentSessionKey = activity.sessionKey
        }
        self.refreshDerivedState()
    }

    private func clearJob(sessionKey: String) {
        guard self.jobs[sessionKey] != nil else { return }
        self.jobs.removeValue(forKey: sessionKey)

        if self.currentSessionKey == sessionKey, !self.isActive(sessionKey: sessionKey) {
            self.pickNextSession()
        }
        self.refreshDerivedState()
    }

    private func clearTool(sessionKey: String) {
        guard self.tools[sessionKey] != nil else { return }
        self.tools.removeValue(forKey: sessionKey)

        if self.currentSessionKey == sessionKey, !self.isActive(sessionKey: sessionKey) {
            self.pickNextSession()
        }
        self.refreshDerivedState()
    }

    private func pickNextSession() {
        // Prefer main if present.
        if self.isActive(sessionKey: self.mainSessionKeyStorage) {
            self.currentSessionKey = self.mainSessionKeyStorage
            return
        }

        // Otherwise, pick most recent by lastUpdate across job/tool.
        let keys = Set(self.jobs.keys).union(self.tools.keys)
        let next = keys.max(by: { self.lastUpdate(for: $0) < self.lastUpdate(for: $1) })
        self.currentSessionKey = next
    }

    private func role(for sessionKey: String) -> WorkSessionRole {
        sessionKey == self.mainSessionKeyStorage ? .main : .other
    }

    private func isActive(sessionKey: String) -> Bool {
        self.jobs[sessionKey] != nil || self.tools[sessionKey] != nil
    }

    private func lastUpdate(for sessionKey: String) -> Date {
        max(self.jobs[sessionKey]?.lastUpdate ?? .distantPast, self.tools[sessionKey]?.lastUpdate ?? .distantPast)
    }

    private func currentActivity(for sessionKey: String) -> Activity? {
        // Prefer tool overlay if present, otherwise job.
        self.tools[sessionKey] ?? self.jobs[sessionKey]
    }

    private func refreshDerivedState() {
        if let key = self.currentSessionKey, !self.isActive(sessionKey: key) {
            self.currentSessionKey = nil
        }
        self.current = self.currentSessionKey.flatMap { self.currentActivity(for: $0) }
        self.iconState = self.deriveIconState()
    }

    private func deriveIconState() -> WorkIconState {
        guard let sessionKey = self.currentSessionKey,
              let activity = self.currentActivity(for: sessionKey)
        else { return .idle }

        switch activity.role {
        case .main: return .workingMain(activity.kind)
        case .other: return .workingOther(activity.kind)
        }
    }

    // MARK: - Static Helpers

    private static func mapToolKind(_ name: String?) -> WorkToolKind {
        switch name?.lowercased() {
        case "bash", "shell": .bash
        case "read": .read
        case "write": .write
        case "edit": .edit
        case "attach": .attach
        default: .other
        }
    }

    private static func buildLabel(
        name: String?,
        meta: String?,
        args: [String: Any]?) -> String
    {
        let toolName = name ?? "tool"

        // Try to extract a meaningful detail from args or meta
        var detail: String?

        if let args = args {
            // Common arg patterns for tool labels
            if let filePath = args["file_path"] as? String ?? args["path"] as? String {
                let filename = (filePath as NSString).lastPathComponent
                detail = filename.isEmpty ? nil : filename
            } else if let command = args["command"] as? String {
                // Truncate long commands
                let trimmed = command.trimmingCharacters(in: .whitespacesAndNewlines)
                let firstLine = trimmed.split(separator: "\n").first.map(String.init) ?? trimmed
                detail = firstLine.count > 40 ? String(firstLine.prefix(37)) + "..." : firstLine
            } else if let pattern = args["pattern"] as? String {
                detail = pattern.count > 30 ? String(pattern.prefix(27)) + "..." : pattern
            } else if let query = args["query"] as? String {
                detail = query.count > 30 ? String(query.prefix(27)) + "..." : query
            }
        }

        // Fall back to meta if no detail from args
        if detail == nil, let meta = meta, !meta.isEmpty {
            detail = meta.count > 40 ? String(meta.prefix(37)) + "..." : meta
        }

        if let detail = detail, !detail.isEmpty {
            return "\(toolName): \(detail)"
        }

        return toolName
    }
}
