import Foundation
import OSLog
import UserNotifications

// MARK: - Cron Job Definition

/// Cron job definition for scheduled automation tasks.
struct CronJob: Identifiable, Codable, Sendable {
    let id: String
    var name: String
    var schedule: CronSchedule
    var command: String
    var enabled: Bool
    var createdAt: Date
    var lastRunAt: Date?
    var lastResult: CronJobResult?
    var runCount: Int

    init(
        id: String = UUID().uuidString,
        name: String,
        schedule: CronSchedule,
        command: String
    ) {
        self.id = id
        self.name = name
        self.schedule = schedule
        self.command = command
        self.enabled = true
        self.createdAt = Date()
        self.lastRunAt = nil
        self.lastResult = nil
        self.runCount = 0
    }
}

// MARK: - Cron Schedule Expression

/// Cron schedule expression with standard cron field semantics.
struct CronSchedule: Codable, Sendable, Equatable {
    var minute: String      // 0-59
    var hour: String        // 0-23
    var dayOfMonth: String  // 1-31
    var month: String       // 1-12
    var dayOfWeek: String   // 0-6 (Sunday = 0)

    /// The full cron expression string.
    var expression: String {
        "\(minute) \(hour) \(dayOfMonth) \(month) \(dayOfWeek)"
    }

    // MARK: - Preset Schedules

    static let everyMinute = CronSchedule(
        minute: "*",
        hour: "*",
        dayOfMonth: "*",
        month: "*",
        dayOfWeek: "*"
    )

    static let every5Minutes = CronSchedule(
        minute: "*/5",
        hour: "*",
        dayOfMonth: "*",
        month: "*",
        dayOfWeek: "*"
    )

    static let every15Minutes = CronSchedule(
        minute: "*/15",
        hour: "*",
        dayOfMonth: "*",
        month: "*",
        dayOfWeek: "*"
    )

    static let hourly = CronSchedule(
        minute: "0",
        hour: "*",
        dayOfMonth: "*",
        month: "*",
        dayOfWeek: "*"
    )

    static let daily = CronSchedule(
        minute: "0",
        hour: "9",
        dayOfMonth: "*",
        month: "*",
        dayOfWeek: "*"
    )

    static let weekly = CronSchedule(
        minute: "0",
        hour: "9",
        dayOfMonth: "*",
        month: "*",
        dayOfWeek: "1"
    )

    static let monthly = CronSchedule(
        minute: "0",
        hour: "9",
        dayOfMonth: "1",
        month: "*",
        dayOfWeek: "*"
    )

    // MARK: - Next Run Calculation

    /// Calculate the next run time from a given date.
    /// - Parameter date: The reference date (defaults to current time).
    /// - Returns: The next scheduled run time, or nil if unable to calculate.
    func nextRun(from date: Date = Date()) -> Date? {
        let calendar = Calendar.current
        var searchDate = date
        let maxIterations = 525600 // One year of minutes

        for _ in 0..<maxIterations {
            searchDate = searchDate.addingTimeInterval(60)

            let components = calendar.dateComponents(
                [.minute, .hour, .day, .month, .weekday],
                from: searchDate
            )

            guard let minute = components.minute,
                  let hour = components.hour,
                  let day = components.day,
                  let month = components.month,
                  let weekday = components.weekday else { continue }

            // weekday in Calendar is 1-7 (Sun=1), cron uses 0-6 (Sun=0)
            let cronWeekday = weekday - 1

            if matchesCronField(self.minute, value: minute) &&
               matchesCronField(self.hour, value: hour) &&
               matchesCronField(self.dayOfMonth, value: day) &&
               matchesCronField(self.month, value: month) &&
               matchesCronField(self.dayOfWeek, value: cronWeekday) {
                return searchDate
            }
        }

        return nil
    }

    /// Calculate the next N run times.
    /// - Parameters:
    ///   - count: Number of run times to calculate.
    ///   - date: The reference date (defaults to current time).
    /// - Returns: Array of next scheduled run times.
    func nextRunTimes(count: Int, from date: Date = Date()) -> [Date] {
        var results: [Date] = []
        var current = date

        for _ in 0..<count {
            guard let next = nextRun(from: current) else { break }
            results.append(next)
            current = next
        }

        return results
    }

    /// Human-readable description of the schedule.
    var humanReadable: String {
        switch (minute, hour, dayOfMonth, month, dayOfWeek) {
        case ("*", "*", "*", "*", "*"):
            return "Every minute"
        case ("*/5", "*", "*", "*", "*"):
            return "Every 5 minutes"
        case ("*/15", "*", "*", "*", "*"):
            return "Every 15 minutes"
        case ("*/30", "*", "*", "*", "*"):
            return "Every 30 minutes"
        case ("0", "*", "*", "*", "*"):
            return "Every hour"
        case ("0", let h, "*", "*", "*") where h != "*":
            return "Daily at \(h):00"
        case ("0", let h, "*", "*", "1-5") where h != "*":
            return "Weekdays at \(h):00"
        case ("0", let h, "*", "*", "1") where h != "*":
            return "Weekly on Monday at \(h):00"
        case ("0", let h, "1", "*", "*") where h != "*":
            return "Monthly on the 1st at \(h):00"
        default:
            return expression
        }
    }

    // MARK: - Private Helpers

    private func matchesCronField(_ field: String, value: Int) -> Bool {
        if field == "*" { return true }

        // Single value
        if let fieldValue = Int(field) {
            return fieldValue == value
        }

        // Step values (*/N)
        if field.hasPrefix("*/") {
            let stepStr = String(field.dropFirst(2))
            if let step = Int(stepStr), step > 0 {
                return value % step == 0
            }
        }

        // Ranges (N-M)
        if field.contains("-") && !field.contains(",") {
            let parts = field.split(separator: "-").compactMap { Int($0) }
            if parts.count == 2 {
                return value >= parts[0] && value <= parts[1]
            }
        }

        // Lists (N,M,O)
        if field.contains(",") {
            let values = field.split(separator: ",").compactMap { Int($0) }
            return values.contains(value)
        }

        return false
    }
}

// MARK: - Cron Job Result

/// Result of a cron job execution.
struct CronJobResult: Codable, Sendable {
    let success: Bool
    let output: String?
    let error: String?
    let duration: TimeInterval
    let timestamp: Date

    init(
        success: Bool,
        output: String? = nil,
        error: String? = nil,
        duration: TimeInterval,
        timestamp: Date = Date()
    ) {
        self.success = success
        self.output = output
        self.error = error
        self.duration = duration
        self.timestamp = timestamp
    }
}

// MARK: - Cron Scheduler

/// Schedules and executes cron jobs with timer-based checking.
/// Provides local execution capability independent of the gateway.
@MainActor
@Observable
final class CronScheduler {
    static let shared = CronScheduler()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "cron-scheduler")

    // MARK: - State

    private(set) var jobs: [CronJob] = []
    private(set) var isRunning = false
    private(set) var nextScheduledRun: Date?

    // MARK: - History

    private(set) var executionHistory: [String: [CronJobResult]] = [:] // jobId -> results
    private let maxHistoryPerJob = 100

    // MARK: - Timer

    private var schedulerTimer: Timer?
    private let checkInterval: TimeInterval = 60 // Check every minute

    // MARK: - Storage

    private let storageURL: URL
    private let historyURL: URL

    // MARK: - Notifications

    private let notificationCenter = UNUserNotificationCenter.current()

    // MARK: - Initialization

    init() {
        let appSupport = FileManager.default.urls(
            for: .applicationSupportDirectory,
            in: .userDomainMask
        ).first!
        let nexusDir = appSupport.appendingPathComponent("Nexus")
        storageURL = nexusDir.appendingPathComponent("cron-scheduler-jobs.json")
        historyURL = nexusDir.appendingPathComponent("cron-scheduler-history.json")

        loadJobs()
        loadHistory()
    }

    // MARK: - Lifecycle

    /// Start the cron scheduler.
    func start() {
        guard !isRunning else {
            logger.debug("Cron scheduler already running")
            return
        }

        isRunning = true
        scheduleNextCheck()
        updateNextScheduledRun()

        logger.info("Cron scheduler started with \(self.jobs.count) jobs")
    }

    /// Stop the cron scheduler.
    func stop() {
        schedulerTimer?.invalidate()
        schedulerTimer = nil
        isRunning = false
        nextScheduledRun = nil

        logger.info("Cron scheduler stopped")
    }

    /// Restart the scheduler (stop and start).
    func restart() {
        stop()
        start()
    }

    // MARK: - Job Management

    /// Add a new cron job.
    /// - Parameter job: The job to add.
    func addJob(_ job: CronJob) {
        jobs.append(job)
        saveJobs()
        updateNextScheduledRun()

        logger.info("Added cron job: \(job.name) [\(job.id)]")
    }

    /// Update an existing cron job.
    /// - Parameter job: The job with updated values.
    func updateJob(_ job: CronJob) {
        guard let index = jobs.firstIndex(where: { $0.id == job.id }) else {
            logger.warning("Cannot update job: not found [\(job.id)]")
            return
        }

        jobs[index] = job
        saveJobs()
        updateNextScheduledRun()

        logger.info("Updated cron job: \(job.name) [\(job.id)]")
    }

    /// Remove a cron job.
    /// - Parameter jobId: The ID of the job to remove.
    func removeJob(_ jobId: String) {
        let jobName = jobs.first { $0.id == jobId }?.name ?? "unknown"

        jobs.removeAll { $0.id == jobId }
        executionHistory.removeValue(forKey: jobId)

        saveJobs()
        saveHistory()
        updateNextScheduledRun()

        logger.info("Removed cron job: \(jobName) [\(jobId)]")
    }

    /// Enable or disable a cron job.
    /// - Parameters:
    ///   - jobId: The ID of the job.
    ///   - enabled: Whether the job should be enabled.
    func enableJob(_ jobId: String, enabled: Bool) {
        guard let index = jobs.firstIndex(where: { $0.id == jobId }) else {
            return
        }

        jobs[index].enabled = enabled
        saveJobs()
        updateNextScheduledRun()

        logger.info("Job \(enabled ? "enabled" : "disabled"): \(self.jobs[index].name)")
    }

    /// Get a job by its ID.
    /// - Parameter jobId: The job ID.
    /// - Returns: The job if found, nil otherwise.
    func job(for jobId: String) -> CronJob? {
        jobs.first { $0.id == jobId }
    }

    /// Get all enabled jobs.
    var enabledJobs: [CronJob] {
        jobs.filter { $0.enabled }
    }

    // MARK: - Execution

    /// Run a job immediately (manual trigger).
    /// - Parameter jobId: The ID of the job to run.
    func runJob(_ jobId: String) async {
        guard let index = jobs.firstIndex(where: { $0.id == jobId }) else {
            logger.error("Cannot run job: not found [\(jobId)]")
            return
        }

        let job = jobs[index]
        logger.info("Running cron job: \(job.name) [\(job.id)]")

        let startTime = Date()

        do {
            let result = try await executeCommand(job.command, jobName: job.name)

            let duration = Date().timeIntervalSince(startTime)
            let jobResult = CronJobResult(
                success: true,
                output: result,
                error: nil,
                duration: duration
            )

            jobs[index].lastRunAt = Date()
            jobs[index].lastResult = jobResult
            jobs[index].runCount += 1

            addToHistory(jobId: jobId, result: jobResult)
            saveJobs()

            logger.info("Cron job completed: \(job.name) in \(String(format: "%.2f", duration))s")

            await sendNotification(
                title: "Cron Job Completed",
                body: "\(job.name) finished successfully",
                success: true
            )

        } catch {
            let duration = Date().timeIntervalSince(startTime)
            let jobResult = CronJobResult(
                success: false,
                output: nil,
                error: error.localizedDescription,
                duration: duration
            )

            jobs[index].lastRunAt = Date()
            jobs[index].lastResult = jobResult
            jobs[index].runCount += 1

            addToHistory(jobId: jobId, result: jobResult)
            saveJobs()

            logger.error("Cron job failed: \(job.name) - \(error.localizedDescription)")

            await sendNotification(
                title: "Cron Job Failed",
                body: "\(job.name): \(error.localizedDescription)",
                success: false
            )
        }
    }

    /// Execute a command string.
    /// - Parameters:
    ///   - command: The command to execute.
    ///   - jobName: The name of the job (for logging).
    /// - Returns: The execution result string.
    private func executeCommand(_ command: String, jobName: String) async throws -> String {
        // Try to send to ChatSessionManager if available
        do {
            try await ChatSessionManager.shared.send(content: command)
            return "Command sent to agent: \(command)"
        } catch {
            logger.warning("Failed to send to chat session: \(error.localizedDescription)")

            // Fallback: Try to execute via AgentOrchestrator
            let request = AgentOrchestrator.AgentRequest(
                agentId: "cron-\(UUID().uuidString.prefix(8))",
                type: "cron",
                action: "execute",
                parameters: ["command": AnyCodable(command)]
            )

            let response = try await AgentOrchestrator.shared.processRequest(request)

            if response.success {
                return response.result as? String ?? "Command executed"
            } else {
                throw CronSchedulerError.executionFailed(response.error ?? "Unknown error")
            }
        }
    }

    // MARK: - Scheduling

    private func scheduleNextCheck() {
        schedulerTimer?.invalidate()

        schedulerTimer = Timer.scheduledTimer(
            withTimeInterval: checkInterval,
            repeats: true
        ) { [weak self] _ in
            Task { @MainActor in
                self?.checkScheduledJobs()
            }
        }

        // Run mode to ensure timer fires during scroll/resize
        RunLoop.main.add(schedulerTimer!, forMode: .common)

        // Also check immediately
        checkScheduledJobs()
    }

    private func checkScheduledJobs() {
        let now = Date()

        for job in jobs where job.enabled {
            if shouldRunJob(job, at: now) {
                Task {
                    await runJob(job.id)
                }
            }
        }

        updateNextScheduledRun()
    }

    private func shouldRunJob(_ job: CronJob, at date: Date) -> Bool {
        let calendar = Calendar.current
        let minute = calendar.component(.minute, from: date)
        let hour = calendar.component(.hour, from: date)
        let day = calendar.component(.day, from: date)
        let month = calendar.component(.month, from: date)
        let weekday = calendar.component(.weekday, from: date) - 1 // Make Sunday = 0

        // Check if already run this minute to prevent duplicate executions
        if let lastRun = job.lastRunAt {
            let lastMinute = calendar.component(.minute, from: lastRun)
            let lastHour = calendar.component(.hour, from: lastRun)
            let lastDay = calendar.component(.day, from: lastRun)

            if calendar.isDate(date, inSameDayAs: lastRun) &&
               lastHour == hour &&
               lastMinute == minute {
                return false
            }
        }

        return matchesCronField(job.schedule.minute, value: minute) &&
               matchesCronField(job.schedule.hour, value: hour) &&
               matchesCronField(job.schedule.dayOfMonth, value: day) &&
               matchesCronField(job.schedule.month, value: month) &&
               matchesCronField(job.schedule.dayOfWeek, value: weekday)
    }

    private func matchesCronField(_ field: String, value: Int) -> Bool {
        if field == "*" { return true }

        if let fieldValue = Int(field) {
            return fieldValue == value
        }

        // Step values (*/N)
        if field.hasPrefix("*/") {
            let stepStr = String(field.dropFirst(2))
            if let step = Int(stepStr), step > 0 {
                return value % step == 0
            }
        }

        // Ranges (N-M)
        if field.contains("-") && !field.contains(",") {
            let parts = field.split(separator: "-").compactMap { Int($0) }
            if parts.count == 2 {
                return value >= parts[0] && value <= parts[1]
            }
        }

        // Lists (N,M,O)
        if field.contains(",") {
            let values = field.split(separator: ",").compactMap { Int($0) }
            return values.contains(value)
        }

        return false
    }

    private func updateNextScheduledRun() {
        var earliest: Date?

        for job in jobs where job.enabled {
            if let next = job.schedule.nextRun() {
                if earliest == nil || next < earliest! {
                    earliest = next
                }
            }
        }

        nextScheduledRun = earliest
    }

    // MARK: - History

    private func addToHistory(jobId: String, result: CronJobResult) {
        var history = executionHistory[jobId] ?? []
        history.insert(result, at: 0)

        // Trim to max history
        if history.count > maxHistoryPerJob {
            history = Array(history.prefix(maxHistoryPerJob))
        }

        executionHistory[jobId] = history
        saveHistory()
    }

    /// Get execution history for a job.
    /// - Parameter jobId: The job ID.
    /// - Returns: Array of execution results, newest first.
    func getHistory(for jobId: String) -> [CronJobResult] {
        executionHistory[jobId] ?? []
    }

    /// Clear execution history for a job.
    /// - Parameter jobId: The job ID.
    func clearHistory(for jobId: String) {
        executionHistory.removeValue(forKey: jobId)
        saveHistory()
        logger.info("Cleared history for job: \(jobId)")
    }

    /// Clear all execution history.
    func clearAllHistory() {
        executionHistory.removeAll()
        saveHistory()
        logger.info("Cleared all cron job history")
    }

    // MARK: - Notifications

    private func sendNotification(title: String, body: String, success: Bool) async {
        let content = UNMutableNotificationContent()
        content.title = title
        content.body = body
        content.sound = success ? .default : UNNotificationSound.defaultCritical

        if !success {
            content.interruptionLevel = .timeSensitive
        }

        let request = UNNotificationRequest(
            identifier: UUID().uuidString,
            content: content,
            trigger: nil
        )

        do {
            try await notificationCenter.add(request)
        } catch {
            logger.warning("Failed to send notification: \(error.localizedDescription)")
        }
    }

    // MARK: - Persistence

    private func loadJobs() {
        do {
            guard FileManager.default.fileExists(atPath: storageURL.path) else {
                jobs = []
                return
            }

            let data = try Data(contentsOf: storageURL)
            let decoder = JSONDecoder()
            decoder.dateDecodingStrategy = .iso8601
            jobs = try decoder.decode([CronJob].self, from: data)

            logger.debug("Loaded \(self.jobs.count) cron jobs")
        } catch {
            logger.error("Failed to load cron jobs: \(error.localizedDescription)")
            jobs = []
        }
    }

    private func saveJobs() {
        do {
            try FileManager.default.createDirectory(
                at: storageURL.deletingLastPathComponent(),
                withIntermediateDirectories: true
            )

            let encoder = JSONEncoder()
            encoder.dateEncodingStrategy = .iso8601
            encoder.outputFormatting = [.prettyPrinted, .sortedKeys]

            let data = try encoder.encode(jobs)
            try data.write(to: storageURL, options: [.atomic])

            // Set restrictive permissions
            try? FileManager.default.setAttributes(
                [.posixPermissions: 0o600],
                ofItemAtPath: storageURL.path
            )

            logger.debug("Saved \(self.jobs.count) cron jobs")
        } catch {
            logger.error("Failed to save cron jobs: \(error.localizedDescription)")
        }
    }

    private func loadHistory() {
        do {
            guard FileManager.default.fileExists(atPath: historyURL.path) else {
                executionHistory = [:]
                return
            }

            let data = try Data(contentsOf: historyURL)
            let decoder = JSONDecoder()
            decoder.dateDecodingStrategy = .iso8601
            executionHistory = try decoder.decode([String: [CronJobResult]].self, from: data)

            logger.debug("Loaded cron job history for \(self.executionHistory.count) jobs")
        } catch {
            logger.error("Failed to load cron job history: \(error.localizedDescription)")
            executionHistory = [:]
        }
    }

    private func saveHistory() {
        do {
            try FileManager.default.createDirectory(
                at: historyURL.deletingLastPathComponent(),
                withIntermediateDirectories: true
            )

            let encoder = JSONEncoder()
            encoder.dateEncodingStrategy = .iso8601
            encoder.outputFormatting = [.prettyPrinted, .sortedKeys]

            let data = try encoder.encode(executionHistory)
            try data.write(to: historyURL, options: [.atomic])

            try? FileManager.default.setAttributes(
                [.posixPermissions: 0o600],
                ofItemAtPath: historyURL.path
            )
        } catch {
            logger.error("Failed to save cron job history: \(error.localizedDescription)")
        }
    }

    // MARK: - Import/Export

    /// Export jobs to JSON data.
    /// - Returns: JSON data containing all jobs.
    func exportJobs() throws -> Data {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        return try encoder.encode(jobs)
    }

    /// Import jobs from JSON data.
    /// - Parameters:
    ///   - data: JSON data containing jobs.
    ///   - merge: If true, merge with existing jobs; if false, replace all.
    func importJobs(from data: Data, merge: Bool = true) throws {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let importedJobs = try decoder.decode([CronJob].self, from: data)

        if merge {
            // Merge: update existing, add new
            for job in importedJobs {
                if let index = jobs.firstIndex(where: { $0.id == job.id }) {
                    jobs[index] = job
                } else {
                    jobs.append(job)
                }
            }
        } else {
            // Replace all
            jobs = importedJobs
        }

        saveJobs()
        updateNextScheduledRun()

        logger.info("Imported \(importedJobs.count) cron jobs (merge: \(merge))")
    }
}

// MARK: - Cron Scheduler Error

enum CronSchedulerError: LocalizedError {
    case executionFailed(String)
    case jobNotFound(String)
    case invalidSchedule(String)

    var errorDescription: String? {
        switch self {
        case .executionFailed(let reason):
            return "Cron job execution failed: \(reason)"
        case .jobNotFound(let id):
            return "Cron job not found: \(id)"
        case .invalidSchedule(let expression):
            return "Invalid cron schedule: \(expression)"
        }
    }
}

// MARK: - Cron Preset

/// Common cron schedule presets for quick selection.
enum CronPreset: String, CaseIterable, Identifiable {
    case everyMinute = "every_minute"
    case every5Minutes = "every_5_minutes"
    case every15Minutes = "every_15_minutes"
    case every30Minutes = "every_30_minutes"
    case hourly = "hourly"
    case daily = "daily"
    case weekly = "weekly"
    case monthly = "monthly"

    var id: String { rawValue }

    var title: String {
        switch self {
        case .everyMinute: return "Every Minute"
        case .every5Minutes: return "Every 5 Minutes"
        case .every15Minutes: return "Every 15 Minutes"
        case .every30Minutes: return "Every 30 Minutes"
        case .hourly: return "Hourly"
        case .daily: return "Daily"
        case .weekly: return "Weekly"
        case .monthly: return "Monthly"
        }
    }

    var schedule: CronSchedule {
        switch self {
        case .everyMinute: return .everyMinute
        case .every5Minutes: return .every5Minutes
        case .every15Minutes: return .every15Minutes
        case .every30Minutes:
            return CronSchedule(
                minute: "*/30",
                hour: "*",
                dayOfMonth: "*",
                month: "*",
                dayOfWeek: "*"
            )
        case .hourly: return .hourly
        case .daily: return .daily
        case .weekly: return .weekly
        case .monthly: return .monthly
        }
    }

    var description: String {
        schedule.humanReadable
    }
}
