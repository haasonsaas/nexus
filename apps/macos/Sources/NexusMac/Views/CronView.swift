import SwiftUI

struct CronView: View {
    @EnvironmentObject var model: AppModel
    @State private var isRefreshing = false
    @State private var selectedJob: CronJob?
    @State private var showCreateSheet = false

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            // Header
            headerView

            // Content
            Group {
                if isRefreshing && model.cronJobs.isEmpty {
                    LoadingStateView(message: "Loading cron jobs...", showSkeleton: true)
                } else if model.cronJobs.isEmpty {
                    EmptyStateView(
                        icon: "clock.arrow.circlepath",
                        title: "No Scheduled Jobs",
                        description: "Create cron jobs to automate recurring tasks.",
                        actionTitle: "Refresh"
                    ) {
                        refreshCron()
                    }
                } else {
                    cronJobsList
                }
            }
            .animation(.easeInOut(duration: 0.2), value: isRefreshing)
            .animation(.easeInOut(duration: 0.2), value: model.cronJobs.isEmpty)

            // Error banner
            if let error = model.lastError {
                ErrorBanner(message: error, severity: .error) {
                    model.lastError = nil
                }
                .transition(.move(edge: .top).combined(with: .opacity))
            }

            Spacer()
        }
        .padding()
        .animation(.easeInOut(duration: 0.2), value: model.lastError)
    }

    // MARK: - Header

    private var headerView: some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                Text("Scheduled Jobs")
                    .font(.title2)
                Text("Manage automated recurring tasks")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            // Global status badge
            CronStatusBadge(isEnabled: model.cronEnabled)

            Button {
                refreshCron()
            } label: {
                Image(systemName: "arrow.clockwise")
            }
            .disabled(isRefreshing)
        }
    }

    // MARK: - Cron Jobs List

    private var cronJobsList: some View {
        ScrollView {
            LazyVStack(spacing: 10) {
                ForEach(model.cronJobs) { job in
                    CronJobCard(
                        job: job,
                        isSelected: selectedJob?.id == job.id,
                        onSelect: {
                            withAnimation(.spring(response: 0.3)) {
                                selectedJob = job
                            }
                        },
                        onToggle: { enabled in
                            // Toggle job enabled state
                        }
                    )
                }
            }
        }
    }

    // MARK: - Actions

    private func refreshCron() {
        isRefreshing = true
        Task {
            await model.refreshCron()
            isRefreshing = false
        }
    }
}

// MARK: - Cron Status Badge

struct CronStatusBadge: View {
    let isEnabled: Bool

    var body: some View {
        HStack(spacing: 6) {
            Circle()
                .fill(isEnabled ? Color.green : Color.red)
                .frame(width: 8, height: 8)

            Text(isEnabled ? "Running" : "Paused")
                .font(.caption.weight(.medium))
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
        .background(
            Capsule()
                .fill(isEnabled ? Color.green.opacity(0.15) : Color.red.opacity(0.15))
        )
        .foregroundStyle(isEnabled ? .green : .red)
    }
}

// MARK: - Cron Job Card

struct CronJobCard: View {
    let job: CronJob
    let isSelected: Bool
    let onSelect: () -> Void
    let onToggle: (Bool) -> Void

    @State private var isHovered = false

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            // Header
            HStack(spacing: 10) {
                // Job icon
                ZStack {
                    Circle()
                        .fill(jobColor.opacity(0.2))
                        .frame(width: 36, height: 36)

                    Image(systemName: jobIcon)
                        .font(.system(size: 16))
                        .foregroundStyle(jobColor)
                }

                VStack(alignment: .leading, spacing: 2) {
                    Text(job.name)
                        .font(.subheadline.weight(.medium))

                    HStack(spacing: 8) {
                        Label(job.type, systemImage: "tag")
                            .font(.caption2)
                            .foregroundStyle(.secondary)

                        StatusBadge(
                            status: job.enabled ? .online : .offline,
                            variant: .minimal
                        )
                    }
                }

                Spacer()

                Toggle("", isOn: Binding(
                    get: { job.enabled },
                    set: { onToggle($0) }
                ))
                .toggleStyle(.switch)
                .controlSize(.small)
            }

            // Schedule info
            HStack(spacing: 16) {
                Label(job.schedule, systemImage: "clock")
                    .font(.caption)
                    .foregroundStyle(.primary)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(
                        RoundedRectangle(cornerRadius: 6, style: .continuous)
                            .fill(Color(NSColor.controlBackgroundColor))
                    )
            }

            // Timing details
            HStack(spacing: 16) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Next Run")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                    Text(job.nextRun, style: .relative)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                VStack(alignment: .leading, spacing: 2) {
                    Text("Last Run")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                    Text(job.lastRun, style: .relative)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                Spacer()
            }
        }
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .fill(Color(NSColor.controlBackgroundColor))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .stroke(isSelected ? jobColor.opacity(0.5) : (isHovered ? Color.gray.opacity(0.3) : Color.gray.opacity(0.15)), lineWidth: isSelected ? 2 : 1)
        )
        .scaleEffect(isHovered ? 1.01 : 1.0)
        .animation(.spring(response: 0.3, dampingFraction: 0.8), value: isHovered)
        .onHover { hovering in
            isHovered = hovering
        }
        .onTapGesture {
            onSelect()
        }
    }

    private var jobColor: Color {
        if !job.enabled { return .gray }
        return .blue
    }

    private var jobIcon: String {
        switch job.type.lowercased() {
        case "email", "mail": return "envelope.fill"
        case "sync": return "arrow.triangle.2.circlepath"
        case "backup": return "externaldrive.fill"
        case "report": return "doc.text.fill"
        case "cleanup": return "trash.fill"
        default: return "clock.arrow.circlepath"
        }
    }
}
