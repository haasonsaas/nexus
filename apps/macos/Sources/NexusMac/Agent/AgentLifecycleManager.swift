import Foundation
import OSLog

// MARK: - Agent State

/// Agent execution states following Clawdbot's state machine patterns
enum AgentState: String, Codable, Sendable {
    case idle
    case spawning
    case executing
    case waitingForTool
    case waitingForApproval
    case paused
    case completed
    case failed
    case cancelled

    var isTerminal: Bool {
        switch self {
        case .completed, .failed, .cancelled:
            return true
        default:
            return false
        }
    }

    var isActive: Bool {
        !isTerminal && self != .idle
    }

    var displayName: String {
        switch self {
        case .idle: return "Idle"
        case .spawning: return "Spawning"
        case .executing: return "Executing"
        case .waitingForTool: return "Waiting for Tool"
        case .waitingForApproval: return "Waiting for Approval"
        case .paused: return "Paused"
        case .completed: return "Completed"
        case .failed: return "Failed"
        case .cancelled: return "Cancelled"
        }
    }
}

// MARK: - Agent Metrics

/// Metrics for an agent run
struct AgentMetrics: Codable, Sendable {
    var inputTokens: Int
    var outputTokens: Int
    var toolCallCount: Int
    var totalDuration: TimeInterval
    var thinkingDuration: TimeInterval
    var toolDuration: TimeInterval

    var totalTokens: Int { inputTokens + outputTokens }

    static var empty: AgentMetrics {
        AgentMetrics(
            inputTokens: 0,
            outputTokens: 0,
            toolCallCount: 0,
            totalDuration: 0,
            thinkingDuration: 0,
            toolDuration: 0
        )
    }
}

// MARK: - Agent Instance

/// Agent instance representing a running or completed agent
struct AgentInstance: Identifiable, Sendable {
    let id: String
    let sessionId: String
    var state: AgentState
    var currentTask: String?
    var progress: Double
    var startedAt: Date
    var pausedAt: Date?
    var completedAt: Date?
    var error: String?
    var toolCalls: [String] // Tool call IDs
    var metrics: AgentMetrics

    /// Duration since agent started
    var elapsedDuration: TimeInterval {
        if let completed = completedAt {
            return completed.timeIntervalSince(startedAt)
        }
        if let paused = pausedAt {
            return paused.timeIntervalSince(startedAt)
        }
        return Date().timeIntervalSince(startedAt)
    }

    /// Whether this agent is currently active
    var isActive: Bool {
        state.isActive
    }
}

// MARK: - Agent Error

enum AgentError: LocalizedError {
    case maxConcurrentReached
    case invalidState
    case notFound
    case invalidTransition(from: AgentState, to: AgentState)
    case timeout
    case cancelled

    var errorDescription: String? {
        switch self {
        case .maxConcurrentReached:
            return "Maximum concurrent agents reached"
        case .invalidState:
            return "Invalid agent state"
        case .notFound:
            return "Agent not found"
        case .invalidTransition(let from, let to):
            return "Invalid state transition from \(from.rawValue) to \(to.rawValue)"
        case .timeout:
            return "Agent timed out"
        case .cancelled:
            return "Agent was cancelled"
        }
    }
}

// MARK: - Agent Lifecycle Manager

/// Manages agent lifecycle and state transitions following Clawdbot patterns.
/// Provides state machine validation, timeout handling, and observer notifications.
@MainActor
@Observable
final class AgentLifecycleManager {
    static let shared = AgentLifecycleManager()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "agent-lifecycle")

    // MARK: - Configuration

    /// Maximum number of concurrent agents allowed
    let maxConcurrentAgents = 5

    /// Default timeout for agent operations (5 minutes)
    let defaultTimeout: TimeInterval = 300

    /// Maximum number of completed agents to keep in history
    private let maxHistory = 50

    // MARK: - State

    /// Active agents indexed by ID
    private(set) var agents: [String: AgentInstance] = [:]

    /// Completed agents history (most recent first)
    private(set) var completedAgents: [AgentInstance] = []

    // MARK: - Internal State

    /// Timeout tasks indexed by agent ID
    private var timeoutTasks: [String: Task<Void, Never>] = [:]

    /// Observers indexed by UUID
    private var observers: [UUID: (AgentInstance) -> Void] = [:]

    // MARK: - Computed Properties

    /// All currently active (non-terminal) agents
    var activeAgents: [AgentInstance] {
        agents.values
            .filter { $0.state.isActive }
            .sorted { $0.startedAt < $1.startedAt }
    }

    /// Whether any agent is currently running
    var isAnyAgentRunning: Bool {
        !activeAgents.isEmpty
    }

    /// Count of active agents
    var activeCount: Int {
        activeAgents.count
    }

    /// Total token usage across all active agents
    var totalActiveTokens: Int {
        activeAgents.reduce(0) { $0 + $1.metrics.totalTokens }
    }

    // MARK: - Initialization

    private init() {
        logger.info("AgentLifecycleManager initialized")
    }

    // MARK: - Agent Queries

    /// Get agent by ID
    func agent(for id: String) -> AgentInstance? {
        agents[id]
    }

    /// Get all agents for a specific session
    func agentsForSession(_ sessionId: String) -> [AgentInstance] {
        agents.values.filter { $0.sessionId == sessionId }
    }

    /// Get completed agents for a specific session
    func completedAgentsForSession(_ sessionId: String) -> [AgentInstance] {
        completedAgents.filter { $0.sessionId == sessionId }
    }

    // MARK: - Lifecycle Operations

    /// Spawn a new agent instance
    /// - Parameters:
    ///   - sessionId: The session this agent belongs to
    ///   - task: Description of the current task
    /// - Returns: The newly created agent instance
    /// - Throws: `AgentError.maxConcurrentReached` if limit is reached
    func spawn(sessionId: String, task: String) throws -> AgentInstance {
        guard activeAgents.count < maxConcurrentAgents else {
            logger.warning("Cannot spawn agent: max concurrent (\(self.maxConcurrentAgents)) reached")
            throw AgentError.maxConcurrentReached
        }

        let id = UUID().uuidString
        let agent = AgentInstance(
            id: id,
            sessionId: sessionId,
            state: .spawning,
            currentTask: task,
            progress: 0,
            startedAt: Date(),
            pausedAt: nil,
            completedAt: nil,
            error: nil,
            toolCalls: [],
            metrics: .empty
        )

        agents[id] = agent
        notifyObservers(agent)

        logger.info("Agent spawned: id=\(id) session=\(sessionId) task=\(task)")

        // Start timeout timer
        scheduleTimeout(for: id)

        return agent
    }

    /// Transition an agent to a new state
    /// - Parameters:
    ///   - agentId: The agent ID
    ///   - newState: The target state
    func transitionState(_ agentId: String, to newState: AgentState) {
        guard var agent = agents[agentId] else {
            logger.warning("Cannot transition unknown agent: \(agentId)")
            return
        }

        let oldState = agent.state
        guard isValidTransition(from: oldState, to: newState) else {
            logger.warning("Invalid transition: \(oldState.rawValue) -> \(newState.rawValue) for agent \(agentId)")
            return
        }

        agent.state = newState

        // Handle state-specific updates
        switch newState {
        case .paused:
            agent.pausedAt = Date()

        case .executing:
            agent.pausedAt = nil
            // Reset timeout on resume
            scheduleTimeout(for: agentId)

        case .completed, .failed, .cancelled:
            agent.completedAt = Date()
            agent.metrics.totalDuration = agent.completedAt!.timeIntervalSince(agent.startedAt)
            moveToHistory(agent)

        case .waitingForTool, .waitingForApproval:
            // Extend timeout while waiting
            scheduleTimeout(for: agentId, extended: true)

        default:
            break
        }

        agents[agentId] = agent
        notifyObservers(agent)

        logger.info("Agent \(agentId) transitioned: \(oldState.rawValue) -> \(newState.rawValue)")
    }

    /// Check if a state transition is valid
    private func isValidTransition(from: AgentState, to: AgentState) -> Bool {
        // Define valid state transitions
        switch (from, to) {
        // From idle
        case (.idle, .spawning): return true

        // From spawning
        case (.spawning, .executing): return true
        case (.spawning, .failed): return true
        case (.spawning, .cancelled): return true

        // From executing
        case (.executing, .waitingForTool): return true
        case (.executing, .waitingForApproval): return true
        case (.executing, .paused): return true
        case (.executing, .completed): return true
        case (.executing, .failed): return true
        case (.executing, .cancelled): return true

        // From waitingForTool
        case (.waitingForTool, .executing): return true
        case (.waitingForTool, .failed): return true
        case (.waitingForTool, .cancelled): return true

        // From waitingForApproval
        case (.waitingForApproval, .executing): return true
        case (.waitingForApproval, .cancelled): return true
        case (.waitingForApproval, .failed): return true

        // From paused
        case (.paused, .executing): return true
        case (.paused, .cancelled): return true
        case (.paused, .failed): return true

        default:
            return false
        }
    }

    // MARK: - Agent Actions

    /// Pause an executing agent
    func pause(_ agentId: String) {
        guard let agent = agents[agentId] else { return }

        if agent.state == .executing || agent.state == .waitingForTool {
            transitionState(agentId, to: .paused)
            cancelTimeout(for: agentId)
        }
    }

    /// Resume a paused agent
    func resume(_ agentId: String) {
        guard let agent = agents[agentId], agent.state == .paused else { return }
        transitionState(agentId, to: .executing)
    }

    /// Cancel an agent
    func cancel(_ agentId: String) {
        guard let agent = agents[agentId] else { return }

        if !agent.state.isTerminal {
            transitionState(agentId, to: .cancelled)

            // Notify gateway to stop the agent
            Task {
                try? await ControlChannel.shared.request(
                    method: "agent.cancel",
                    params: ["agent_id": agentId]
                )
            }
        }
    }

    /// Cancel all agents, optionally filtered by session
    func cancelAll(for sessionId: String? = nil) {
        let toCancel: [AgentInstance]
        if let sessionId {
            toCancel = agentsForSession(sessionId)
        } else {
            toCancel = Array(agents.values)
        }

        for agent in toCancel {
            cancel(agent.id)
        }

        logger.info("Cancelled \(toCancel.count) agents")
    }

    // MARK: - Updates

    /// Update agent progress (0.0 to 1.0)
    func updateProgress(_ agentId: String, progress: Double) {
        guard var agent = agents[agentId] else { return }
        agent.progress = min(max(progress, 0), 1)
        agents[agentId] = agent
        notifyObservers(agent)
    }

    /// Update the current task description
    func updateTask(_ agentId: String, task: String) {
        guard var agent = agents[agentId] else { return }
        agent.currentTask = task
        agents[agentId] = agent
        notifyObservers(agent)
    }

    /// Record a tool call for the agent
    func recordToolCall(_ agentId: String, toolCallId: String) {
        guard var agent = agents[agentId] else { return }
        agent.toolCalls.append(toolCallId)
        agent.metrics.toolCallCount += 1
        agents[agentId] = agent
    }

    /// Update token metrics
    func updateMetrics(_ agentId: String, inputTokens: Int? = nil, outputTokens: Int? = nil) {
        guard var agent = agents[agentId] else { return }
        if let input = inputTokens {
            agent.metrics.inputTokens += input
        }
        if let output = outputTokens {
            agent.metrics.outputTokens += output
        }
        agents[agentId] = agent
    }

    /// Update thinking/tool durations
    func updateDurations(_ agentId: String, thinkingDuration: TimeInterval? = nil, toolDuration: TimeInterval? = nil) {
        guard var agent = agents[agentId] else { return }
        if let thinking = thinkingDuration {
            agent.metrics.thinkingDuration += thinking
        }
        if let tool = toolDuration {
            agent.metrics.toolDuration += tool
        }
        agents[agentId] = agent
    }

    /// Mark an agent as failed with an error message
    func fail(_ agentId: String, error: String) {
        guard var agent = agents[agentId] else { return }
        agent.error = error
        agents[agentId] = agent
        transitionState(agentId, to: .failed)
    }

    /// Mark an agent as completed successfully
    func complete(_ agentId: String) {
        transitionState(agentId, to: .completed)
    }

    // MARK: - Timeout Management

    private func scheduleTimeout(for agentId: String, extended: Bool = false) {
        cancelTimeout(for: agentId)

        let timeout = extended ? defaultTimeout * 2 : defaultTimeout

        timeoutTasks[agentId] = Task { [weak self] in
            try? await Task.sleep(for: .seconds(timeout))

            guard !Task.isCancelled else { return }

            await MainActor.run {
                self?.handleTimeout(agentId)
            }
        }
    }

    private func handleTimeout(_ agentId: String) {
        guard let agent = agents[agentId],
              !agent.state.isTerminal else {
            return
        }

        logger.warning("Agent timed out: \(agentId) after \(Int(self.defaultTimeout))s")
        fail(agentId, error: "Agent timed out after \(Int(defaultTimeout)) seconds")
    }

    private func cancelTimeout(for agentId: String) {
        timeoutTasks[agentId]?.cancel()
        timeoutTasks.removeValue(forKey: agentId)
    }

    // MARK: - History Management

    private func moveToHistory(_ agent: AgentInstance) {
        agents.removeValue(forKey: agent.id)
        cancelTimeout(for: agent.id)

        completedAgents.insert(agent, at: 0)

        // Prune history if needed
        if completedAgents.count > maxHistory {
            completedAgents = Array(completedAgents.prefix(maxHistory))
        }

        logger.debug("Agent \(agent.id) moved to history, \(self.completedAgents.count) in history")
    }

    /// Clear completed agents history
    func clearHistory() {
        completedAgents.removeAll()
        logger.info("Cleared agent history")
    }

    /// Clear history for a specific session
    func clearHistory(for sessionId: String) {
        completedAgents.removeAll { $0.sessionId == sessionId }
        logger.info("Cleared agent history for session \(sessionId)")
    }

    // MARK: - Observer Pattern

    /// Register an observer for agent state changes
    /// - Parameter handler: Callback invoked when any agent changes
    /// - Returns: Observer ID for later removal
    func observe(_ handler: @escaping (AgentInstance) -> Void) -> UUID {
        let id = UUID()
        observers[id] = handler
        return id
    }

    /// Remove an observer
    func removeObserver(_ id: UUID) {
        observers.removeValue(forKey: id)
    }

    private func notifyObservers(_ agent: AgentInstance) {
        for handler in observers.values {
            handler(agent)
        }
    }

    // MARK: - Control Channel Integration

    /// Process an agent event from the control channel
    func processControlEvent(_ event: ControlAgentEvent) {
        let agentId = event.runId

        // Check if this is a known agent
        guard var agent = agents[agentId] else {
            // Could be an external agent - log but don't track
            logger.debug("Event for unknown agent: \(agentId) stream=\(event.stream)")
            return
        }

        // Update based on event stream type
        switch event.stream {
        case "status":
            if let status = event.data["status"]?.value as? String {
                switch status {
                case "executing": transitionState(agentId, to: .executing)
                case "completed": transitionState(agentId, to: .completed)
                case "failed":
                    let error = event.data["error"]?.value as? String ?? "Unknown error"
                    fail(agentId, error: error)
                case "cancelled": transitionState(agentId, to: .cancelled)
                default: break
                }
            }

        case "tool_use":
            transitionState(agentId, to: .waitingForTool)
            if let toolId = event.data["id"]?.value as? String {
                recordToolCall(agentId, toolCallId: toolId)
            }

        case "tool_result":
            transitionState(agentId, to: .executing)

        case "thinking":
            // Agent is processing/thinking
            if agent.state == .spawning {
                transitionState(agentId, to: .executing)
            }

        case "output":
            // Token usage update
            if let tokens = event.data["tokens"]?.value as? [String: Any] {
                let input = tokens["input"] as? Int
                let output = tokens["output"] as? Int
                updateMetrics(agentId, inputTokens: input, outputTokens: output)
            }

        case "progress":
            if let progress = event.data["progress"]?.value as? Double {
                updateProgress(agentId, progress: progress)
            }
            if let task = event.data["task"]?.value as? String {
                updateTask(agentId, task: task)
            }

        default:
            break
        }

        // Refresh agent reference after potential updates
        if let updatedAgent = agents[agentId] {
            agent = updatedAgent
        }
    }

    /// Register for control channel events
    func registerForEvents() {
        NotificationCenter.default.addObserver(
            forName: .controlAgentEvent,
            object: nil,
            queue: .main
        ) { [weak self] notification in
            guard let event = notification.object as? ControlAgentEvent else { return }
            Task { @MainActor in
                self?.processControlEvent(event)
            }
        }

        logger.info("Registered for control channel agent events")
    }
}

// MARK: - Debug Support

#if DEBUG
extension AgentLifecycleManager {
    /// Create a mock agent for testing
    static func _testCreateAgent(
        id: String = UUID().uuidString,
        sessionId: String = "test-session",
        state: AgentState = .executing,
        task: String = "Test task"
    ) -> AgentInstance {
        AgentInstance(
            id: id,
            sessionId: sessionId,
            state: state,
            currentTask: task,
            progress: 0.5,
            startedAt: Date().addingTimeInterval(-30),
            pausedAt: nil,
            completedAt: nil,
            error: nil,
            toolCalls: ["tool-1", "tool-2"],
            metrics: AgentMetrics(
                inputTokens: 1000,
                outputTokens: 500,
                toolCallCount: 2,
                totalDuration: 30,
                thinkingDuration: 20,
                toolDuration: 10
            )
        )
    }
}
#endif
