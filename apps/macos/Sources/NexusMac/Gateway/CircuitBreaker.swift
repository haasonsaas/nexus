import Foundation
import OSLog

// MARK: - CircuitState

/// Circuit breaker states following the standard pattern
enum CircuitState: String, Sendable {
    /// Normal operation - requests allowed
    case closed

    /// Failing - requests rejected immediately
    case open

    /// Testing if service recovered - limited requests allowed
    case halfOpen
}

// MARK: - CircuitBreakerError

enum CircuitBreakerError: Error, LocalizedError {
    case circuitOpen
    case executionFailed(underlying: Error)

    var errorDescription: String? {
        switch self {
        case .circuitOpen:
            return "Circuit breaker is open - service unavailable"
        case .executionFailed(let underlying):
            return "Operation failed: \(underlying.localizedDescription)"
        }
    }
}

// MARK: - CircuitBreaker

/// Circuit breaker for gateway operations
///
/// Implements the circuit breaker pattern to prevent cascading failures:
/// - CLOSED: Normal operation, failures are counted
/// - OPEN: Too many failures, requests are rejected immediately
/// - HALF-OPEN: After timeout, limited requests are allowed to test recovery
///
/// Based on Clawdbot's retry patterns.
@MainActor
@Observable
final class CircuitBreaker {
    private let logger = Logger(subsystem: "com.nexus.mac", category: "circuit-breaker")

    // MARK: - Properties

    /// Name of this circuit breaker for logging
    let name: String

    // MARK: - State

    /// Current circuit state
    private(set) var state: CircuitState = .closed

    /// Number of consecutive failures
    private(set) var failureCount: Int = 0

    /// Number of consecutive successes in half-open state
    private(set) var successCount: Int = 0

    /// Timestamp of last failure
    private(set) var lastFailure: Date?

    /// Timestamp of last success
    private(set) var lastSuccess: Date?

    /// Total number of requests processed
    private(set) var totalRequests: Int = 0

    /// Total number of rejected requests (circuit open)
    private(set) var rejectedRequests: Int = 0

    // MARK: - Configuration

    /// Number of failures before opening the circuit
    let failureThreshold: Int

    /// Number of successes in half-open state before closing
    let successThreshold: Int

    /// Time to wait before transitioning from open to half-open
    let timeout: TimeInterval

    // MARK: - Initialization

    init(
        name: String,
        failureThreshold: Int = 5,
        successThreshold: Int = 2,
        timeout: TimeInterval = 30
    ) {
        self.name = name
        self.failureThreshold = failureThreshold
        self.successThreshold = successThreshold
        self.timeout = timeout
    }

    // MARK: - Execution

    /// Execute an operation through the circuit breaker
    ///
    /// - Parameter operation: The async operation to execute
    /// - Returns: The result of the operation
    /// - Throws: `CircuitBreakerError.circuitOpen` if the circuit is open,
    ///           or the underlying error if the operation fails
    func execute<T>(_ operation: () async throws -> T) async throws -> T {
        totalRequests += 1

        // Check if we should allow the request
        guard shouldAllowRequest() else {
            rejectedRequests += 1
            logger.warning("[\(self.name)] Request rejected - circuit open")
            throw CircuitBreakerError.circuitOpen
        }

        do {
            let result = try await operation()
            recordSuccess()
            return result
        } catch {
            recordFailure()
            throw error
        }
    }

    /// Execute an operation with a fallback if the circuit is open
    ///
    /// - Parameters:
    ///   - operation: The async operation to execute
    ///   - fallback: Fallback value to return if circuit is open
    /// - Returns: The result of the operation or the fallback
    func executeWithFallback<T>(
        _ operation: () async throws -> T,
        fallback: @autoclosure () -> T
    ) async -> T {
        do {
            return try await execute(operation)
        } catch is CircuitBreakerError {
            return fallback()
        } catch {
            return fallback()
        }
    }

    /// Check if a request should be allowed
    private func shouldAllowRequest() -> Bool {
        switch state {
        case .closed:
            return true

        case .open:
            // Check if timeout has passed
            if let lastFailure = lastFailure,
               Date().timeIntervalSince(lastFailure) > timeout {
                state = .halfOpen
                successCount = 0
                logger.info("[\(self.name)] Circuit half-open, testing recovery")
                return true
            }
            return false

        case .halfOpen:
            return true
        }
    }

    // MARK: - Recording

    /// Record a successful operation
    private func recordSuccess() {
        lastSuccess = Date()

        switch state {
        case .closed:
            // Reset failure count on success
            failureCount = 0

        case .halfOpen:
            successCount += 1
            logger.debug("[\(self.name)] Half-open success \(self.successCount)/\(self.successThreshold)")

            if successCount >= successThreshold {
                state = .closed
                failureCount = 0
                successCount = 0
                logger.info("[\(self.name)] Circuit closed - service recovered")
            }

        case .open:
            // Shouldn't happen, but handle gracefully
            break
        }
    }

    /// Record a failed operation
    private func recordFailure() {
        lastFailure = Date()
        failureCount += 1

        switch state {
        case .closed:
            logger.debug("[\(self.name)] Failure \(self.failureCount)/\(self.failureThreshold)")

            if failureCount >= failureThreshold {
                state = .open
                logger.warning("[\(self.name)] Circuit opened after \(self.failureCount) failures")
            }

        case .halfOpen:
            // Any failure in half-open immediately reopens
            state = .open
            successCount = 0
            logger.warning("[\(self.name)] Circuit reopened - failure in half-open state")

        case .open:
            // Already open, just update timestamp
            break
        }
    }

    // MARK: - Manual Control

    /// Reset the circuit breaker to closed state
    func reset() {
        state = .closed
        failureCount = 0
        successCount = 0
        lastFailure = nil
        logger.info("[\(self.name)] Circuit reset manually")
    }

    /// Manually trip the circuit to open state
    func trip() {
        state = .open
        lastFailure = Date()
        logger.warning("[\(self.name)] Circuit tripped manually")
    }

    /// Force the circuit to half-open state for testing
    func forceHalfOpen() {
        state = .halfOpen
        successCount = 0
        logger.info("[\(self.name)] Circuit forced to half-open")
    }

    // MARK: - Status

    /// Whether the circuit is allowing requests
    var isAllowingRequests: Bool {
        shouldAllowRequest()
    }

    /// Time remaining until circuit transitions from open to half-open
    var timeUntilHalfOpen: TimeInterval? {
        guard state == .open, let lastFailure = lastFailure else { return nil }
        let elapsed = Date().timeIntervalSince(lastFailure)
        let remaining = timeout - elapsed
        return remaining > 0 ? remaining : nil
    }

    /// Summary of circuit breaker status
    var statusSummary: String {
        switch state {
        case .closed:
            return "Closed (healthy)"
        case .open:
            if let remaining = timeUntilHalfOpen {
                return "Open (retry in \(Int(remaining))s)"
            }
            return "Open"
        case .halfOpen:
            return "Half-open (\(successCount)/\(successThreshold) successes)"
        }
    }
}

// MARK: - CircuitBreakerRegistry

/// Registry for managing multiple circuit breakers
@MainActor
final class CircuitBreakerRegistry {
    static let shared = CircuitBreakerRegistry()

    private var breakers: [String: CircuitBreaker] = [:]

    private init() {}

    /// Get or create a circuit breaker with the given name
    ///
    /// - Parameters:
    ///   - name: Unique name for the circuit breaker
    ///   - failureThreshold: Number of failures before opening (default: 5)
    ///   - successThreshold: Number of successes to close (default: 2)
    ///   - timeout: Seconds before half-open (default: 30)
    /// - Returns: The circuit breaker instance
    func breaker(
        for name: String,
        failureThreshold: Int = 5,
        successThreshold: Int = 2,
        timeout: TimeInterval = 30
    ) -> CircuitBreaker {
        if let existing = breakers[name] {
            return existing
        }

        let breaker = CircuitBreaker(
            name: name,
            failureThreshold: failureThreshold,
            successThreshold: successThreshold,
            timeout: timeout
        )
        breakers[name] = breaker
        return breaker
    }

    /// Get an existing circuit breaker, or nil if it doesn't exist
    func existingBreaker(for name: String) -> CircuitBreaker? {
        breakers[name]
    }

    /// Reset all circuit breakers
    func resetAll() {
        for breaker in breakers.values {
            breaker.reset()
        }
    }

    /// Get all registered circuit breakers
    var allBreakers: [CircuitBreaker] {
        Array(breakers.values)
    }

    /// Get status summary for all breakers
    var statusSummary: [String: String] {
        breakers.mapValues { $0.statusSummary }
    }
}

// MARK: - Predefined Circuit Breakers

extension CircuitBreakerRegistry {
    /// Circuit breaker for gateway health checks
    var healthCheck: CircuitBreaker {
        breaker(for: "health_check", failureThreshold: 3, successThreshold: 1, timeout: 15)
    }

    /// Circuit breaker for gateway requests
    var gatewayRequest: CircuitBreaker {
        breaker(for: "gateway_request", failureThreshold: 5, successThreshold: 2, timeout: 30)
    }

    /// Circuit breaker for control channel operations
    var controlChannel: CircuitBreaker {
        breaker(for: "control_channel", failureThreshold: 5, successThreshold: 2, timeout: 30)
    }
}
