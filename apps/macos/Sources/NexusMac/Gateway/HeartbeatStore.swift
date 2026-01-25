import Foundation
import Observation
import SwiftUI

@MainActor
@Observable
final class HeartbeatStore {
    static let shared = HeartbeatStore()

    private(set) var lastEvent: ControlHeartbeatEvent?

    private var observer: NSObjectProtocol?

    private init() {
        self.observer = NotificationCenter.default.addObserver(
            forName: .controlHeartbeat,
            object: nil,
            queue: .main)
        { [weak self] note in
            guard let event = note.object as? ControlHeartbeatEvent else { return }
            Task { @MainActor in self?.lastEvent = event }
        }
    }

    @MainActor
    deinit {
        if let observer { NotificationCenter.default.removeObserver(observer) }
    }
}
