import SwiftUI

@main
struct NexusMacApp: App {
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup("Nexus", id: "main") {
            ContentView()
                .environmentObject(model)
        }
        .defaultSize(width: 980, height: 640)

        MenuBarExtra {
            MenuBarContentView()
                .environmentObject(model)
        } label: {
            Label("Nexus", systemImage: "bolt.horizontal")
        }
        .menuBarExtraStyle(.menu)
    }
}
