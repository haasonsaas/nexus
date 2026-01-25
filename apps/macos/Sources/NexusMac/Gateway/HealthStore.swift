import Foundation
import Observation
import SwiftUI

// MARK: - HealthState

enum HealthState: Equatable, Sendable {
    case ok
    case degraded(String)
    case linkingNeeded
    case unknown
}

// MARK: - HealthStore

@MainActor
@Observable
final class HealthStore {
    static let shared = HealthStore()

    private(set) var state: HealthState = .unknown
    private(set) var isRefreshing: Bool = false
    private(set) var lastSuccess: Date?
    private(set) var degradedSummary: String?

    private var periodicTask: Task<Void, Never>?
    private let refreshInterval: TimeInterval = 30

    private init() {}

    // MARK: - Lifecycle

    func start() {
        guard periodicTask == nil else { return }
        periodicTask = Task { [weak self] in
            while !Task.isCancelled {
                await self?.refresh()
                try? await Task.sleep(nanoseconds: UInt64((self?.refreshInterval ?? 30) * 1_000_000_000))
            }
        }
    }

    func stop() {
        periodicTask?.cancel()
        periodicTask = nil
    }

    // MARK: - Refresh

    func refresh() async {
        guard !isRefreshing else { return }
        isRefreshing = true
        defer { isRefreshing = false }

        do {
            let data = try await ControlChannel.shared.health(timeout: 15)
            let response = try? JSONDecoder().decode(HealthResponse.self, from: data)
            applyHealthResponse(response)
        } catch {
            applyError(error)
        }
    }

    // MARK: - State Updates

    private func applyHealthResponse(_ response: HealthResponse?) {
        guard let response else {
            state = .degraded("Invalid health response")
            degradedSummary = "Invalid health response"
            return
        }

        if response.linkingNeeded == true {
            state = .linkingNeeded
            degradedSummary = "Linking required"
            return
        }

        if response.ok == true {
            state = .ok
            lastSuccess = Date()
            degradedSummary = nil
        } else {
            let message = response.message ?? "Health check failed"
            state = .degraded(message)
            degradedSummary = message
        }
    }

    private func applyError(_ error: Error) {
        let message: String
        if let ctrl = error as? ControlChannelError, let desc = ctrl.errorDescription {
            message = desc
        } else {
            message = error.localizedDescription
        }
        state = .degraded(message)
        degradedSummary = message
    }

    func markLinkingNeeded() {
        state = .linkingNeeded
        degradedSummary = "Linking required"
    }

    func markUnknown() {
        state = .unknown
        degradedSummary = nil
    }
}

// MARK: - HealthResponse

private struct HealthResponse: Decodable {
    let ok: Bool?
    let message: String?
    let linkingNeeded: Bool?
}

// MARK: - UI Extension

extension HealthState {
    var tint: Color {
        switch self {
        case .ok:
            return .green
        case .degraded:
            return .orange
        case .linkingNeeded:
            return .red
        case .unknown:
            return .gray
        }
    }
}
