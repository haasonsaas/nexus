import Foundation

enum BundledBinaryLocator {
    static func path(for name: String) -> String? {
        guard let url = Bundle.main.url(forResource: name, withExtension: nil) else {
            return nil
        }

        let path = url.path
        return FileManager.default.isExecutableFile(atPath: path) ? path : nil
    }
}
