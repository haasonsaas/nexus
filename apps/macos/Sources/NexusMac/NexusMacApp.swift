import SwiftUI

@main
struct NexusMacApp: App {
    @StateObject private var model = AppModel()

    init() {
        // Configure the HotkeyManager with the app model after initialization
        // This is done via a delayed task to ensure model is fully initialized
    }

    var body: some Scene {
        WindowGroup("Nexus", id: "main") {
            ContentView()
                .environmentObject(model)
                .onAppear {
                    // Configure HotkeyManager with the app model
                    HotkeyManager.shared.configure(with: model)
                }
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
