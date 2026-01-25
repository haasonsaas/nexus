import XCTest
@testable import NexusMac
import Foundation
import Security

final class KeychainStoreTests: XCTestCase {

    private var keychainStore: KeychainStore!

    override func setUp() {
        super.setUp()
        keychainStore = KeychainStore()
        // Clean up any existing test data
        keychainStore.delete()
    }

    override func tearDown() {
        // Clean up after tests
        keychainStore.delete()
        keychainStore = nil
        super.tearDown()
    }

    // MARK: - Save Tests

    func testSaveAPIKey() {
        let testKey = "test-api-key-12345"
        let result = keychainStore.write(testKey)

        XCTAssertTrue(result, "Writing to keychain should succeed")
    }

    func testSaveEmptyAPIKey() {
        let result = keychainStore.write("")
        XCTAssertTrue(result, "Writing empty string to keychain should succeed")
    }

    func testSaveLongAPIKey() {
        let longKey = String(repeating: "a", count: 1000)
        let result = keychainStore.write(longKey)

        XCTAssertTrue(result, "Writing long key to keychain should succeed")
    }

    func testSaveSpecialCharacters() {
        let specialKey = "api-key!@#$%^&*()_+-=[]{}|;':\",./<>?"
        let result = keychainStore.write(specialKey)

        XCTAssertTrue(result, "Writing key with special characters should succeed")
    }

    func testSaveUnicodeCharacters() {
        let unicodeKey = "api-key-\u{1F600}-\u{1F680}-\u{2764}"
        let result = keychainStore.write(unicodeKey)

        XCTAssertTrue(result, "Writing key with unicode characters should succeed")
    }

    // MARK: - Read Tests

    func testReadAPIKey() {
        let testKey = "test-api-key-read"
        _ = keychainStore.write(testKey)

        let readKey = keychainStore.read()

        XCTAssertEqual(readKey, testKey, "Read key should match written key")
    }

    func testReadEmptyKeychain() {
        // Ensure keychain is empty
        keychainStore.delete()

        let readKey = keychainStore.read()

        XCTAssertNil(readKey, "Reading from empty keychain should return nil")
    }

    func testReadAfterWrite() {
        let testKey = "my-secret-api-key"
        let writeResult = keychainStore.write(testKey)
        XCTAssertTrue(writeResult)

        let readKey = keychainStore.read()
        XCTAssertEqual(readKey, testKey)
    }

    func testReadSpecialCharacters() {
        let specialKey = "api-key!@#$%^&*()"
        _ = keychainStore.write(specialKey)

        let readKey = keychainStore.read()

        XCTAssertEqual(readKey, specialKey)
    }

    // MARK: - Delete Tests

    func testDeleteAPIKey() {
        let testKey = "key-to-delete"
        _ = keychainStore.write(testKey)

        // Verify key exists
        XCTAssertNotNil(keychainStore.read())

        // Delete
        keychainStore.delete()

        // Verify key is gone
        XCTAssertNil(keychainStore.read())
    }

    func testDeleteEmptyKeychain() {
        // Should not crash when deleting from empty keychain
        keychainStore.delete()
        keychainStore.delete()

        XCTAssertNil(keychainStore.read())
    }

    func testDeleteAndRewrite() {
        let firstKey = "first-key"
        let secondKey = "second-key"

        _ = keychainStore.write(firstKey)
        XCTAssertEqual(keychainStore.read(), firstKey)

        keychainStore.delete()
        XCTAssertNil(keychainStore.read())

        _ = keychainStore.write(secondKey)
        XCTAssertEqual(keychainStore.read(), secondKey)
    }

    // MARK: - Overwrite Tests

    func testOverwriteAPIKey() {
        let firstKey = "first-api-key"
        let secondKey = "second-api-key"

        _ = keychainStore.write(firstKey)
        XCTAssertEqual(keychainStore.read(), firstKey)

        let updateResult = keychainStore.write(secondKey)
        XCTAssertTrue(updateResult, "Overwriting should succeed")

        XCTAssertEqual(keychainStore.read(), secondKey, "Read should return updated key")
    }

    func testMultipleOverwrites() {
        for i in 1...5 {
            let key = "api-key-\(i)"
            let result = keychainStore.write(key)
            XCTAssertTrue(result)
            XCTAssertEqual(keychainStore.read(), key)
        }
    }

    // MARK: - Edge Cases

    func testWhitespaceKey() {
        let whitespaceKey = "   api-key-with-spaces   "
        _ = keychainStore.write(whitespaceKey)

        let readKey = keychainStore.read()
        XCTAssertEqual(readKey, whitespaceKey, "Whitespace should be preserved")
    }

    func testNewlineKey() {
        let newlineKey = "api-key\nwith\nnewlines"
        _ = keychainStore.write(newlineKey)

        let readKey = keychainStore.read()
        XCTAssertEqual(readKey, newlineKey, "Newlines should be preserved")
    }

    // MARK: - Persistence Tests

    func testKeyPersistsAcrossInstances() {
        let testKey = "persistent-key"

        // Write with first instance
        let store1 = KeychainStore()
        _ = store1.write(testKey)

        // Read with second instance
        let store2 = KeychainStore()
        let readKey = store2.read()

        XCTAssertEqual(readKey, testKey, "Key should persist across instances")

        // Cleanup
        store2.delete()
    }
}
