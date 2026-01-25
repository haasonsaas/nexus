import Foundation

@MainActor
final class UsageStore: ObservableObject {
    @Published private(set) var summary: GatewayUsageSummary?
    @Published private(set) var rows: [UsageRow] = []
    @Published private(set) var error: String?
    @Published private(set) var isLoading = false

    private var refreshTask: Task<Void, Never>?
    private let refreshInterval: TimeInterval = 60

    func start(api: NexusAPI) {
        stop()
        refreshTask = Task { [weak self] in
            while !Task.isCancelled {
                await self?.refresh(api: api)
                try? await Task.sleep(nanoseconds: UInt64(self?.refreshInterval ?? 60) * 1_000_000_000)
            }
        }
    }

    func stop() {
        refreshTask?.cancel()
        refreshTask = nil
    }

    func refresh(api: NexusAPI) async {
        isLoading = true
        defer { isLoading = false }

        do {
            let result = try await UsageLoader.loadSummary(api: api)
            summary = result
            rows = result.primaryRows()
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}
