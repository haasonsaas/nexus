import AppKit
import OSLog
import SwiftUI

/// Controller for managing the onboarding window.
@MainActor
final class OnboardingController {
    static let shared = OnboardingController()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "onboarding")
    private var window: NSWindow?

    private init() {}

    /// Show the onboarding window
    func show() {
        if let window {
            window.makeKeyAndOrderFront(nil)
            NSApp.activate(ignoringOtherApps: true)
            return
        }

        let hosting = NSHostingController(rootView: OnboardingView())
        let window = NSWindow(contentViewController: hosting)
        window.title = "Welcome to Nexus"
        window.setContentSize(NSSize(width: 600, height: 500))
        window.styleMask = [.titled, .closable, .fullSizeContentView]
        window.titlebarAppearsTransparent = true
        window.titleVisibility = .hidden
        window.isMovableByWindowBackground = true
        window.center()
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        self.window = window

        logger.info("onboarding window shown")
    }

    /// Close the onboarding window
    func close() {
        window?.close()
        window = nil
        logger.info("onboarding window closed")
    }

    /// Show onboarding if not completed
    func showIfNeeded() {
        guard !AppStateStore.shared.hasCompletedOnboarding else { return }
        show()
    }

    /// Mark onboarding as complete and close
    func complete() {
        AppStateStore.shared.hasCompletedOnboarding = true
        close()
        logger.info("onboarding completed")
    }

    /// Reset onboarding state (for testing)
    func reset() {
        AppStateStore.shared.hasCompletedOnboarding = false
        logger.info("onboarding reset")
    }
}
