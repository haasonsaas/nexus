import SwiftUI

struct MenuBarContentView: View {
    @EnvironmentObject var model: AppModel
    @StateObject private var usageStore = UsageStore()
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Nexus Edge")
                .font(.headline)

            // Edge service status
            HStack {
                Text("Edge:")
                    .font(.caption)
                    .foregroundColor(.secondary)
                Text(model.edgeServiceStatus.rawValue)
                    .font(.caption)
                    .foregroundColor(edgeStatusColor)
            }

            // WebSocket connection status
            HStack {
                Text("Gateway:")
                    .font(.caption)
                    .foregroundColor(.secondary)
                HStack(spacing: 4) {
                    Circle()
                        .fill(model.isWebSocketConnected ? Color.green : Color.red)
                        .frame(width: 6, height: 6)
                    Text(model.isWebSocketConnected ? "Connected" : "Disconnected")
                        .font(.caption)
                        .foregroundColor(model.isWebSocketConnected ? .green : .red)
                }
            }

            // Active nodes count
            if !model.nodes.isEmpty {
                let onlineCount = model.nodes.filter { $0.status == "online" }.count
                HStack {
                    Text("Nodes:")
                        .font(.caption)
                        .foregroundColor(.secondary)
                    Text("\(onlineCount)/\(model.nodes.count) online")
                        .font(.caption)
                }
            }

            if let error = model.lastError {
                Text(error)
                    .font(.caption)
                    .foregroundColor(.red)
                    .lineLimit(2)
            }

            // Usage summary
            if model.isWebSocketConnected {
                Divider()
                UsageMenuView(store: usageStore)
            }

            Divider()

            Button("Open Nexus") {
                openWindow(id: "main")
            }
            Button("Refresh") {
                model.refreshEdgeServiceStatus()
                Task { await model.refreshAll() }
            }

            Divider()

            Button("Start Edge") { model.startService() }
                .disabled(model.edgeServiceStatus == .running)
            Button("Stop Edge") { model.stopService() }
                .disabled(model.edgeServiceStatus != .running)

            if !model.isWebSocketConnected {
                Button("Reconnect Gateway") { model.reconnectWebSocket() }
            }

            Divider()

            Button("Quit") { NSApplication.shared.terminate(nil) }
        }
        .padding(8)
        .frame(minWidth: 240)
        .onAppear {
            startUsageRefresh()
        }
        .onChange(of: model.isWebSocketConnected) { _, connected in
            if connected {
                startUsageRefresh()
            } else {
                usageStore.stop()
            }
        }
    }

    private func startUsageRefresh() {
        guard model.isWebSocketConnected,
              !model.baseURL.isEmpty,
              !model.apiKey.isEmpty else { return }
        let api = NexusAPI(baseURL: model.baseURL, apiKey: model.apiKey)
        usageStore.start(api: api)
    }

    private var edgeStatusColor: Color {
        switch model.edgeServiceStatus {
        case .running:
            return .green
        case .stopped:
            return .orange
        case .notInstalled:
            return .red
        case .unknown:
            return .gray
        }
    }
}
