import Foundation
import OSLog

/// Engine for executing automated workflows.
/// Chains tool actions into reusable sequences.
@MainActor
@Observable
final class WorkflowEngine {
    static let shared = WorkflowEngine()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "workflow")

    private(set) var workflows: [Workflow] = []
    private(set) var runningWorkflowId: String?
    private(set) var currentStep: Int = 0

    struct Workflow: Identifiable, Codable {
        let id: String
        var name: String
        var description: String?
        var steps: [WorkflowStep]
        var trigger: WorkflowTrigger?
        var isEnabled: Bool

        struct WorkflowStep: Codable {
            let id: String
            let action: String
            let parameters: [String: AnyCodable]
            var continueOnError: Bool
            var delay: TimeInterval?
        }

        enum WorkflowTrigger: Codable {
            case hotkey(String)
            case appLaunch(String)
            case schedule(String)
            case manual
        }
    }

    struct WorkflowResult {
        let workflowId: String
        let success: Bool
        let completedSteps: Int
        let totalSteps: Int
        let errors: [WorkflowError]
        let duration: TimeInterval
    }

    struct WorkflowError {
        let stepIndex: Int
        let stepId: String
        let error: Error
    }

    // MARK: - Workflow Management

    /// Register a workflow
    func register(_ workflow: Workflow) {
        if let index = workflows.firstIndex(where: { $0.id == workflow.id }) {
            workflows[index] = workflow
        } else {
            workflows.append(workflow)
        }
        persistWorkflows()
        logger.info("workflow registered id=\(workflow.id) name=\(workflow.name)")
    }

    /// Remove a workflow
    func remove(workflowId: String) {
        workflows.removeAll { $0.id == workflowId }
        persistWorkflows()
        logger.info("workflow removed id=\(workflowId)")
    }

    /// Enable/disable a workflow
    func setEnabled(_ enabled: Bool, workflowId: String) {
        guard let index = workflows.firstIndex(where: { $0.id == workflowId }) else { return }
        workflows[index].isEnabled = enabled
        persistWorkflows()
    }

    // MARK: - Workflow Execution

    /// Execute a workflow
    func execute(workflowId: String) async -> WorkflowResult {
        guard let workflow = workflows.first(where: { $0.id == workflowId }) else {
            return WorkflowResult(
                workflowId: workflowId,
                success: false,
                completedSteps: 0,
                totalSteps: 0,
                errors: [],
                duration: 0
            )
        }

        return await execute(workflow: workflow)
    }

    /// Execute a workflow directly
    func execute(workflow: Workflow) async -> WorkflowResult {
        runningWorkflowId = workflow.id
        currentStep = 0

        let startTime = Date()
        var errors: [WorkflowError] = []
        var completedSteps = 0

        logger.info("workflow starting id=\(workflow.id) steps=\(workflow.steps.count)")

        for (index, step) in workflow.steps.enumerated() {
            currentStep = index

            // Apply delay if specified
            if let delay = step.delay, delay > 0 {
                try? await Task.sleep(nanoseconds: UInt64(delay * 1_000_000_000))
            }

            do {
                try await executeStep(step)
                completedSteps += 1
                logger.debug("workflow step completed index=\(index) action=\(step.action)")
            } catch {
                errors.append(WorkflowError(stepIndex: index, stepId: step.id, error: error))
                logger.error("workflow step failed index=\(index) error=\(error.localizedDescription)")

                if !step.continueOnError {
                    break
                }
            }
        }

        runningWorkflowId = nil
        let duration = Date().timeIntervalSince(startTime)

        logger.info("workflow completed id=\(workflow.id) success=\(errors.isEmpty) duration=\(String(format: "%.2f", duration))s")

        return WorkflowResult(
            workflowId: workflow.id,
            success: errors.isEmpty,
            completedSteps: completedSteps,
            totalSteps: workflow.steps.count,
            errors: errors,
            duration: duration
        )
    }

    /// Stop the currently running workflow
    func stop() {
        if let id = runningWorkflowId {
            logger.info("workflow stopped id=\(id)")
            runningWorkflowId = nil
        }
    }

    // MARK: - Step Execution

    private func executeStep(_ step: Workflow.WorkflowStep) async throws {
        // Convert step to agent request format
        let request = AgentOrchestrator.AgentRequest(
            agentId: "workflow",
            type: "workflow",
            action: step.action,
            parameters: step.parameters
        )

        let response = try await AgentOrchestrator.shared.processRequest(request)

        if !response.success {
            throw WorkflowStepError.executionFailed(response.error ?? "Unknown error")
        }
    }

    // MARK: - Built-in Workflows

    /// Create a quick screenshot workflow
    static func screenshotWorkflow() -> Workflow {
        Workflow(
            id: "builtin_screenshot",
            name: "Quick Screenshot",
            description: "Take a screenshot and copy to clipboard",
            steps: [
                Workflow.WorkflowStep(
                    id: "capture",
                    action: "screenshot",
                    parameters: [:],
                    continueOnError: false,
                    delay: nil
                )
            ],
            trigger: .hotkey("cmd+shift+5"),
            isEnabled: true
        )
    }

    /// Create a copy context workflow
    static func copyContextWorkflow() -> Workflow {
        Workflow(
            id: "builtin_copy_context",
            name: "Copy Context",
            description: "Gather and copy current context to clipboard",
            steps: [
                Workflow.WorkflowStep(
                    id: "gather",
                    action: "gather_context",
                    parameters: [:],
                    continueOnError: false,
                    delay: nil
                ),
                Workflow.WorkflowStep(
                    id: "copy",
                    action: "clipboard_set",
                    parameters: ["content": AnyCodable("{{context}}")],
                    continueOnError: false,
                    delay: nil
                )
            ],
            trigger: .hotkey("cmd+shift+c"),
            isEnabled: true
        )
    }

    // MARK: - Persistence

    private func persistWorkflows() {
        let url = workflowsFileURL()
        do {
            let data = try JSONEncoder().encode(workflows)
            try data.write(to: url)
        } catch {
            logger.error("failed to persist workflows: \(error.localizedDescription)")
        }
    }

    func loadWorkflows() {
        let url = workflowsFileURL()
        guard FileManager.default.fileExists(atPath: url.path),
              let data = try? Data(contentsOf: url),
              let loaded = try? JSONDecoder().decode([Workflow].self, from: data) else {
            return
        }
        workflows = loaded
        logger.debug("loaded \(loaded.count) workflows")
    }

    private func workflowsFileURL() -> URL {
        let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        let nexusDir = appSupport.appendingPathComponent("Nexus")
        try? FileManager.default.createDirectory(at: nexusDir, withIntermediateDirectories: true)
        return nexusDir.appendingPathComponent("workflows.json")
    }
}

enum WorkflowStepError: LocalizedError {
    case executionFailed(String)
    case invalidParameter(String)
    case timeout

    var errorDescription: String? {
        switch self {
        case .executionFailed(let reason):
            return "Step execution failed: \(reason)"
        case .invalidParameter(let name):
            return "Invalid parameter: \(name)"
        case .timeout:
            return "Step timed out"
        }
    }
}
