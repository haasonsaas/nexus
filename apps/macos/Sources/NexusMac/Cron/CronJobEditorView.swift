import SwiftUI
import OSLog

// MARK: - Cron Schedule Model Extension

extension CronJobsStore.CronJob.Schedule {
    /// Compute the next N run times for this schedule.
    func nextRunTimes(count: Int) -> [Date] {
        guard count > 0 else { return [] }

        let calendar = Calendar.current
        var dates: [Date] = []
        var current = Date()

        switch type {
        case .interval:
            guard let interval = interval, interval > 0 else { return [] }
            for i in 1...count {
                dates.append(current.addingTimeInterval(interval * Double(i)))
            }

        case .daily:
            for i in 1...count {
                if let next = calendar.date(byAdding: .day, value: i, to: current) {
                    dates.append(calendar.startOfDay(for: next))
                }
            }

        case .weekly:
            for i in 1...count {
                if let next = calendar.date(byAdding: .weekOfYear, value: i, to: current) {
                    dates.append(calendar.startOfDay(for: next))
                }
            }

        case .monthly:
            for i in 1...count {
                if let next = calendar.date(byAdding: .month, value: i, to: current) {
                    dates.append(calendar.startOfDay(for: next))
                }
            }

        case .cron:
            guard let expression = cron else { return [] }
            dates = CronParser.nextRunTimes(expression: expression, count: count, from: current)
        }

        return dates
    }
}

// MARK: - Cron Parser

/// Simple cron expression parser for computing next run times.
enum CronParser {
    struct ParsedCron {
        let minutes: Set<Int>
        let hours: Set<Int>
        let daysOfMonth: Set<Int>
        let months: Set<Int>
        let daysOfWeek: Set<Int>
    }

    enum ValidationError: LocalizedError {
        case invalidFormat(String)
        case invalidField(String, String)
        case valueOutOfRange(String, Int, ClosedRange<Int>)

        var errorDescription: String? {
            switch self {
            case .invalidFormat(let msg):
                return "Invalid format: \(msg)"
            case .invalidField(let field, let value):
                return "Invalid \(field): '\(value)'"
            case .valueOutOfRange(let field, let value, let range):
                return "\(field) value \(value) out of range (\(range.lowerBound)-\(range.upperBound))"
            }
        }
    }

    /// Validate a cron expression and return any errors.
    static func validate(_ expression: String) -> ValidationError? {
        let parts = expression.trimmingCharacters(in: .whitespaces).split(separator: " ")

        guard parts.count == 5 else {
            return .invalidFormat("Expected 5 fields (minute hour day month weekday), got \(parts.count)")
        }

        let fields: [(name: String, value: String, range: ClosedRange<Int>)] = [
            ("minute", String(parts[0]), 0...59),
            ("hour", String(parts[1]), 0...23),
            ("day", String(parts[2]), 1...31),
            ("month", String(parts[3]), 1...12),
            ("weekday", String(parts[4]), 0...6)
        ]

        for (name, value, range) in fields {
            if let error = validateField(name: name, value: value, range: range) {
                return error
            }
        }

        return nil
    }

    private static func validateField(name: String, value: String, range: ClosedRange<Int>) -> ValidationError? {
        if value == "*" { return nil }

        // Handle */N syntax
        if value.hasPrefix("*/") {
            let stepStr = String(value.dropFirst(2))
            guard let step = Int(stepStr), step > 0 else {
                return .invalidField(name, value)
            }
            return nil
        }

        // Handle ranges like 1-5
        if value.contains("-") {
            let rangeParts = value.split(separator: "-")
            guard rangeParts.count == 2,
                  let start = Int(rangeParts[0]),
                  let end = Int(rangeParts[1]) else {
                return .invalidField(name, value)
            }
            if !range.contains(start) {
                return .valueOutOfRange(name, start, range)
            }
            if !range.contains(end) {
                return .valueOutOfRange(name, end, range)
            }
            return nil
        }

        // Handle comma-separated values
        if value.contains(",") {
            for part in value.split(separator: ",") {
                guard let num = Int(part) else {
                    return .invalidField(name, value)
                }
                if !range.contains(num) {
                    return .valueOutOfRange(name, num, range)
                }
            }
            return nil
        }

        // Single value
        guard let num = Int(value) else {
            return .invalidField(name, value)
        }
        if !range.contains(num) {
            return .valueOutOfRange(name, num, range)
        }

        return nil
    }

    /// Parse a cron field into a set of valid values.
    private static func parseField(_ field: String, range: ClosedRange<Int>) -> Set<Int> {
        if field == "*" {
            return Set(range)
        }

        if field.hasPrefix("*/") {
            let step = Int(field.dropFirst(2)) ?? 1
            return Set(stride(from: range.lowerBound, through: range.upperBound, by: step))
        }

        if field.contains("-") {
            let parts = field.split(separator: "-")
            if parts.count == 2, let start = Int(parts[0]), let end = Int(parts[1]) {
                return Set(start...end)
            }
        }

        if field.contains(",") {
            return Set(field.split(separator: ",").compactMap { Int($0) })
        }

        if let value = Int(field) {
            return [value]
        }

        return Set(range)
    }

    /// Compute the next N run times for a cron expression.
    static func nextRunTimes(expression: String, count: Int, from date: Date) -> [Date] {
        let parts = expression.trimmingCharacters(in: .whitespaces).split(separator: " ")
        guard parts.count == 5 else { return [] }

        let parsed = ParsedCron(
            minutes: parseField(String(parts[0]), range: 0...59),
            hours: parseField(String(parts[1]), range: 0...23),
            daysOfMonth: parseField(String(parts[2]), range: 1...31),
            months: parseField(String(parts[3]), range: 1...12),
            daysOfWeek: parseField(String(parts[4]), range: 0...6)
        )

        var results: [Date] = []
        var current = date
        let calendar = Calendar.current
        let maxIterations = 525600 // One year of minutes
        var iterations = 0

        while results.count < count && iterations < maxIterations {
            iterations += 1
            current = current.addingTimeInterval(60)

            let components = calendar.dateComponents([.minute, .hour, .day, .month, .weekday], from: current)
            guard let minute = components.minute,
                  let hour = components.hour,
                  let day = components.day,
                  let month = components.month,
                  let weekday = components.weekday else { continue }

            // weekday in Calendar is 1-7 (Sun=1), cron uses 0-6 (Sun=0)
            let cronWeekday = weekday - 1

            if parsed.minutes.contains(minute) &&
               parsed.hours.contains(hour) &&
               parsed.daysOfMonth.contains(day) &&
               parsed.months.contains(month) &&
               parsed.daysOfWeek.contains(cronWeekday) {
                results.append(current)
            }
        }

        return results
    }
}

// MARK: - Schedule Preset

enum SchedulePreset: String, CaseIterable, Identifiable {
    case everyMinute = "Every minute"
    case every5Minutes = "Every 5 minutes"
    case every15Minutes = "Every 15 minutes"
    case everyHour = "Every hour"
    case everyDay = "Daily at midnight"
    case everyWeekday = "Weekdays at 9am"
    case everyWeek = "Weekly on Monday"
    case everyMonth = "Monthly on the 1st"
    case custom = "Custom"

    var id: String { rawValue }

    var cronExpression: String? {
        switch self {
        case .everyMinute: return "* * * * *"
        case .every5Minutes: return "*/5 * * * *"
        case .every15Minutes: return "*/15 * * * *"
        case .everyHour: return "0 * * * *"
        case .everyDay: return "0 0 * * *"
        case .everyWeekday: return "0 9 * * 1-5"
        case .everyWeek: return "0 0 * * 1"
        case .everyMonth: return "0 0 1 * *"
        case .custom: return nil
        }
    }

    static func from(cron: String) -> SchedulePreset {
        for preset in allCases {
            if preset.cronExpression == cron {
                return preset
            }
        }
        return .custom
    }
}

// MARK: - Cron Job Editor View

/// Full CRUD editor for cron jobs with schedule builder and validation.
struct CronJobEditorView: View {
    @State private var cronStore = CronJobsStore.shared
    @State private var selectedJob: CronJobsStore.CronJob?
    @State private var isEditorPresented = false
    @State private var isDeleteConfirmPresented = false
    @State private var jobToDelete: CronJobsStore.CronJob?
    @State private var isCreatingNew = false

    private let logger = Logger(subsystem: "com.nexus.mac", category: "cron-editor")

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            headerView
            Divider()
            jobsList
        }
        .task {
            await cronStore.loadJobs()
        }
        .sheet(isPresented: $isEditorPresented) {
            if isCreatingNew {
                CronJobFormSheet(
                    mode: .create,
                    onSave: { job in
                        Task {
                            try? await cronStore.addJob(job)
                        }
                    }
                )
            } else if let job = selectedJob {
                CronJobFormSheet(
                    mode: .edit(job),
                    onSave: { updatedJob in
                        Task {
                            try? await cronStore.updateJob(updatedJob)
                        }
                    }
                )
            }
        }
        .confirmationDialog(
            "Delete Job",
            isPresented: $isDeleteConfirmPresented,
            presenting: jobToDelete
        ) { job in
            Button("Delete", role: .destructive) {
                Task {
                    try? await cronStore.removeJob(id: job.id)
                }
            }
            Button("Cancel", role: .cancel) {}
        } message: { job in
            Text("Are you sure you want to delete \"\(job.name)\"? This action cannot be undone.")
        }
    }

    // MARK: - Header

    private var headerView: some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                Text("Scheduled Jobs")
                    .font(.headline)
                Text("Automate tasks with cron schedules.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            if cronStore.isLoading {
                ProgressView()
                    .controlSize(.small)
            }

            Button {
                Task { await cronStore.loadJobs() }
            } label: {
                Image(systemName: "arrow.clockwise")
            }
            .buttonStyle(.borderless)
            .help("Refresh jobs")

            Button {
                isCreatingNew = true
                selectedJob = nil
                isEditorPresented = true
            } label: {
                Image(systemName: "plus")
            }
            .buttonStyle(.bordered)
            .help("Create new job")
        }
        .padding()
    }

    // MARK: - Jobs List

    @ViewBuilder
    private var jobsList: some View {
        if cronStore.jobs.isEmpty && !cronStore.isLoading {
            ContentUnavailableView(
                "No Scheduled Jobs",
                systemImage: "clock.badge.questionmark",
                description: Text("Create a cron job to automate tasks")
            )
        } else {
            List {
                ForEach(cronStore.jobs) { job in
                    CronJobRowView(
                        job: job,
                        onEdit: {
                            selectedJob = job
                            isCreatingNew = false
                            isEditorPresented = true
                        },
                        onDelete: {
                            jobToDelete = job
                            isDeleteConfirmPresented = true
                        },
                        onToggleEnabled: { enabled in
                            Task {
                                try? await cronStore.setEnabled(enabled, jobId: job.id)
                            }
                        },
                        onRunNow: {
                            Task {
                                try? await cronStore.runNow(jobId: job.id)
                                logger.info("Triggered manual run for job: \(job.name)")
                            }
                        }
                    )
                }
            }
            .listStyle(.inset)
        }
    }
}

// MARK: - Cron Job Row View

struct CronJobRowView: View {
    let job: CronJobsStore.CronJob
    let onEdit: () -> Void
    let onDelete: () -> Void
    let onToggleEnabled: (Bool) -> Void
    let onRunNow: () -> Void

    @State private var isHovered = false

    var body: some View {
        HStack(spacing: 12) {
            // Status indicator
            Circle()
                .fill(job.enabled ? Color.green : Color.gray)
                .frame(width: 8, height: 8)

            // Job info
            VStack(alignment: .leading, spacing: 4) {
                Text(job.name)
                    .font(.body.weight(.medium))

                HStack(spacing: 8) {
                    Label(job.schedule.displayString, systemImage: "clock")
                        .font(.caption)
                        .foregroundStyle(.secondary)

                    if let lastRun = job.lastRunAt {
                        Text("Last: \(lastRun, style: .relative) ago")
                            .font(.caption)
                            .foregroundStyle(.tertiary)
                    }

                    if job.runCount > 0 {
                        Text("\(job.runCount) runs")
                            .font(.caption)
                            .foregroundStyle(.tertiary)
                    }
                }

                if let nextRun = job.nextRunAt, job.enabled {
                    Text("Next: \(nextRun, style: .relative)")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }

            Spacer()

            // Actions
            HStack(spacing: 8) {
                if isHovered {
                    Button {
                        onRunNow()
                    } label: {
                        Image(systemName: "play.fill")
                    }
                    .buttonStyle(.borderless)
                    .help("Run now")

                    Button {
                        onEdit()
                    } label: {
                        Image(systemName: "pencil")
                    }
                    .buttonStyle(.borderless)
                    .help("Edit job")

                    Button {
                        onDelete()
                    } label: {
                        Image(systemName: "trash")
                            .foregroundStyle(.red)
                    }
                    .buttonStyle(.borderless)
                    .help("Delete job")
                }

                Toggle("", isOn: Binding(
                    get: { job.enabled },
                    set: { onToggleEnabled($0) }
                ))
                .toggleStyle(.switch)
                .labelsHidden()
            }
        }
        .padding(.vertical, 8)
        .contentShape(Rectangle())
        .onHover { hovering in
            withAnimation(.easeOut(duration: 0.15)) {
                isHovered = hovering
            }
        }
        .onTapGesture(count: 2) {
            onEdit()
        }
    }
}

// MARK: - Cron Job Form Sheet

struct CronJobFormSheet: View {
    enum Mode {
        case create
        case edit(CronJobsStore.CronJob)

        var isEditing: Bool {
            if case .edit = self { return true }
            return false
        }

        var existingJob: CronJobsStore.CronJob? {
            if case .edit(let job) = self { return job }
            return nil
        }
    }

    let mode: Mode
    let onSave: (CronJobsStore.CronJob) -> Void

    @Environment(\.dismiss) private var dismiss

    // Form state
    @State private var name = ""
    @State private var prompt = ""
    @State private var enabled = true
    @State private var notifyOnCompletion = true
    @State private var selectedPreset: SchedulePreset = .everyHour
    @State private var customCron = "0 * * * *"
    @State private var cronValidationError: CronParser.ValidationError?
    @State private var isTestRunning = false

    private let logger = Logger(subsystem: "com.nexus.mac", category: "cron-form")

    private var isValid: Bool {
        !name.trimmingCharacters(in: .whitespaces).isEmpty &&
        !prompt.trimmingCharacters(in: .whitespaces).isEmpty &&
        cronValidationError == nil
    }

    private var effectiveCron: String {
        selectedPreset.cronExpression ?? customCron
    }

    var body: some View {
        VStack(spacing: 0) {
            // Header
            HStack {
                Text(mode.isEditing ? "Edit Job" : "New Job")
                    .font(.headline)
                Spacer()
                Button {
                    dismiss()
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .foregroundStyle(.secondary)
                }
                .buttonStyle(.borderless)
            }
            .padding()

            Divider()

            // Form content
            ScrollView {
                Form {
                    // Basic info section
                    Section {
                        TextField("Job Name", text: $name)
                            .textFieldStyle(.roundedBorder)

                        VStack(alignment: .leading, spacing: 4) {
                            Text("Prompt / Command")
                                .font(.caption)
                                .foregroundStyle(.secondary)

                            TextEditor(text: $prompt)
                                .font(.body)
                                .frame(minHeight: 80, maxHeight: 150)
                                .overlay(
                                    RoundedRectangle(cornerRadius: 6)
                                        .stroke(Color.secondary.opacity(0.3), lineWidth: 1)
                                )
                        }
                    } header: {
                        Text("Job Details")
                    }

                    // Schedule section
                    Section {
                        scheduleBuilderSection
                    } header: {
                        Text("Schedule")
                    }

                    // Schedule preview section
                    Section {
                        schedulePreviewSection
                    } header: {
                        Text("Next Run Times")
                    }

                    // Options section
                    Section {
                        Toggle("Enabled", isOn: $enabled)
                        Toggle("Notify on Completion", isOn: $notifyOnCompletion)
                    } header: {
                        Text("Options")
                    }
                }
                .formStyle(.grouped)
            }

            Divider()

            // Footer buttons
            HStack {
                if mode.isEditing {
                    Button {
                        testRun()
                    } label: {
                        if isTestRunning {
                            ProgressView()
                                .controlSize(.small)
                        } else {
                            Label("Test Run", systemImage: "play.fill")
                        }
                    }
                    .disabled(isTestRunning)
                }

                Spacer()

                Button("Cancel") {
                    dismiss()
                }
                .keyboardShortcut(.cancelAction)

                Button(mode.isEditing ? "Save" : "Create") {
                    saveJob()
                }
                .keyboardShortcut(.defaultAction)
                .buttonStyle(.borderedProminent)
                .disabled(!isValid)
            }
            .padding()
        }
        .frame(width: 500, height: 600)
        .onAppear {
            loadExistingJob()
        }
        .onChange(of: customCron) { _, newValue in
            validateCron(newValue)
        }
        .onChange(of: selectedPreset) { _, newPreset in
            if let cron = newPreset.cronExpression {
                customCron = cron
            }
            validateCron(effectiveCron)
        }
    }

    // MARK: - Schedule Builder

    private var scheduleBuilderSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Picker("Preset", selection: $selectedPreset) {
                ForEach(SchedulePreset.allCases) { preset in
                    Text(preset.rawValue).tag(preset)
                }
            }
            .pickerStyle(.menu)

            if selectedPreset == .custom {
                VStack(alignment: .leading, spacing: 4) {
                    TextField("Cron Expression", text: $customCron)
                        .textFieldStyle(.roundedBorder)
                        .font(.system(.body, design: .monospaced))

                    Text("Format: minute hour day month weekday")
                        .font(.caption)
                        .foregroundStyle(.secondary)

                    if let error = cronValidationError {
                        HStack(spacing: 4) {
                            Image(systemName: "exclamationmark.triangle.fill")
                                .foregroundStyle(.orange)
                            Text(error.localizedDescription)
                                .font(.caption)
                                .foregroundStyle(.orange)
                        }
                    }
                }
            }

            // Cron expression reference
            DisclosureGroup("Cron Syntax Reference") {
                VStack(alignment: .leading, spacing: 6) {
                    cronSyntaxRow("*", "Any value")
                    cronSyntaxRow("*/N", "Every N units")
                    cronSyntaxRow("1,2,3", "Specific values")
                    cronSyntaxRow("1-5", "Range of values")
                    Divider()
                    cronSyntaxRow("0 9 * * 1-5", "9am on weekdays")
                    cronSyntaxRow("0 0 1 * *", "Midnight on the 1st")
                    cronSyntaxRow("*/15 * * * *", "Every 15 minutes")
                }
                .font(.caption)
                .padding(.top, 4)
            }
            .font(.caption)
            .foregroundStyle(.secondary)
        }
    }

    private func cronSyntaxRow(_ syntax: String, _ description: String) -> some View {
        HStack {
            Text(syntax)
                .font(.system(.caption, design: .monospaced))
                .frame(width: 80, alignment: .leading)
            Text(description)
                .foregroundStyle(.secondary)
        }
    }

    // MARK: - Schedule Preview

    private var schedulePreviewSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            let schedule = CronJobsStore.CronJob.Schedule(
                type: .cron,
                cron: effectiveCron,
                interval: nil,
                timezone: nil
            )
            let nextTimes = schedule.nextRunTimes(count: 5)

            if nextTimes.isEmpty {
                Text("Unable to compute next run times")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            } else {
                ForEach(Array(nextTimes.enumerated()), id: \.offset) { index, date in
                    HStack {
                        Text("\(index + 1).")
                            .font(.caption.monospacedDigit())
                            .foregroundStyle(.tertiary)
                            .frame(width: 20, alignment: .trailing)

                        Text(date, format: .dateTime.weekday(.abbreviated).month(.abbreviated).day().hour().minute())
                            .font(.caption)

                        Spacer()

                        Text(date, style: .relative)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        }
    }

    // MARK: - Actions

    private func loadExistingJob() {
        guard let job = mode.existingJob else { return }

        name = job.name
        prompt = job.command.message ?? job.command.bash ?? ""
        enabled = job.enabled

        if let cron = job.schedule.cron {
            selectedPreset = SchedulePreset.from(cron: cron)
            customCron = cron
        }

        validateCron(effectiveCron)
    }

    private func validateCron(_ expression: String) {
        cronValidationError = CronParser.validate(expression)
    }

    private func saveJob() {
        let schedule = CronJobsStore.CronJob.Schedule(
            type: .cron,
            cron: effectiveCron,
            interval: nil,
            timezone: TimeZone.current.identifier
        )

        let command = CronJobsStore.CronJob.Command(
            type: .agent,
            message: prompt.trimmingCharacters(in: .whitespacesAndNewlines),
            workflowId: nil,
            bash: nil
        )

        let job = CronJobsStore.CronJob(
            id: mode.existingJob?.id ?? UUID().uuidString,
            name: name.trimmingCharacters(in: .whitespacesAndNewlines),
            schedule: schedule,
            command: command,
            enabled: enabled,
            lastRunAt: mode.existingJob?.lastRunAt,
            nextRunAt: nil,
            runCount: mode.existingJob?.runCount ?? 0
        )

        logger.info("Saving cron job: \(job.name)")
        onSave(job)
        dismiss()
    }

    private func testRun() {
        guard let job = mode.existingJob else { return }

        isTestRunning = true
        logger.info("Starting test run for job: \(job.name)")

        Task {
            do {
                try await CronJobsStore.shared.runNow(jobId: job.id)
                logger.info("Test run triggered successfully")
            } catch {
                logger.error("Test run failed: \(error.localizedDescription)")
            }

            await MainActor.run {
                isTestRunning = false
            }
        }
    }
}

// MARK: - Preview

#Preview("Editor") {
    CronJobEditorView()
        .frame(width: 550, height: 500)
}

#Preview("Form - Create") {
    CronJobFormSheet(mode: .create) { job in
        print("Created: \(job.name)")
    }
}

#Preview("Form - Edit") {
    let sampleJob = CronJobsStore.CronJob(
        id: "test-123",
        name: "Daily Backup",
        schedule: .init(type: .cron, cron: "0 0 * * *", interval: nil, timezone: nil),
        command: .init(type: .agent, message: "Run daily backup and summarize results", workflowId: nil, bash: nil),
        enabled: true,
        lastRunAt: Date().addingTimeInterval(-3600),
        nextRunAt: Date().addingTimeInterval(3600),
        runCount: 42
    )

    CronJobFormSheet(mode: .edit(sampleJob)) { job in
        print("Updated: \(job.name)")
    }
}
