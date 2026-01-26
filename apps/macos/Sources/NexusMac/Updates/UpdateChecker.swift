import AppKit
import Foundation
import OSLog

/// Checks for application updates.
/// Supports GitHub releases and custom update servers.
@MainActor
@Observable
final class UpdateChecker {
    static let shared = UpdateChecker()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "updates")

    private(set) var currentVersion: String
    private(set) var latestVersion: String?
    private(set) var updateAvailable = false
    private(set) var releaseNotes: String?
    private(set) var downloadURL: URL?
    private(set) var isChecking = false
    private(set) var lastChecked: Date?

    var updateSource: UpdateSource = .github(owner: "haasonsaas", repo: "nexus")
    var checkAutomatically = true
    var checkInterval: TimeInterval = 86400 // 24 hours

    enum UpdateSource {
        case github(owner: String, repo: String)
        case custom(URL)
    }

    struct Release: Codable {
        let tagName: String
        let name: String
        let body: String?
        let htmlUrl: String
        let assets: [Asset]
        let prerelease: Bool
        let publishedAt: String

        enum CodingKeys: String, CodingKey {
            case tagName = "tag_name"
            case name
            case body
            case htmlUrl = "html_url"
            case assets
            case prerelease
            case publishedAt = "published_at"
        }

        struct Asset: Codable {
            let name: String
            let browserDownloadUrl: String
            let size: Int

            enum CodingKeys: String, CodingKey {
                case name
                case browserDownloadUrl = "browser_download_url"
                case size
            }
        }
    }

    init() {
        currentVersion = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "0.0.0"
    }

    // MARK: - Update Check

    /// Check for updates
    func checkForUpdates() async {
        isChecking = true
        defer {
            isChecking = false
            lastChecked = Date()
        }

        logger.info("checking for updates currentVersion=\(self.currentVersion)")

        do {
            let release = try await fetchLatestRelease()

            latestVersion = release.tagName.trimmingCharacters(in: CharacterSet(charactersIn: "v"))
            releaseNotes = release.body
            downloadURL = URL(string: release.htmlUrl)

            // Find macOS asset (prefer DMG over ZIP)
            let assets = release.assets
            let dmgAsset = assets.first { $0.name.lowercased().hasSuffix(".dmg") }
            let zipAsset = assets.first {
                let lower = $0.name.lowercased()
                return lower.hasSuffix(".zip") && lower.contains("mac")
            }
            let pkgAsset = assets.first {
                let lower = $0.name.lowercased()
                return lower.hasSuffix(".pkg") && lower.contains("mac")
            }
            let macAsset = assets.first { $0.name.lowercased().contains("mac") }

            if let asset = dmgAsset ?? zipAsset ?? pkgAsset ?? macAsset {
                downloadURL = URL(string: asset.browserDownloadUrl)
            }

            updateAvailable = isNewerVersion(latestVersion ?? "", than: currentVersion)

            if updateAvailable {
                logger.info("update available latestVersion=\(self.latestVersion ?? "unknown")")
            } else {
                logger.debug("no update available")
            }
        } catch {
            logger.error("update check failed: \(error.localizedDescription)")
        }
    }

    /// Check for updates in background if needed
    func checkIfNeeded() async {
        guard checkAutomatically else { return }

        if let lastChecked {
            let elapsed = Date().timeIntervalSince(lastChecked)
            if elapsed < checkInterval {
                return // Not time to check yet
            }
        }

        await checkForUpdates()
    }

    // MARK: - Update Actions

    /// Open download page
    func openDownloadPage() {
        if let url = downloadURL {
            NSWorkspace.shared.open(url)
        }
    }

    /// Show update notification
    func showUpdateNotification() async {
        guard updateAvailable, let version = latestVersion else { return }

        do {
            try await NotificationBridge.shared.send(
                title: "Update Available",
                body: "Nexus \(version) is available. Click to download.",
                category: "update"
            )
        } catch {
            logger.warning("failed to show update notification: \(error.localizedDescription)")
        }
    }

    // MARK: - Private

    private func fetchLatestRelease() async throws -> Release {
        let url: URL

        switch updateSource {
        case .github(let owner, let repo):
            url = URL(string: "https://api.github.com/repos/\(owner)/\(repo)/releases/latest")!
        case .custom(let customURL):
            url = customURL
        }

        var request = URLRequest(url: url)
        request.setValue("application/vnd.github.v3+json", forHTTPHeaderField: "Accept")
        request.setValue("Nexus/\(currentVersion)", forHTTPHeaderField: "User-Agent")

        let (data, response) = try await URLSession.shared.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse,
              httpResponse.statusCode == 200 else {
            throw UpdateError.fetchFailed
        }

        return try JSONDecoder().decode(Release.self, from: data)
    }

    private func isNewerVersion(_ version1: String, than version2: String) -> Bool {
        let v1 = parseVersion(version1)
        let v2 = parseVersion(version2)

        for i in 0..<max(v1.count, v2.count) {
            let c1 = i < v1.count ? v1[i] : 0
            let c2 = i < v2.count ? v2[i] : 0

            if c1 > c2 { return true }
            if c1 < c2 { return false }
        }

        return false
    }

    private func parseVersion(_ version: String) -> [Int] {
        version
            .split(separator: ".")
            .compactMap { Int($0.trimmingCharacters(in: .letters)) }
    }
}

enum UpdateError: LocalizedError {
    case fetchFailed
    case invalidResponse
    case noDownloadURL

    var errorDescription: String? {
        switch self {
        case .fetchFailed:
            return "Failed to check for updates"
        case .invalidResponse:
            return "Invalid update response"
        case .noDownloadURL:
            return "No download URL found"
        }
    }
}
