import SwiftUI

/// View for displaying and managing permissions
struct PermissionGuardView: View {
    @State private var guard_ = PermissionGuard.shared
    @State private var isLoading = false
    @State private var showingAlert = false
    @State private var alertPermission: GuardedPermission?

    var body: some View {
        Form {
            Section {
                HStack {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Permission Status")
                            .font(.headline)
                        Text(statusSummary)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }

                    Spacer()

                    if let lastChecked = guard_.lastCheckedAt {
                        Text("Updated \(lastChecked, style: .relative) ago")
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                }
                .padding(.vertical, 4)
            }

            Section("Required Permissions") {
                ForEach(GuardedPermission.allCases, id: \.self) { permission in
                    PermissionRow(
                        permission: permission,
                        status: guard_.permissionStates[permission] ?? .notDetermined,
                        onGrant: { await handleGrant(permission) },
                        onOpenSettings: { openSettings(for: permission) }
                    )
                }
            }

            Section {
                HStack {
                    Button("Refresh All") {
                        Task {
                            isLoading = true
                            await guard_.checkAll()
                            isLoading = false
                        }
                    }
                    .disabled(isLoading)

                    if isLoading {
                        ProgressView()
                            .controlSize(.small)
                            .padding(.leading, 8)
                    }

                    Spacer()

                    Button("Request All") {
                        Task {
                            await requestAllUndetermined()
                        }
                    }
                    .disabled(guard_.undeterminedPermissions.isEmpty)
                }
            }

            if !guard_.deniedPermissions.isEmpty {
                Section {
                    VStack(alignment: .leading, spacing: 8) {
                        Label("Some permissions are denied", systemImage: "exclamationmark.triangle.fill")
                            .foregroundStyle(.orange)

                        Text("Open System Settings to grant the following permissions:")
                            .font(.caption)
                            .foregroundStyle(.secondary)

                        ForEach(guard_.deniedPermissions, id: \.self) { permission in
                            HStack {
                                Text("- \(permission.displayName)")
                                    .font(.caption)
                                Spacer()
                                Button("Open") {
                                    openSettings(for: permission)
                                }
                                .buttonStyle(.bordered)
                                .controlSize(.mini)
                            }
                        }
                    }
                    .padding(.vertical, 4)
                }
            }
        }
        .formStyle(.grouped)
        .task {
            await guard_.checkAll()
        }
        .alert("Permission Required", isPresented: $showingAlert, presenting: alertPermission) { permission in
            Button("Open Settings") {
                openSettings(for: permission)
            }
            Button("Cancel", role: .cancel) {}
        } message: { permission in
            Text(permission.permissionDescription)
        }
    }

    // MARK: - Computed

    private var statusSummary: String {
        let granted = guard_.permissionStates.values.filter { $0 == .granted }.count
        let total = GuardedPermission.allCases.count

        if granted == total {
            return "All permissions granted"
        } else if granted == 0 {
            return "No permissions granted"
        } else {
            return "\(granted) of \(total) permissions granted"
        }
    }

    // MARK: - Actions

    private func handleGrant(_ permission: GuardedPermission) async {
        let granted = await guard_.request(permission)
        if !granted {
            alertPermission = permission
            showingAlert = true
        }
    }

    private func openSettings(for permission: GuardedPermission) {
        guard_.openSystemPreferences(for: permission)
    }

    private func requestAllUndetermined() async {
        for permission in guard_.undeterminedPermissions {
            _ = await guard_.request(permission)
        }
    }
}

// MARK: - Permission Row

struct PermissionRow: View {
    let permission: GuardedPermission
    let status: PermissionCheckResult
    let onGrant: () async -> Void
    let onOpenSettings: () -> Void

    @State private var isRequesting = false

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: iconName)
                .font(.title2)
                .foregroundStyle(iconColor)
                .frame(width: 32, height: 32)

            VStack(alignment: .leading, spacing: 2) {
                Text(permission.displayName)
                    .font(.body)

                Text(statusText)
                    .font(.caption)
                    .foregroundStyle(statusColor)
            }

            Spacer()

            actionButton
        }
        .padding(.vertical, 4)
    }

    // MARK: - Subviews

    @ViewBuilder
    private var actionButton: some View {
        switch status {
        case .granted:
            Image(systemName: "checkmark.circle.fill")
                .font(.title2)
                .foregroundStyle(.green)

        case .denied, .restricted:
            Button("Settings") {
                onOpenSettings()
            }
            .buttonStyle(.bordered)
            .controlSize(.small)

        case .notDetermined:
            if isRequesting {
                ProgressView()
                    .controlSize(.small)
            } else {
                Button("Grant") {
                    Task {
                        isRequesting = true
                        await onGrant()
                        isRequesting = false
                    }
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.small)
            }
        }
    }

    // MARK: - Computed Properties

    private var iconName: String {
        switch permission {
        case .screenRecording:
            return "rectangle.dashed.badge.record"
        case .camera:
            return "camera.fill"
        case .microphone:
            return "mic.fill"
        case .accessibility:
            return "accessibility"
        case .location:
            return "location.fill"
        case .contacts:
            return "person.crop.circle.fill"
        case .calendar:
            return "calendar"
        case .photos:
            return "photo.fill"
        }
    }

    private var iconColor: Color {
        switch status {
        case .granted:
            return .green
        case .denied, .restricted:
            return .red
        case .notDetermined:
            return .orange
        }
    }

    private var statusText: String {
        switch status {
        case .granted:
            return "Permission granted"
        case .denied:
            return "Permission denied - Open System Settings to grant"
        case .notDetermined:
            return "Not yet requested"
        case .restricted:
            return "Restricted by system policy"
        }
    }

    private var statusColor: Color {
        switch status {
        case .granted:
            return .secondary
        case .denied, .restricted:
            return .red
        case .notDetermined:
            return .orange
        }
    }
}

// MARK: - Permission Badge

/// A compact badge showing permission status
struct PermissionBadge: View {
    let permission: GuardedPermission
    let status: PermissionCheckResult

    var body: some View {
        HStack(spacing: 4) {
            Circle()
                .fill(statusColor)
                .frame(width: 8, height: 8)

            Text(permission.displayName)
                .font(.caption)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(statusColor.opacity(0.1))
        .clipShape(Capsule())
    }

    private var statusColor: Color {
        switch status {
        case .granted: return .green
        case .denied, .restricted: return .red
        case .notDetermined: return .orange
        }
    }
}

// MARK: - Permission Summary

/// A summary view showing overall permission status
struct PermissionSummaryView: View {
    @State private var guard_ = PermissionGuard.shared

    var body: some View {
        HStack(spacing: 8) {
            if guard_.allPermissionsGranted {
                Label("All permissions granted", systemImage: "checkmark.shield.fill")
                    .foregroundStyle(.green)
            } else {
                Label("\(guard_.deniedPermissions.count) permissions needed", systemImage: "exclamationmark.shield.fill")
                    .foregroundStyle(.orange)

                Button("Review") {
                    // Open permissions settings
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
            }
        }
        .font(.caption)
        .task {
            await guard_.checkAll()
        }
    }
}

// MARK: - Guarded Content

/// A view that shows content only when a permission is granted
struct GuardedContent<Content: View, Fallback: View>: View {
    let permission: GuardedPermission
    let content: () -> Content
    let fallback: () -> Fallback

    @State private var guard_ = PermissionGuard.shared
    @State private var hasChecked = false

    init(
        requiring permission: GuardedPermission,
        @ViewBuilder content: @escaping () -> Content,
        @ViewBuilder fallback: @escaping () -> Fallback
    ) {
        self.permission = permission
        self.content = content
        self.fallback = fallback
    }

    var body: some View {
        Group {
            if guard_.permissionStates[permission] == .granted {
                content()
            } else {
                fallback()
            }
        }
        .task {
            guard !hasChecked else { return }
            _ = await guard_.check(permission)
            hasChecked = true
        }
    }
}

extension GuardedContent where Fallback == PermissionRequiredView {
    init(
        requiring permission: GuardedPermission,
        @ViewBuilder content: @escaping () -> Content
    ) {
        self.permission = permission
        self.content = content
        self.fallback = { PermissionRequiredView(permission: permission) }
    }
}

// MARK: - Permission Required View

/// A fallback view shown when a permission is not granted
struct PermissionRequiredView: View {
    let permission: GuardedPermission

    @State private var guard_ = PermissionGuard.shared
    @State private var isRequesting = false

    var body: some View {
        VStack(spacing: 16) {
            Image(systemName: iconName)
                .font(.system(size: 48))
                .foregroundStyle(.secondary)

            Text("\(permission.displayName) Required")
                .font(.headline)

            Text(permission.permissionDescription)
                .font(.caption)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)

            HStack(spacing: 12) {
                if guard_.permissionStates[permission] == .notDetermined {
                    Button {
                        Task {
                            isRequesting = true
                            _ = await guard_.request(permission)
                            isRequesting = false
                        }
                    } label: {
                        if isRequesting {
                            ProgressView()
                                .controlSize(.small)
                        } else {
                            Text("Grant Permission")
                        }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(isRequesting)
                }

                Button("Open Settings") {
                    guard_.openSystemPreferences(for: permission)
                }
                .buttonStyle(.bordered)
            }
        }
        .padding()
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private var iconName: String {
        switch permission {
        case .screenRecording: return "rectangle.dashed.badge.record"
        case .camera: return "camera.fill"
        case .microphone: return "mic.fill"
        case .accessibility: return "accessibility"
        case .location: return "location.fill"
        case .contacts: return "person.crop.circle.fill"
        case .calendar: return "calendar"
        case .photos: return "photo.fill"
        }
    }
}

// MARK: - Previews

#Preview("Permission Guard View") {
    PermissionGuardView()
        .frame(width: 450, height: 600)
}

#Preview("Permission Row - Granted") {
    PermissionRow(
        permission: .camera,
        status: .granted,
        onGrant: {},
        onOpenSettings: {}
    )
    .padding()
}

#Preview("Permission Row - Denied") {
    PermissionRow(
        permission: .screenRecording,
        status: .denied,
        onGrant: {},
        onOpenSettings: {}
    )
    .padding()
}

#Preview("Permission Row - Not Determined") {
    PermissionRow(
        permission: .microphone,
        status: .notDetermined,
        onGrant: {},
        onOpenSettings: {}
    )
    .padding()
}

#Preview("Permission Required") {
    PermissionRequiredView(permission: .accessibility)
        .frame(width: 300, height: 300)
}

#Preview("Guarded Content") {
    GuardedContent(requiring: .camera) {
        Text("Camera access granted!")
    }
    .frame(width: 300, height: 200)
}
