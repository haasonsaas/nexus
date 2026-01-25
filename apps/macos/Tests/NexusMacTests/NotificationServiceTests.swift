import XCTest
@testable import NexusMac
import Foundation

final class NotificationServiceTests: XCTestCase {

    // MARK: - NotificationCategory Enum Tests

    func testNotificationCategoryRawValues() {
        XCTAssertEqual(NotificationCategory.statusChange.rawValue, "statusChange")
        XCTAssertEqual(NotificationCategory.toolComplete.rawValue, "toolComplete")
        XCTAssertEqual(NotificationCategory.error.rawValue, "error")
        XCTAssertEqual(NotificationCategory.edgeStatus.rawValue, "edgeStatus")
    }

    func testNotificationCategoryAllCases() {
        let allCases = NotificationCategory.allCases

        XCTAssertEqual(allCases.count, 4)
        XCTAssertTrue(allCases.contains(.statusChange))
        XCTAssertTrue(allCases.contains(.toolComplete))
        XCTAssertTrue(allCases.contains(.error))
        XCTAssertTrue(allCases.contains(.edgeStatus))
    }

    func testNotificationCategoryDisplayNames() {
        XCTAssertEqual(NotificationCategory.statusChange.displayName, "Gateway Status Changes")
        XCTAssertEqual(NotificationCategory.toolComplete.displayName, "Tool Execution Complete")
        XCTAssertEqual(NotificationCategory.error.displayName, "Errors")
        XCTAssertEqual(NotificationCategory.edgeStatus.displayName, "Edge Service Status")
    }

    func testNotificationCategoryDescriptions() {
        XCTAssertEqual(
            NotificationCategory.statusChange.description,
            "Notify when gateway connects or disconnects"
        )
        XCTAssertEqual(
            NotificationCategory.toolComplete.description,
            "Notify when tool invocations finish"
        )
        XCTAssertEqual(
            NotificationCategory.error.description,
            "Notify when errors occur during operations"
        )
        XCTAssertEqual(
            NotificationCategory.edgeStatus.description,
            "Notify when edge service starts or stops"
        )
    }

    func testNotificationCategoryUserDefaultsKeys() {
        XCTAssertEqual(
            NotificationCategory.statusChange.userDefaultsKey,
            "notification_statusChange_enabled"
        )
        XCTAssertEqual(
            NotificationCategory.toolComplete.userDefaultsKey,
            "notification_toolComplete_enabled"
        )
        XCTAssertEqual(
            NotificationCategory.error.userDefaultsKey,
            "notification_error_enabled"
        )
        XCTAssertEqual(
            NotificationCategory.edgeStatus.userDefaultsKey,
            "notification_edgeStatus_enabled"
        )
    }

    func testAllUserDefaultsKeysAreUnique() {
        let keys = NotificationCategory.allCases.map { $0.userDefaultsKey }
        let uniqueKeys = Set(keys)

        XCTAssertEqual(keys.count, uniqueKeys.count, "All UserDefaults keys should be unique")
    }

    func testAllRawValuesAreUnique() {
        let rawValues = NotificationCategory.allCases.map { $0.rawValue }
        let uniqueRawValues = Set(rawValues)

        XCTAssertEqual(rawValues.count, uniqueRawValues.count, "All raw values should be unique")
    }

    // MARK: - Preference Storage Tests

    func testPreferenceStorageForCategory() {
        let testKey = "test_notification_preference_\(UUID().uuidString)"

        // Clean up any existing value
        UserDefaults.standard.removeObject(forKey: testKey)

        // Verify default behavior (nil returns true in isEnabled)
        XCTAssertNil(UserDefaults.standard.object(forKey: testKey))

        // Set to false
        UserDefaults.standard.set(false, forKey: testKey)
        XCTAssertFalse(UserDefaults.standard.bool(forKey: testKey))

        // Set to true
        UserDefaults.standard.set(true, forKey: testKey)
        XCTAssertTrue(UserDefaults.standard.bool(forKey: testKey))

        // Clean up
        UserDefaults.standard.removeObject(forKey: testKey)
    }

    func testPreferenceDefaultsToTrueWhenNeverSet() {
        // Create a unique key that has never been set
        let uniqueKey = "notification_never_set_\(UUID().uuidString)_enabled"

        // Verify the key doesn't exist
        XCTAssertNil(UserDefaults.standard.object(forKey: uniqueKey))

        // The logic in NotificationService.isEnabled returns true if never set
        // We test this logic directly
        let neverSet = UserDefaults.standard.object(forKey: uniqueKey) == nil
        let defaultValue = neverSet ? true : UserDefaults.standard.bool(forKey: uniqueKey)

        XCTAssertTrue(defaultValue, "Default should be true when preference is never set")
    }

    func testPreferenceReturnsFalseWhenExplicitlySet() {
        let testKey = "notification_explicit_test_\(UUID().uuidString)"

        UserDefaults.standard.set(false, forKey: testKey)

        let wasSet = UserDefaults.standard.object(forKey: testKey) != nil
        let value = wasSet ? UserDefaults.standard.bool(forKey: testKey) : true

        XCTAssertTrue(wasSet)
        XCTAssertFalse(value)

        // Clean up
        UserDefaults.standard.removeObject(forKey: testKey)
    }

    func testPreferencePersistsBetweenReads() {
        let testKey = "notification_persist_test_\(UUID().uuidString)"

        UserDefaults.standard.set(true, forKey: testKey)

        // Read multiple times
        XCTAssertTrue(UserDefaults.standard.bool(forKey: testKey))
        XCTAssertTrue(UserDefaults.standard.bool(forKey: testKey))
        XCTAssertTrue(UserDefaults.standard.bool(forKey: testKey))

        UserDefaults.standard.set(false, forKey: testKey)

        XCTAssertFalse(UserDefaults.standard.bool(forKey: testKey))
        XCTAssertFalse(UserDefaults.standard.bool(forKey: testKey))

        // Clean up
        UserDefaults.standard.removeObject(forKey: testKey)
    }

    // MARK: - Category Iteration Tests

    func testIteratingAllCategories() {
        var categoryCount = 0
        for category in NotificationCategory.allCases {
            XCTAssertFalse(category.displayName.isEmpty)
            XCTAssertFalse(category.description.isEmpty)
            XCTAssertFalse(category.userDefaultsKey.isEmpty)
            XCTAssertFalse(category.rawValue.isEmpty)
            categoryCount += 1
        }
        XCTAssertEqual(categoryCount, 4)
    }

    // MARK: - Display Name Formatting Tests

    func testDisplayNamesAreHumanReadable() {
        for category in NotificationCategory.allCases {
            // Display names should not be camelCase or snake_case
            XCTAssertFalse(category.displayName.contains("_"), "Display name should not contain underscores")
            // Display names should have spaces
            XCTAssertTrue(category.displayName.contains(" "), "Display name should contain spaces: \(category.displayName)")
            // Display names should start with uppercase
            let firstChar = category.displayName.first!
            XCTAssertTrue(firstChar.isUppercase, "Display name should start with uppercase: \(category.displayName)")
        }
    }

    // MARK: - Description Completeness Tests

    func testDescriptionsAreCompleteSentences() {
        for category in NotificationCategory.allCases {
            // Descriptions should start with "Notify"
            XCTAssertTrue(
                category.description.hasPrefix("Notify"),
                "Description should start with 'Notify': \(category.description)"
            )
            // Descriptions should be reasonably long
            XCTAssertGreaterThan(
                category.description.count,
                20,
                "Description should be descriptive: \(category.description)"
            )
        }
    }

    // MARK: - UserDefaults Key Format Tests

    func testUserDefaultsKeyFormat() {
        for category in NotificationCategory.allCases {
            let key = category.userDefaultsKey

            // Should start with "notification_"
            XCTAssertTrue(key.hasPrefix("notification_"), "Key should start with notification_: \(key)")

            // Should end with "_enabled"
            XCTAssertTrue(key.hasSuffix("_enabled"), "Key should end with _enabled: \(key)")

            // Should contain the raw value
            XCTAssertTrue(key.contains(category.rawValue), "Key should contain raw value: \(key)")
        }
    }

    // MARK: - Edge Cases

    func testCategoryEquality() {
        let status1 = NotificationCategory.statusChange
        let status2 = NotificationCategory.statusChange
        let tool = NotificationCategory.toolComplete

        XCTAssertEqual(status1, status2)
        XCTAssertNotEqual(status1, tool)
    }

    func testCategoryInitFromRawValue() {
        let status = NotificationCategory(rawValue: "statusChange")
        let tool = NotificationCategory(rawValue: "toolComplete")
        let error = NotificationCategory(rawValue: "error")
        let edge = NotificationCategory(rawValue: "edgeStatus")
        let invalid = NotificationCategory(rawValue: "invalid")

        XCTAssertEqual(status, .statusChange)
        XCTAssertEqual(tool, .toolComplete)
        XCTAssertEqual(error, .error)
        XCTAssertEqual(edge, .edgeStatus)
        XCTAssertNil(invalid)
    }

    func testCategoryInSwitch() {
        func getMessage(for category: NotificationCategory) -> String {
            switch category {
            case .statusChange:
                return "status"
            case .toolComplete:
                return "tool"
            case .error:
                return "error"
            case .edgeStatus:
                return "edge"
            }
        }

        XCTAssertEqual(getMessage(for: .statusChange), "status")
        XCTAssertEqual(getMessage(for: .toolComplete), "tool")
        XCTAssertEqual(getMessage(for: .error), "error")
        XCTAssertEqual(getMessage(for: .edgeStatus), "edge")
    }

    // MARK: - Concurrent Preference Access

    func testConcurrentPreferenceAccess() async {
        let testKey = "notification_concurrent_\(UUID().uuidString)"

        // Set initial value
        UserDefaults.standard.set(true, forKey: testKey)

        // Perform concurrent reads
        await withTaskGroup(of: Bool.self) { group in
            for _ in 0..<100 {
                group.addTask {
                    return UserDefaults.standard.bool(forKey: testKey)
                }
            }

            for await result in group {
                XCTAssertTrue(result)
            }
        }

        // Clean up
        UserDefaults.standard.removeObject(forKey: testKey)
    }
}
