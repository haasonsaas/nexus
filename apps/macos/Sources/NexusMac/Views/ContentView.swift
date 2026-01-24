import SwiftUI

enum SidebarItem: String, CaseIterable, Hashable {
    case overview = "Overview"
    case edge = "Edge Service"
    case nodes = "Nodes"
    case artifacts = "Artifacts"
    case config = "Config"
    case logs = "Logs"
    case settings = "Settings"

    var systemImage: String {
        switch self {
        case .overview: return "gauge"
        case .edge: return "bolt.horizontal"
        case .nodes: return "desktopcomputer"
        case .artifacts: return "photo.on.rectangle"
        case .config: return "slider.horizontal.3"
        case .logs: return "doc.text.magnifyingglass"
        case .settings: return "gearshape"
        }
    }
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
            case .edge:
                EdgeServiceView()
            case .nodes:
                NodesView()
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
            ToolbarItemGroup {
                Button {
                    Task { await model.refreshAll() }
                } label: {
                    Label("Refresh", systemImage: "arrow.clockwise")
                }
            }
        }
    }
}
