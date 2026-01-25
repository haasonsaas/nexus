import Foundation
import OSLog

/// Manages scheduled cron jobs for automated agent tasks.
/// Enables time-based automation and recurring workflows.
@MainActor
@Observable
final class CronJobsStore {
    static let shared = CronJobsStore()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "cron")

    private(set) var jobs: [CronJob] = []
    private(set) var recentRuns: [CronRun] = []
    private(set) var isLoading = false

    struct CronJob: Identifiable, Codable, Equatable {
        let id: String
        var name: String
        var schedule: Schedule
        var command: Command
        var enabled: Bool
        var lastRunAt: Date?
        var nextRunAt: Date?
        var runCount: Int

        struct Schedule: Codable, Equatable {
            var type: ScheduleType
            var cron: String?
            var interval: TimeInterval?
            var timezone: String?

            enum ScheduleType: String, Codable {
                case cron
                case interval
                case daily
                case weekly
                case monthly
            }

            var displayString: String {
                switch type {
                case .cron:
                    return cron ?? "* * * * *"
                case .interval:
                    if let interval {
                        if interval < 60 {
                            return "Every \(Int(interval))s"
                        } else if interval < 3600 {
                            return "Every \(Int(interval / 60))m"
                        } else {
                            return "Every \(Int(interval / 3600))h"
                        }
                    }
                    return "Unknown"
                case .daily:
                    return "Daily"
                case .weekly:
                    return "Weekly"
                case .monthly:
                    return "Monthly"
                }
            }
        }

        struct Command: Codable, Equatable {
            var type: CommandType
            var message: String?
            var workflowId: String?
            var bash: String?

            enum CommandType: String, Codable {
                case agent
                case workflow
                case bash
            }
        }
    }

    struct CronRun: Identifiable, Codable {
        let id: String
        let jobId: String
        let startedAt: Date
        var completedAt: Date?
        var status: RunStatus
        var output: String?
        var error: String?

        enum RunStatus: String, Codable {
            case running
            case completed
            case failed
            case cancelled
        }
    }

    // MARK: - Job Management

    /// Load jobs from gateway
    func loadJobs() async {
        isLoading = true
        defer { isLoading = false }

        do {
            let data = try await ControlChannel.shared.request(method: "cron.list")
            let response = try JSONDecoder().decode(CronListResponse.self, from: data)
            jobs = response.jobs
            logger.info("loaded \(self.jobs.count) cron jobs")
        } catch {
            logger.error("failed to load cron jobs: \(error.localizedDescription)")
        }
    }

    /// Add a new cron job
    func addJob(_ job: CronJob) async throws {
        let params: [String: AnyHashable] = [
            "name": job.name,
            "schedule": try encodeSchedule(job.schedule),
            "command": try encodeCommand(job.command),
            "enabled": job.enabled
        ]

        _ = try await ControlChannel.shared.request(
            method: "cron.add",
            params: params
        )

        await loadJobs()
        logger.info("cron job added name=\(job.name)")
    }

    /// Update an existing job
    func updateJob(_ job: CronJob) async throws {
        let params: [String: AnyHashable] = [
            "jobId": job.id,
            "name": job.name,
            "schedule": try encodeSchedule(job.schedule),
            "command": try encodeCommand(job.command),
            "enabled": job.enabled
        ]

        _ = try await ControlChannel.shared.request(
            method: "cron.update",
            params: params
        )

        if let index = jobs.firstIndex(where: { $0.id == job.id }) {
            jobs[index] = job
        }
        logger.info("cron job updated id=\(job.id)")
    }

    /// Remove a job
    func removeJob(id: String) async throws {
        _ = try await ControlChannel.shared.request(
            method: "cron.remove",
            params: ["jobId": id]
        )

        jobs.removeAll { $0.id == id }
        logger.info("cron job removed id=\(id)")
    }

    /// Enable/disable a job
    func setEnabled(_ enabled: Bool, jobId: String) async throws {
        guard let index = jobs.firstIndex(where: { $0.id == jobId }) else { return }

        var job = jobs[index]
        job.enabled = enabled

        try await updateJob(job)
    }

    /// Run a job immediately
    func runNow(jobId: String) async throws {
        _ = try await ControlChannel.shared.request(
            method: "cron.run",
            params: ["jobId": jobId]
        )

        logger.info("cron job triggered id=\(jobId)")
    }

    // MARK: - Run History

    /// Load recent runs for a job
    func loadRuns(jobId: String) async throws {
        let data = try await ControlChannel.shared.request(
            method: "cron.runs",
            params: ["jobId": jobId, "limit": 50]
        )

        let response = try JSONDecoder().decode(CronRunsResponse.self, from: data)
        recentRuns = response.runs
    }

    /// Cancel a running job
    func cancelRun(runId: String) async throws {
        _ = try await ControlChannel.shared.request(
            method: "cron.cancel",
            params: ["runId": runId]
        )

        if let index = recentRuns.firstIndex(where: { $0.id == runId }) {
            recentRuns[index].status = .cancelled
        }
    }

    // MARK: - Private

    private func encodeSchedule(_ schedule: CronJob.Schedule) throws -> String {
        let data = try JSONEncoder().encode(schedule)
        return data.base64EncodedString()
    }

    private func encodeCommand(_ command: CronJob.Command) throws -> String {
        let data = try JSONEncoder().encode(command)
        return data.base64EncodedString()
    }
}

// MARK: - Response Models

private struct CronListResponse: Codable {
    let jobs: [CronJobsStore.CronJob]
}

private struct CronRunsResponse: Codable {
    let runs: [CronJobsStore.CronRun]
}
