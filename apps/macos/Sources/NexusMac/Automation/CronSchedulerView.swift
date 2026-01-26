import SwiftUI
import OSLog

// MARK: - Cron Scheduler View

/// Main view for managing local cron jobs scheduled by CronScheduler.
struct CronSchedulerView: View {
    @State private var scheduler = CronScheduler.shared
    @State private var selectedJob: CronJob?
    @State private var isEditorPresented = false
    @State private var isCreatingNew = false
    @State private var isDeleteConfirmPresented = false
    @State private var jobToDelete: CronJob?
    @State private var searchText = ""

    private let logger = Logger(subsystem: "com.nexus.mac", category: "cron-scheduler-view")

    private var filteredJobs: [CronJob] {
        if searchText.isEmpty {
            return scheduler.jobs
        }
        return scheduler.jobs.filter {
            $0.name.localizedCaseInsensitiveContains(searchText) ||
            $0.command.localizedCaseInsensitiveContains(searchText)
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            headerView
            Divider()

            if scheduler.jobs.isEmpty {
                emptyStateView
            } else {
                jobsListView
            }
        }
        .sheet(isPresented: $isEditorPresented) {
            if isCreatingNew {
                CronJobEditorSheet(
                    mode: .create,
                    onSave: { job in
                        scheduler.addJob(job)
                    }
                )
            } else if let job = selectedJob {
                CronJobEditorSheet(
                    mode: .edit(job),
                    onSave: { updatedJob in
                        scheduler.updateJob(updatedJob)
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
                scheduler.removeJob(job.id)
            }
            Button("Cancel", role: .cancel) {}
        } message: { job in
            Text("Are you sure you want to delete \"\(job.name)\"? This action cannot be undone.")
        }
    }

    // MARK: - Header

    private var headerView: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Cron Scheduler")
                        .font(.headline)
                    Text("Local scheduled automation tasks")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                Spacer()

                // Scheduler status
                schedulerStatusBadge

                Button {
                    isCreatingNew = true
                    selectedJob = nil
                    isEditorPresented = true
                } label: {
                    Label("New Job", systemImage: "plus")
                }
                .buttonStyle(.bordered)
            }

            // Search and controls
            HStack {
                HStack {
                    Image(systemName: "magnifyingglass")
                        .foregroundStyle(.secondary)
                    TextField("Search jobs...", text: $searchText)
                        .textFieldStyle(.plain)
                }
                .padding(8)
                .background(Color(NSColor.controlBackgroundColor))
                .cornerRadius(8)

                Spacer()

                // Start/Stop toggle
                Button {
                    if scheduler.isRunning {
                        scheduler.stop()
                    } else {
                        scheduler.start()
                    }
                } label: {
                    Label(
                        scheduler.isRunning ? "Stop" : "Start",
                        systemImage: scheduler.isRunning ? "stop.fill" : "play.fill"
                    )
                }
                .buttonStyle(.bordered)
                .tint(scheduler.isRunning ? .red : .green)
            }

            // Next run info
            if scheduler.isRunning, let nextRun = scheduler.nextScheduledRun {
                HStack(spacing: 4) {
                    Image(systemName: "clock")
                        .font(.caption)
                    Text("Next run:")
                        .font(.caption)
                    Text(nextRun, style: .relative)
                        .font(.caption.weight(.medium))
                }
                .foregroundStyle(.secondary)
            }
        }
        .padding()
    }

    private var schedulerStatusBadge: some View {
        HStack(spacing: 6) {
            Circle()
                .fill(scheduler.isRunning ? Color.green : Color.gray)
                .frame(width: 8, height: 8)

            Text(scheduler.isRunning ? "Running" : "Stopped")
                .font(.caption.weight(.medium))
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
        .background(
            Capsule()
                .fill(scheduler.isRunning ? Color.green.opacity(0.15) : Color.gray.opacity(0.15))
        )
        .foregroundStyle(scheduler.isRunning ? .green : .gray)
    }

    // MARK: - Empty State

    private var emptyStateView: some View {
        ContentUnavailableView {
            Label("No Scheduled Jobs", systemImage: "clock.badge.questionmark")
        } description: {
            Text("Create a cron job to automate recurring tasks locally.")
        } actions: {
            Button("Create Job") {
                isCreatingNew = true
                selectedJob = nil
                isEditorPresented = true
            }
            .buttonStyle(.borderedProminent)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    // MARK: - Jobs List

    private var jobsListView: some View {
        List {
            ForEach(filteredJobs) { job in
                CronJobRowView(
                    job: job,
                    isSelected: selectedJob?.id == job.id,
                    onSelect: {
                        selectedJob = job
                    },
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
                        scheduler.enableJob(job.id, enabled: enabled)
                    },
                    onRunNow: {
                        Task {
                            await scheduler.runJob(job.id)
                        }
                    }
                )
            }
        }
        .listStyle(.inset)
    }
}

// MARK: - Cron Job Row View

struct CronJobRowView: View {
    let job: CronJob
    let isSelected: Bool
    let onSelect: () -> Void
    let onEdit: () -> Void
    let onDelete: () -> Void
    let onToggleEnabled: (Bool) -> Void
    let onRunNow: () -> Void

    @State private var isHovered = false
    @State private var isRunning = false

    var body: some View {
        HStack(spacing: 12) {
            // Status indicator
            Circle()
                .fill(statusColor)
                .frame(width: 10, height: 10)

            // Job info
            VStack(alignment: .leading, spacing: 4) {
                Text(job.name)
                    .font(.body.weight(.medium))

                HStack(spacing: 8) {
                    Label(job.schedule.humanReadable, systemImage: "clock")
                        .font(.caption)
                        .foregroundStyle(.secondary)

                    if job.runCount > 0 {
                        Text("\(job.runCount) runs")
                            .font(.caption)
                            .foregroundStyle(.tertiary)
                    }
                }

                HStack(spacing: 16) {
                    if let lastRun = job.lastRunAt {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("Last Run")
                                .font(.caption2)
                                .foregroundStyle(.tertiary)
                            Text(lastRun, style: .relative)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }

                    if job.enabled, let nextRun = job.schedule.nextRun() {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("Next Run")
                                .font(.caption2)
                                .foregroundStyle(.tertiary)
                            Text(nextRun, style: .relative)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                }

                // Last result indicator
                if let lastResult = job.lastResult {
                    HStack(spacing: 4) {
                        Image(systemName: lastResult.success ? "checkmark.circle.fill" : "xmark.circle.fill")
                            .foregroundStyle(lastResult.success ? .green : .red)
                        Text(lastResult.success ? "Success" : "Failed")
                            .font(.caption)
                            .foregroundStyle(lastResult.success ? .green : .red)
                        Text("(\(String(format: "%.1fs", lastResult.duration)))")
                            .font(.caption)
                            .foregroundStyle(.tertiary)
                    }
                }
            }

            Spacer()

            // Actions
            HStack(spacing: 8) {
                if isHovered || isSelected {
                    Button {
                        isRunning = true
                        Task {
                            await CronScheduler.shared.runJob(job.id)
                            isRunning = false
                        }
                    } label: {
                        if isRunning {
                            ProgressView()
                                .controlSize(.small)
                        } else {
                            Image(systemName: "play.fill")
                        }
                    }
                    .buttonStyle(.borderless)
                    .help("Run now")
                    .disabled(isRunning)

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
        .onTapGesture {
            onSelect()
        }
        .onTapGesture(count: 2) {
            onEdit()
        }
    }

    private var statusColor: Color {
        if !job.enabled {
            return .gray
        }
        if let lastResult = job.lastResult, !lastResult.success {
            return .orange
        }
        return .green
    }
}

// MARK: - Cron Job Editor Sheet

struct CronJobEditorSheet: View {
    enum Mode {
        case create
        case edit(CronJob)

        var isEditing: Bool {
            if case .edit = self { return true }
            return false
        }

        var existingJob: CronJob? {
            if case .edit(let job) = self { return job }
            return nil
        }
    }

    let mode: Mode
    let onSave: (CronJob) -> Void

    @Environment(\.dismiss) private var dismiss

    // Form state
    @State private var name = ""
    @State private var command = ""
    @State private var schedule = CronSchedule.daily
    @State private var enabled = true
    @State private var selectedPreset: CronPreset = .daily
    @State private var useCustomSchedule = false
    @State private var isHistoryPresented = false
    @State private var scheduler = CronScheduler.shared

    private let logger = Logger(subsystem: "com.nexus.mac", category: "cron-editor-sheet")

    private var isValid: Bool {
        !name.trimmingCharacters(in: .whitespaces).isEmpty &&
        !command.trimmingCharacters(in: .whitespaces).isEmpty
    }

    var body: some View {
        VStack(spacing: 0) {
            // Header
            headerView
            Divider()

            // Form
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    // Basic info
                    basicInfoSection

                    Divider()

                    // Schedule
                    scheduleSection

                    Divider()

                    // Schedule preview
                    previewSection

                    Divider()

                    // Options
                    optionsSection
                }
                .padding()
            }

            Divider()

            // Footer
            footerView
        }
        .frame(width: 520, height: 650)
        .onAppear {
            loadExistingJob()
        }
        .sheet(isPresented: $isHistoryPresented) {
            if let job = mode.existingJob {
                CronJobHistorySheet(job: job, scheduler: scheduler)
            }
        }
    }

    // MARK: - Header

    private var headerView: some View {
        HStack {
            Text(mode.isEditing ? "Edit Job" : "New Cron Job")
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
    }

    // MARK: - Basic Info Section

    private var basicInfoSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Job Details")
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.secondary)

            VStack(alignment: .leading, spacing: 8) {
                Text("Name")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                TextField("Daily Backup", text: $name)
                    .textFieldStyle(.roundedBorder)
            }

            VStack(alignment: .leading, spacing: 8) {
                Text("Command / Prompt")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                TextEditor(text: $command)
                    .font(.system(.body, design: .monospaced))
                    .frame(minHeight: 80, maxHeight: 120)
                    .padding(4)
                    .background(Color(NSColor.textBackgroundColor))
                    .cornerRadius(6)
                    .overlay(
                        RoundedRectangle(cornerRadius: 6)
                            .stroke(Color.secondary.opacity(0.3), lineWidth: 1)
                    )
                Text("This command will be sent to the active chat session when the job runs.")
                    .font(.caption)
                    .foregroundStyle(.tertiary)
            }
        }
    }

    // MARK: - Schedule Section

    private var scheduleSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Schedule")
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.secondary)

            // Preset picker
            CronSchedulePicker(
                schedule: $schedule,
                selectedPreset: $selectedPreset,
                useCustomSchedule: $useCustomSchedule
            )
        }
    }

    // MARK: - Preview Section

    private var previewSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Next Run Times")
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.secondary)

            let nextTimes = schedule.nextRunTimes(count: 5)

            if nextTimes.isEmpty {
                Text("Unable to calculate next run times")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            } else {
                VStack(alignment: .leading, spacing: 6) {
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
                .padding()
                .background(Color(NSColor.controlBackgroundColor))
                .cornerRadius(8)
            }
        }
    }

    // MARK: - Options Section

    private var optionsSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Options")
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.secondary)

            Toggle("Enabled", isOn: $enabled)
        }
    }

    // MARK: - Footer

    private var footerView: some View {
        HStack {
            if mode.isEditing {
                Button("View History") {
                    isHistoryPresented = true
                }
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

    // MARK: - Actions

    private func loadExistingJob() {
        guard let job = mode.existingJob else { return }

        name = job.name
        command = job.command
        schedule = job.schedule
        enabled = job.enabled

        // Try to match a preset
        for preset in CronPreset.allCases {
            if preset.schedule == job.schedule {
                selectedPreset = preset
                useCustomSchedule = false
                return
            }
        }

        // No preset matched, use custom
        useCustomSchedule = true
    }

    private func saveJob() {
        var job: CronJob

        if let existing = mode.existingJob {
            job = existing
            job.name = name.trimmingCharacters(in: .whitespacesAndNewlines)
            job.command = command.trimmingCharacters(in: .whitespacesAndNewlines)
            job.schedule = schedule
            job.enabled = enabled
        } else {
            job = CronJob(
                name: name.trimmingCharacters(in: .whitespacesAndNewlines),
                schedule: schedule,
                command: command.trimmingCharacters(in: .whitespacesAndNewlines)
            )
            job.enabled = enabled
        }

        logger.info("Saving cron job: \(job.name)")
        onSave(job)
        dismiss()
    }
}

// MARK: - Cron Job History Sheet

struct CronJobHistorySheet: View {
    let job: CronJob
    @Bindable var scheduler: CronScheduler

    @Environment(\.dismiss) private var dismiss
    @State private var isClearConfirmPresented = false

    private var history: [CronJobResult] {
        scheduler.getHistory(for: job.id)
    }

    var body: some View {
        VStack(spacing: 0) {
            headerView
            Divider()
            historyView
            Divider()
            footerView
        }
        .frame(width: 520, height: 520)
        .confirmationDialog("Clear history for \(job.name)?", isPresented: $isClearConfirmPresented) {
            Button("Clear History", role: .destructive) {
                scheduler.clearHistory(for: job.id)
            }
            Button("Cancel", role: .cancel) {}
        }
    }

    private var headerView: some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                Text("Run History")
                    .font(.headline)
                Text(job.name)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }

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
    }

    private var historyView: some View {
        Group {
            if history.isEmpty {
                ContentUnavailableView {
                    Label("No Run History", systemImage: "clock.badge.questionmark")
                } description: {
                    Text("This job has not run yet.")
                }
            } else {
                List {
                    ForEach(Array(history.enumerated()), id: \.offset) { _, result in
                        historyRow(result)
                    }
                }
                .listStyle(.inset)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private func historyRow(_ result: CronJobResult) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                HStack(spacing: 6) {
                    Circle()
                        .fill(result.success ? Color.green : Color.red)
                        .frame(width: 8, height: 8)
                    Text(result.success ? "Success" : "Failed")
                        .font(.subheadline.weight(.semibold))
                }

                Spacer()

                Text(result.timestamp, format: .dateTime.year().month().day().hour().minute())
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            HStack {
                Text("Duration: \(formatDuration(result.duration))")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
            }

            if let output = result.output, !output.isEmpty {
                Text(output)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(3)
            }

            if let error = result.error, !error.isEmpty {
                Text(error)
                    .font(.caption)
                    .foregroundStyle(.red)
                    .lineLimit(3)
            }
        }
        .padding(.vertical, 4)
    }

    private var footerView: some View {
        HStack {
            Button("Clear History", role: .destructive) {
                isClearConfirmPresented = true
            }

            Spacer()

            Button("Close") {
                dismiss()
            }
            .keyboardShortcut(.cancelAction)
        }
        .padding()
    }

    private func formatDuration(_ duration: TimeInterval) -> String {
        if duration < 1 {
            return String(format: "%.0f ms", duration * 1000)
        }
        if duration < 60 {
            return String(format: "%.2f s", duration)
        }
        let minutes = Int(duration / 60)
        let seconds = Int(duration.truncatingRemainder(dividingBy: 60))
        return "\(minutes)m \(seconds)s"
    }
}

// MARK: - Cron Schedule Picker

/// Schedule picker with preset options and custom cron expression support.
struct CronSchedulePicker: View {
    @Binding var schedule: CronSchedule
    @Binding var selectedPreset: CronPreset
    @Binding var useCustomSchedule: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            // Preset buttons
            presetButtonsView

            // Custom toggle
            Toggle("Use custom schedule", isOn: $useCustomSchedule)
                .toggleStyle(.switch)
                .controlSize(.small)

            if useCustomSchedule {
                customScheduleFields
            }

            // Human readable display
            HStack {
                Image(systemName: "clock")
                    .foregroundStyle(.secondary)
                Text(schedule.humanReadable)
                    .font(.subheadline.weight(.medium))
            }
            .padding(.vertical, 8)
            .padding(.horizontal, 12)
            .background(Color.accentColor.opacity(0.1))
            .cornerRadius(8)
        }
        .onChange(of: selectedPreset) { _, newPreset in
            if !useCustomSchedule {
                schedule = newPreset.schedule
            }
        }
    }

    // MARK: - Preset Buttons

    private var presetButtonsView: some View {
        LazyVGrid(columns: [
            GridItem(.flexible()),
            GridItem(.flexible()),
            GridItem(.flexible()),
            GridItem(.flexible())
        ], spacing: 8) {
            ForEach(CronPreset.allCases) { preset in
                Button {
                    selectedPreset = preset
                    useCustomSchedule = false
                    schedule = preset.schedule
                } label: {
                    Text(preset.title)
                        .font(.caption)
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.bordered)
                .tint(selectedPreset == preset && !useCustomSchedule ? .accentColor : .secondary)
            }
        }
    }

    // MARK: - Custom Schedule Fields

    private var customScheduleFields: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Cron Expression")
                .font(.caption)
                .foregroundStyle(.secondary)

            Grid(alignment: .leading, horizontalSpacing: 12, verticalSpacing: 8) {
                GridRow {
                    fieldLabel("Minute", range: "0-59")
                    TextField("*", text: $schedule.minute)
                        .textFieldStyle(.roundedBorder)
                        .frame(width: 80)
                        .font(.system(.body, design: .monospaced))
                }
                GridRow {
                    fieldLabel("Hour", range: "0-23")
                    TextField("*", text: $schedule.hour)
                        .textFieldStyle(.roundedBorder)
                        .frame(width: 80)
                        .font(.system(.body, design: .monospaced))
                }
                GridRow {
                    fieldLabel("Day", range: "1-31")
                    TextField("*", text: $schedule.dayOfMonth)
                        .textFieldStyle(.roundedBorder)
                        .frame(width: 80)
                        .font(.system(.body, design: .monospaced))
                }
                GridRow {
                    fieldLabel("Month", range: "1-12")
                    TextField("*", text: $schedule.month)
                        .textFieldStyle(.roundedBorder)
                        .frame(width: 80)
                        .font(.system(.body, design: .monospaced))
                }
                GridRow {
                    fieldLabel("Weekday", range: "0-6")
                    TextField("*", text: $schedule.dayOfWeek)
                        .textFieldStyle(.roundedBorder)
                        .frame(width: 80)
                        .font(.system(.body, design: .monospaced))
                }
            }

            // Syntax reference
            DisclosureGroup("Cron Syntax Reference") {
                VStack(alignment: .leading, spacing: 6) {
                    syntaxRow("*", "Any value")
                    syntaxRow("*/N", "Every N units")
                    syntaxRow("1,2,3", "Specific values")
                    syntaxRow("1-5", "Range of values")
                    Divider()
                    syntaxRow("0 9 * * 1-5", "9am on weekdays")
                    syntaxRow("0 0 1 * *", "Midnight on the 1st")
                    syntaxRow("*/15 * * * *", "Every 15 minutes")
                }
                .font(.caption)
                .padding(.top, 8)
            }
            .font(.caption)
            .foregroundStyle(.secondary)
        }
        .padding()
        .background(Color(NSColor.controlBackgroundColor))
        .cornerRadius(8)
    }

    private func fieldLabel(_ name: String, range: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(name)
                .font(.caption.weight(.medium))
            Text(range)
                .font(.caption2)
                .foregroundStyle(.tertiary)
        }
        .frame(width: 80, alignment: .leading)
    }

    private func syntaxRow(_ syntax: String, _ description: String) -> some View {
        HStack {
            Text(syntax)
                .font(.system(.caption, design: .monospaced))
                .frame(width: 100, alignment: .leading)
            Text(description)
                .foregroundStyle(.secondary)
        }
    }
}

// MARK: - Preview

#Preview("Scheduler View") {
    CronSchedulerView()
        .frame(width: 600, height: 500)
}

#Preview("Editor - Create") {
    CronJobEditorSheet(mode: .create) { job in
        print("Created: \(job.name)")
    }
}

#Preview("Editor - Edit") {
    let sampleJob = CronJob(
        name: "Daily Backup",
        schedule: .daily,
        command: "Please run the daily backup workflow and summarize the results"
    )

    CronJobEditorSheet(mode: .edit(sampleJob)) { job in
        print("Updated: \(job.name)")
    }
}

#Preview("Schedule Picker") {
    struct PreviewWrapper: View {
        @State private var schedule = CronSchedule.daily
        @State private var preset = CronPreset.daily
        @State private var useCustom = false

        var body: some View {
            CronSchedulePicker(
                schedule: $schedule,
                selectedPreset: $preset,
                useCustomSchedule: $useCustom
            )
            .padding()
            .frame(width: 500)
        }
    }

    return PreviewWrapper()
}
