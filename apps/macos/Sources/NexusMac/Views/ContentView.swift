import SwiftUI

enum SidebarItem: String, CaseIterable, Hashable, Identifiable {
    case overview = "Overview"
    case agents = "Agents"
    case edge = "Edge Service"
    case nodes = "Nodes"
    case computerUse = "Computer Use"
    case sessions = "Sessions"
    case providers = "Providers"
    case skills = "Skills"
    case cron = "Cron"
    case artifacts = "Artifacts"
    case config = "Config"
    case logs = "Logs"
    case settings = "Settings"

    var systemImage: String {
        switch self {
        case .overview: return "gauge"
        case .agents: return "cpu"
        case .edge: return "bolt.horizontal"
        case .nodes: return "desktopcomputer"
        case .computerUse: return "cursorarrow"
        case .sessions: return "bubble.left.and.bubble.right"
        case .providers: return "antenna.radiowaves.left.and.right"
        case .skills: return "wand.and.stars"
        case .cron: return "clock.arrow.circlepath"
        case .artifacts: return "photo.on.rectangle"
        case .config: return "slider.horizontal.3"
        case .logs: return "doc.text.magnifyingglass"
        case .settings: return "gearshape"
        }
    }

    var id: String { rawValue }
}

struct ContentView: View {
    @EnvironmentObject var model: AppModel
    @State private var selection: SidebarItem? = .overview

    var body: some View {
        NavigationSplitView {
            List(SidebarItem.allCases, selection: $selection) { item in
                Label(item.rawValue, systemImage: item.systemImage)
            }
            .listStyle(.sidebar)
            .frame(minWidth: 180)
        } detail: {
            switch selection ?? .overview {
            case .overview:
                OverviewView()
            case .agents:
                AgentWorkspaceView()
            case .edge:
                EdgeServiceView()
            case .nodes:
                NodesView()
            case .computerUse:
                ComputerUseView()
            case .sessions:
                SessionsView()
            case .providers:
                ProvidersView()
            case .skills:
                SkillsView()
            case .cron:
                CronView()
            case .artifacts:
                ArtifactsView()
            case .config:
                ConfigView()
            case .logs:
                LogsView()
            case .settings:
                SettingsView()
            }
        }
        .toolbar {
            ToolbarItemGroup(placement: .automatic) {
                // WebSocket connection status
                HStack(spacing: 6) {
                    Circle()
                        .fill(model.isWebSocketConnected ? Color.green : Color.red)
                        .frame(width: 8, height: 8)
                    Text(model.isWebSocketConnected ? "Live" : "Offline")
                        .font(.caption)
                        .foregroundColor(model.isWebSocketConnected ? .green : .red)
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .background(model.isWebSocketConnected ? Color.green.opacity(0.1) : Color.red.opacity(0.1))
                .cornerRadius(8)
                .help(model.isWebSocketConnected ? "Real-time updates active" : "Real-time updates disconnected")

                // Reconnect button when disconnected
                if !model.isWebSocketConnected {
                    Button {
                        model.reconnectWebSocket()
                    } label: {
                        Label("Reconnect", systemImage: "bolt.horizontal")
                    }
                    .help("Reconnect to server")
                }

                Button {
                    Task { await model.refreshAll() }
                } label: {
                    Label("Refresh", systemImage: "arrow.clockwise")
                }
            }
        }
    }
}
