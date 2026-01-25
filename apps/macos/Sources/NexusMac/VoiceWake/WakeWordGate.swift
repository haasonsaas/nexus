import Foundation
import Speech

/// Segment from speech recognition with timing info.
public struct WakeWordSegment: Sendable, Equatable {
    public let text: String
    public let start: TimeInterval
    public let duration: TimeInterval
    public let range: Range<String.Index>?
    public var end: TimeInterval { start + duration }

    public init(text: String, start: TimeInterval, duration: TimeInterval, range: Range<String.Index>? = nil) {
        self.text = text; self.start = start; self.duration = duration; self.range = range
    }
}

/// Configuration for wake word matching.
public struct WakeWordGateConfig: Sendable, Equatable {
    public var triggers: [String]
    public var minPostTriggerGap: TimeInterval
    public var minCommandLength: Int

    public init(triggers: [String], minPostTriggerGap: TimeInterval = 0.45, minCommandLength: Int = 1) {
        self.triggers = triggers; self.minPostTriggerGap = minPostTriggerGap; self.minCommandLength = minCommandLength
    }
}

/// Result of a successful wake word match.
public struct WakeWordGateMatch: Sendable, Equatable {
    public let triggerEndTime: TimeInterval
    public let postGap: TimeInterval
    public let command: String

    public init(triggerEndTime: TimeInterval, postGap: TimeInterval, command: String) {
        self.triggerEndTime = triggerEndTime; self.postGap = postGap; self.command = command
    }
}

/// Wake word detection with timing-based gap analysis.
public enum WakeWordGate {
    private static let punct = CharacterSet.whitespacesAndNewlines.union(.punctuationCharacters)

    /// Match wake word using segment timing for gap detection.
    public static func match(transcript: String, segments: [WakeWordSegment], config: WakeWordGateConfig) -> WakeWordGateMatch? {
        let triggerTokens = config.triggers.compactMap { t -> [String]? in
            let tokens = t.split(whereSeparator: \.isWhitespace).map { $0.lowercased().trimmingCharacters(in: punct) }.filter { !$0.isEmpty }
            return tokens.isEmpty ? nil : tokens
        }
        guard !triggerTokens.isEmpty else { return nil }

        let tokens = segments.compactMap { seg -> (n: String, s: TimeInterval, e: TimeInterval)? in
            let n = seg.text.lowercased().trimmingCharacters(in: punct)
            return n.isEmpty ? nil : (n, seg.start, seg.end)
        }
        guard !tokens.isEmpty else { return nil }

        var best: (idx: Int, end: TimeInterval, gap: TimeInterval)?
        for trigger in triggerTokens {
            guard trigger.count > 0, tokens.count > trigger.count else { continue }
            for i in 0...(tokens.count - trigger.count - 1) {
                guard (0..<trigger.count).allSatisfy({ tokens[i + $0].n == trigger[$0] }) else { continue }
                let triggerEnd = tokens[i + trigger.count - 1].e
                let gap = tokens[i + trigger.count].s - triggerEnd
                guard gap >= config.minPostTriggerGap, best.map({ i > $0.idx }) ?? true else { continue }
                best = (i, triggerEnd, gap)
            }
        }
        guard let best else { return nil }

        let cmd = commandText(transcript: transcript, segments: segments, triggerEndTime: best.end).trimmingCharacters(in: punct)
        return cmd.count >= config.minCommandLength ? WakeWordGateMatch(triggerEndTime: best.end, postGap: best.gap, command: cmd) : nil
    }

    /// Extract command text after the trigger end time.
    public static func commandText(transcript: String, segments: [WakeWordSegment], triggerEndTime: TimeInterval) -> String {
        let threshold = triggerEndTime + 0.001
        for seg in segments where seg.start >= threshold {
            let n = seg.text.lowercased().trimmingCharacters(in: punct)
            if n.isEmpty { continue }
            if let r = seg.range { return String(transcript[r.lowerBound...]).trimmingCharacters(in: punct) }
            break
        }
        return segments.filter { $0.start >= threshold && !$0.text.trimmingCharacters(in: punct).isEmpty }
            .map(\.text).joined(separator: " ").trimmingCharacters(in: punct)
    }

    /// Check if text contains any trigger word.
    public static func matchesTextOnly(text: String, triggers: [String]) -> Bool {
        let n = text.lowercased()
        return triggers.contains { !$0.trimmingCharacters(in: punct).isEmpty && n.contains($0.lowercased().trimmingCharacters(in: punct)) }
    }

    /// Check if transcript starts with a trigger.
    public static func startsWithTrigger(transcript: String, triggers: [String]) -> Bool {
        let n = transcript.lowercased().trimmingCharacters(in: punct)
        return triggers.contains { t in let tk = t.lowercased().trimmingCharacters(in: punct); return !tk.isEmpty && n.hasPrefix(tk) }
    }

    /// Extract command using text-only matching.
    public static func textOnlyCommand(transcript: String, triggers: [String], minLength: Int) -> String? {
        let cmd = trimmedAfterTrigger(transcript, triggers: triggers)
        return cmd.count >= minLength ? cmd : nil
    }

    /// Strip trigger and return remaining text.
    public static func trimmedAfterTrigger(_ text: String, triggers: [String]) -> String {
        let lower = text.lowercased()
        for t in triggers {
            let tk = t.lowercased().trimmingCharacters(in: .whitespacesAndNewlines)
            if !tk.isEmpty, let r = lower.range(of: tk) { return String(text[r.upperBound...]).trimmingCharacters(in: .whitespacesAndNewlines) }
        }
        return text
    }

    /// Extract command after trigger, preferring timing-based extraction.
    public static func commandAfterTrigger(transcript: String, segments: [WakeWordSegment], triggerEndTime: TimeInterval?, triggers: [String]) -> String {
        guard let triggerEndTime else { return trimmedAfterTrigger(transcript, triggers: triggers) }
        let trimmed = commandText(transcript: transcript, segments: segments, triggerEndTime: triggerEndTime)
        return trimmed.isEmpty ? trimmedAfterTrigger(transcript, triggers: triggers) : trimmed
    }

    /// Convert SFTranscription to WakeWordSegments.
    public static func segments(from transcription: SFTranscription, transcript: String) -> [WakeWordSegment] {
        transcription.segments.map { WakeWordSegment(text: $0.substring, start: $0.timestamp, duration: $0.duration, range: Range($0.substringRange, in: transcript)) }
    }
}
