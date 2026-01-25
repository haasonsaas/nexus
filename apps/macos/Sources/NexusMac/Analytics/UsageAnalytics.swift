import Foundation
import OSLog

/// Tracks usage analytics and patterns for improving user experience.
/// All data is stored locally and never sent without explicit consent.
@MainActor
@Observable
final class UsageAnalytics {
    static let shared = UsageAnalytics()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "analytics")

    private(set) var dailyStats: DailyStats = DailyStats()
    private(set) var featureUsage: [String: FeatureUsage] = [:]
    private(set) var modelUsage: [String: ModelUsageStats] = [:]

    struct DailyStats: Codable {
        var date: Date = Date()
        var sessionCount: Int = 0
        var messageCount: Int = 0
        var tokenCount: Int = 0
        var toolCallCount: Int = 0
        var voiceInputCount: Int = 0
        var computerUseActionCount: Int = 0
        var activeMinutes: Int = 0
    }

    struct FeatureUsage: Codable {
        let featureId: String
        var firstUsed: Date
        var lastUsed: Date
        var totalUses: Int
        var averageDuration: TimeInterval?
    }

    struct ModelUsageStats: Codable {
        let modelId: String
        let providerId: String
        var requestCount: Int
        var totalInputTokens: Int
        var totalOutputTokens: Int
        var averageLatency: TimeInterval
        var errorCount: Int
    }

    // MARK: - Event Tracking

    /// Track a session start
    func trackSessionStart() {
        ensureCurrentDay()
        dailyStats.sessionCount += 1
        trackFeatureUsage("session")
        logger.debug("analytics: session started")
    }

    /// Track a message sent
    func trackMessage(inputTokens: Int, outputTokens: Int) {
        ensureCurrentDay()
        dailyStats.messageCount += 1
        dailyStats.tokenCount += inputTokens + outputTokens
        logger.debug("analytics: message tracked tokens=\(inputTokens + outputTokens)")
    }

    /// Track a tool call
    func trackToolCall(tool: String) {
        ensureCurrentDay()
        dailyStats.toolCallCount += 1
        trackFeatureUsage("tool:\(tool)")
    }

    /// Track voice input usage
    func trackVoiceInput() {
        ensureCurrentDay()
        dailyStats.voiceInputCount += 1
        trackFeatureUsage("voice_input")
    }

    /// Track computer use action
    func trackComputerUseAction(action: String) {
        ensureCurrentDay()
        dailyStats.computerUseActionCount += 1
        trackFeatureUsage("computer_use:\(action)")
    }

    /// Track model usage
    func trackModelUsage(
        modelId: String,
        providerId: String,
        inputTokens: Int,
        outputTokens: Int,
        latency: TimeInterval,
        success: Bool
    ) {
        let key = "\(providerId)/\(modelId)"

        if var existing = modelUsage[key] {
            existing.requestCount += 1
            existing.totalInputTokens += inputTokens
            existing.totalOutputTokens += outputTokens
            existing.averageLatency = (existing.averageLatency * Double(existing.requestCount - 1) + latency) / Double(existing.requestCount)
            if !success {
                existing.errorCount += 1
            }
            modelUsage[key] = existing
        } else {
            modelUsage[key] = ModelUsageStats(
                modelId: modelId,
                providerId: providerId,
                requestCount: 1,
                totalInputTokens: inputTokens,
                totalOutputTokens: outputTokens,
                averageLatency: latency,
                errorCount: success ? 0 : 1
            )
        }
    }

    /// Track feature usage
    func trackFeatureUsage(_ featureId: String, duration: TimeInterval? = nil) {
        let now = Date()

        if var existing = featureUsage[featureId] {
            existing.lastUsed = now
            existing.totalUses += 1
            if let duration {
                let totalDuration = (existing.averageDuration ?? 0) * Double(existing.totalUses - 1) + duration
                existing.averageDuration = totalDuration / Double(existing.totalUses)
            }
            featureUsage[featureId] = existing
        } else {
            featureUsage[featureId] = FeatureUsage(
                featureId: featureId,
                firstUsed: now,
                lastUsed: now,
                totalUses: 1,
                averageDuration: duration
            )
        }
    }

    /// Track active time
    func trackActiveMinutes(_ minutes: Int) {
        ensureCurrentDay()
        dailyStats.activeMinutes += minutes
    }

    // MARK: - Aggregated Stats

    /// Get total tokens used today
    func todayTokens() -> Int {
        ensureCurrentDay()
        return dailyStats.tokenCount
    }

    /// Get most used features
    func topFeatures(limit: Int = 10) -> [FeatureUsage] {
        featureUsage.values
            .sorted { $0.totalUses > $1.totalUses }
            .prefix(limit)
            .map { $0 }
    }

    /// Get model with lowest latency
    func fastestModel() -> ModelUsageStats? {
        modelUsage.values.min { $0.averageLatency < $1.averageLatency }
    }

    /// Get model with best success rate
    func mostReliableModel() -> ModelUsageStats? {
        modelUsage.values
            .filter { $0.requestCount >= 10 } // Minimum sample size
            .max { model1, model2 in
                let rate1 = Double(model1.requestCount - model1.errorCount) / Double(model1.requestCount)
                let rate2 = Double(model2.requestCount - model2.errorCount) / Double(model2.requestCount)
                return rate1 < rate2
            }
    }

    // MARK: - Export

    /// Export analytics as JSON
    func exportJSON() throws -> Data {
        let export = AnalyticsExport(
            dailyStats: dailyStats,
            featureUsage: Array(featureUsage.values),
            modelUsage: Array(modelUsage.values),
            exportedAt: Date()
        )
        return try JSONEncoder().encode(export)
    }

    struct AnalyticsExport: Codable {
        let dailyStats: DailyStats
        let featureUsage: [FeatureUsage]
        let modelUsage: [ModelUsageStats]
        let exportedAt: Date
    }

    // MARK: - Private

    private func ensureCurrentDay() {
        let calendar = Calendar.current
        if !calendar.isDateInToday(dailyStats.date) {
            // Save yesterday's stats
            saveDailyStats()
            // Reset for new day
            dailyStats = DailyStats()
        }
    }

    // MARK: - Persistence

    private func saveDailyStats() {
        let url = dailyStatsURL()
        let historyURL = statsHistoryURL()

        // Append to history
        var history: [DailyStats] = []
        if let data = try? Data(contentsOf: historyURL),
           let existing = try? JSONDecoder().decode([DailyStats].self, from: data) {
            history = existing
        }
        history.append(dailyStats)

        // Keep last 90 days
        if history.count > 90 {
            history = Array(history.suffix(90))
        }

        do {
            let data = try JSONEncoder().encode(history)
            try data.write(to: historyURL)
        } catch {
            logger.error("failed to save stats history: \(error.localizedDescription)")
        }
    }

    func loadAnalytics() {
        // Load feature usage
        let featureURL = featureUsageURL()
        if let data = try? Data(contentsOf: featureURL),
           let usage = try? JSONDecoder().decode([String: FeatureUsage].self, from: data) {
            featureUsage = usage
        }

        // Load model usage
        let modelURL = modelUsageURL()
        if let data = try? Data(contentsOf: modelURL),
           let usage = try? JSONDecoder().decode([String: ModelUsageStats].self, from: data) {
            modelUsage = usage
        }

        // Load today's stats
        let dailyURL = dailyStatsURL()
        if let data = try? Data(contentsOf: dailyURL),
           let stats = try? JSONDecoder().decode(DailyStats.self, from: data) {
            dailyStats = stats
        }

        logger.debug("analytics loaded")
    }

    func saveAnalytics() {
        do {
            let featureData = try JSONEncoder().encode(featureUsage)
            try featureData.write(to: featureUsageURL())

            let modelData = try JSONEncoder().encode(modelUsage)
            try modelData.write(to: modelUsageURL())

            let dailyData = try JSONEncoder().encode(dailyStats)
            try dailyData.write(to: dailyStatsURL())
        } catch {
            logger.error("failed to save analytics: \(error.localizedDescription)")
        }
    }

    private func analyticsDir() -> URL {
        let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        let dir = appSupport.appendingPathComponent("Nexus/Analytics")
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir
    }

    private func dailyStatsURL() -> URL { analyticsDir().appendingPathComponent("daily.json") }
    private func featureUsageURL() -> URL { analyticsDir().appendingPathComponent("features.json") }
    private func modelUsageURL() -> URL { analyticsDir().appendingPathComponent("models.json") }
    private func statsHistoryURL() -> URL { analyticsDir().appendingPathComponent("history.json") }
}
