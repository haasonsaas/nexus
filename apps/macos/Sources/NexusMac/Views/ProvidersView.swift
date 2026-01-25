import SwiftUI

struct ProvidersView: View {
    @EnvironmentObject var model: AppModel
    @State private var showQR = false
    @State private var qrProviderName: String = ""
    @State private var isRefreshing = false
    @State private var selectedProvider: ProviderStatus?

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            // Header
            headerView

            // Error banner
            if let error = model.lastError {
                ErrorBanner(message: error, severity: .error) {
                    model.lastError = nil
                }
                .transition(.move(edge: .top).combined(with: .opacity))
            }

            // Content
            Group {
                if isRefreshing && model.providers.isEmpty {
                    LoadingStateView(message: "Loading providers...", showSkeleton: true)
                } else if model.providers.isEmpty {
                    EmptyStateView(
                        icon: "antenna.radiowaves.left.and.right",
                        title: "No Providers",
                        description: "Configure LLM providers in the gateway to see them here.",
                        actionTitle: "Refresh"
                    ) {
                        refreshProviders()
                    }
                } else {
                    providersContent
                }
            }
            .animation(.easeInOut(duration: 0.2), value: isRefreshing)
            .animation(.easeInOut(duration: 0.2), value: model.providers.isEmpty)

            Spacer()
        }
        .padding()
        .sheet(isPresented: $showQR) {
            ImprovedProviderQRSheet(providerName: qrProviderName)
                .environmentObject(model)
        }
        .animation(.easeInOut(duration: 0.2), value: model.lastError)
    }

    // MARK: - Header

    private var headerView: some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                Text("Providers")
                    .font(.title2)
                Text("Manage LLM provider connections")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            // Status summary
            HStack(spacing: 12) {
                let connectedCount = model.providers.filter { $0.connected }.count
                let healthyCount = model.providers.filter { $0.healthy == true }.count

                Label("\(connectedCount) connected", systemImage: "link")
                    .font(.caption)
                    .foregroundStyle(connectedCount > 0 ? .green : .secondary)

                Label("\(healthyCount) healthy", systemImage: "heart.fill")
                    .font(.caption)
                    .foregroundStyle(healthyCount > 0 ? .green : .secondary)
            }

            Button {
                refreshProviders()
            } label: {
                Image(systemName: "arrow.clockwise")
            }
            .disabled(isRefreshing)
        }
    }

    // MARK: - Providers Content

    private var providersContent: some View {
        ScrollView {
            LazyVStack(spacing: 10) {
                ForEach(model.providers) { provider in
                    ProviderCard(
                        provider: provider,
                        isSelected: selectedProvider?.name == provider.name,
                        onSelect: {
                            withAnimation(.spring(response: 0.3)) {
                                selectedProvider = provider
                            }
                        },
                        onShowQR: {
                            qrProviderName = provider.name
                            Task {
                                await model.loadProviderQR(name: provider.name)
                                showQR = true
                            }
                        }
                    )
                }
            }
        }
    }

    // MARK: - Actions

    private func refreshProviders() {
        isRefreshing = true
        Task {
            await model.refreshProviders()
            isRefreshing = false
        }
    }
}

// MARK: - Provider Card

struct ProviderCard: View {
    let provider: ProviderStatus
    let isSelected: Bool
    let onSelect: () -> Void
    let onShowQR: () -> Void

    @State private var isHovered = false

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            // Header
            HStack(spacing: 10) {
                // Provider icon
                ZStack {
                    Circle()
                        .fill(statusColor.opacity(0.2))
                        .frame(width: 36, height: 36)

                    Image(systemName: providerIcon)
                        .font(.system(size: 16))
                        .foregroundStyle(statusColor)
                }

                VStack(alignment: .leading, spacing: 2) {
                    Text(provider.name)
                        .font(.subheadline.weight(.medium))

                    HStack(spacing: 8) {
                        StatusBadge(
                            status: provider.connected ? .online : .offline,
                            variant: .minimal
                        )

                        if let healthy = provider.healthy {
                            Label(
                                healthy ? "Healthy" : "Unhealthy",
                                systemImage: healthy ? "heart.fill" : "heart.slash"
                            )
                            .font(.caption2)
                            .foregroundStyle(healthy ? .green : .orange)
                        }
                    }
                }

                Spacer()

                // Actions
                if provider.qrAvailable == true {
                    Button {
                        onShowQR()
                    } label: {
                        Label("QR", systemImage: "qrcode")
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                }

                Toggle("", isOn: .constant(provider.enabled))
                    .toggleStyle(.switch)
                    .controlSize(.small)
                    .disabled(true)
            }

            // Status details
            HStack(spacing: 16) {
                Label(provider.enabled ? "Enabled" : "Disabled", systemImage: "power")
                    .font(.caption)
                    .foregroundStyle(provider.enabled ? .primary : .secondary)

                Label(provider.connected ? "Connected" : "Disconnected", systemImage: "link")
                    .font(.caption)
                    .foregroundStyle(provider.connected ? .green : .red)
            }
        }
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .fill(Color(NSColor.controlBackgroundColor))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .stroke(isSelected ? statusColor.opacity(0.5) : (isHovered ? Color.gray.opacity(0.3) : Color.gray.opacity(0.15)), lineWidth: isSelected ? 2 : 1)
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

    private var statusColor: Color {
        if !provider.enabled { return .gray }
        if provider.connected && (provider.healthy ?? false) { return .green }
        if provider.connected { return .orange }
        return .red
    }

    private var providerIcon: String {
        let name = provider.name.lowercased()
        if name.contains("anthropic") || name.contains("claude") { return "brain" }
        if name.contains("openai") || name.contains("gpt") { return "sparkles" }
        if name.contains("google") || name.contains("gemini") { return "g.circle" }
        if name.contains("ollama") { return "server.rack" }
        return "cpu"
    }
}

// MARK: - Improved QR Sheet

struct ImprovedProviderQRSheet: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let providerName: String

    var body: some View {
        VStack(spacing: 20) {
            // Header
            HStack {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Provider QR Code")
                        .font(.headline)
                    Text(providerName)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                Spacer()

                Button {
                    dismiss()
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.title2)
                        .foregroundStyle(.tertiary)
                }
                .buttonStyle(.plain)
            }

            Divider()

            // QR Code
            if let image = model.providerQRImages[providerName] {
                Image(nsImage: image)
                    .interpolation(.none)
                    .resizable()
                    .scaledToFit()
                    .frame(maxWidth: 280, maxHeight: 280)
                    .padding()
                    .background(
                        RoundedRectangle(cornerRadius: 12, style: .continuous)
                            .fill(Color.white)
                    )
                    .shadow(color: .black.opacity(0.1), radius: 8, y: 4)
            } else {
                VStack(spacing: 12) {
                    ProgressView()
                    Text("Loading QR Code...")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .frame(width: 280, height: 280)
            }

            Text("Scan with your phone to authenticate")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(24)
        .frame(width: 380, height: 460)
    }
}
