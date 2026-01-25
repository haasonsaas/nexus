import Foundation

/// Formats a date as a human-readable age string
func age(from date: Date) -> String {
    let seconds = Int(Date().timeIntervalSince(date))
    if seconds < 0 { return "just now" }
    if seconds < 60 { return "\(seconds)s ago" }
    if seconds < 3600 { return "\(seconds / 60)m ago" }
    if seconds < 86400 { return "\(seconds / 3600)h ago" }
    return "\(seconds / 86400)d ago"
}

/// Formats milliseconds as a human-readable age string
func msToAge(_ ms: Int) -> String {
    let seconds = ms / 1000
    if seconds < 0 { return "just now" }
    if seconds < 60 { return "\(seconds)s" }
    if seconds < 3600 { return "\(seconds / 60)m" }
    if seconds < 86400 { return "\(seconds / 3600)h" }
    return "\(seconds / 86400)d"
}

/// Formats a TimeInterval as a readable duration
func formatDuration(_ interval: TimeInterval) -> String {
    let seconds = Int(interval)
    if seconds < 60 { return "\(seconds)s" }
    if seconds < 3600 { return "\(seconds / 60)m \(seconds % 60)s" }
    let hours = seconds / 3600
    let mins = (seconds % 3600) / 60
    return "\(hours)h \(mins)m"
}

/// Extension for optional Date handling
extension Optional where Wrapped == Date {
    func ageString(fallback: String = "unknown") -> String {
        guard let date = self else { return fallback }
        return age(from: date)
    }
}
