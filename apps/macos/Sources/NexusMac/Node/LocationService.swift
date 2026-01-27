import CoreLocation
import Foundation
import OSLog

@MainActor
final class LocationService: NSObject, CLLocationManagerDelegate {
    static let shared = LocationService()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "location")
    private let manager = CLLocationManager()
    private var continuation: CheckedContinuation<CLLocation, Error>?
    private var timeoutTask: Task<Void, Never>?

    private override init() {
        super.init()
        manager.delegate = self
        manager.desiredAccuracy = kCLLocationAccuracyHundredMeters
    }

    func requestLocation(timeout: Duration = .seconds(10)) async throws -> CLLocation {
        guard continuation == nil else {
            throw LocationError.requestInProgress
        }

        let status = manager.authorizationStatus
        guard status == .authorizedAlways || status == .authorized else {
            throw LocationError.notAuthorized
        }

        return try await withCheckedThrowingContinuation { continuation in
            self.continuation = continuation
            manager.requestLocation()
            timeoutTask?.cancel()
            timeoutTask = Task { [weak self] in
                try? await Task.sleep(for: timeout)
                self?.finish(.failure(LocationError.timeout))
            }
        }
    }

    private func finish(_ result: Result<CLLocation, Error>) {
        timeoutTask?.cancel()
        timeoutTask = nil
        guard let continuation else { return }
        self.continuation = nil

        switch result {
        case .success(let location):
            continuation.resume(returning: location)
        case .failure(let error):
            continuation.resume(throwing: error)
        }
    }

    nonisolated func locationManager(_ manager: CLLocationManager, didUpdateLocations locations: [CLLocation]) {
        Task { @MainActor in
            guard let location = locations.last else {
                finish(.failure(LocationError.unavailable))
                return
            }
            logger.debug("location updated accuracy=\(location.horizontalAccuracy)")
            finish(.success(location))
        }
    }

    nonisolated func locationManager(_ manager: CLLocationManager, didFailWithError error: Error) {
        Task { @MainActor in
            logger.error("location request failed: \(error.localizedDescription)")
            finish(.failure(error))
        }
    }
}

enum LocationError: LocalizedError {
    case notAuthorized
    case timeout
    case unavailable
    case requestInProgress

    var errorDescription: String? {
        switch self {
        case .notAuthorized:
            return "Location permission not granted"
        case .timeout:
            return "Location request timed out"
        case .unavailable:
            return "Location unavailable"
        case .requestInProgress:
            return "Location request already in progress"
        }
    }
}
