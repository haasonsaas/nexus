import Foundation
import OSLog
import WebKit

/// WKURLSchemeHandler implementation for serving canvas content from session directories.
/// Handles the nexus-canvas:// custom URL scheme.
@MainActor
final class CanvasSchemeHandler: NSObject, WKURLSchemeHandler {
    private let logger = Logger(subsystem: "com.nexus.mac", category: "canvas.scheme")
    private let baseDirectory: URL

    /// Initialize the scheme handler with a base directory for session content
    /// - Parameter baseDirectory: The directory containing session subdirectories
    init(baseDirectory: URL) {
        self.baseDirectory = baseDirectory
        super.init()
    }

    // MARK: - WKURLSchemeHandler

    func webView(_ webView: WKWebView, start urlSchemeTask: WKURLSchemeTask) {
        guard let url = urlSchemeTask.request.url else {
            fail(urlSchemeTask, with: .badURL)
            return
        }

        logger.debug("handling request url=\(url.absoluteString)")

        guard let fileURL = CanvasScheme.resolveToFilePath(url: url, baseDirectory: baseDirectory) else {
            logger.warning("invalid canvas URL url=\(url.absoluteString)")
            fail(urlSchemeTask, with: .badURL)
            return
        }

        // Security check: ensure resolved path is within base directory
        guard fileURL.path.hasPrefix(baseDirectory.path) else {
            logger.error("path traversal attempt blocked url=\(url.absoluteString)")
            fail(urlSchemeTask, with: .fileDoesNotExist)
            return
        }

        // Check if file exists
        guard FileManager.default.fileExists(atPath: fileURL.path) else {
            logger.debug("file not found path=\(fileURL.path)")
            fail(urlSchemeTask, with: .fileDoesNotExist)
            return
        }

        // Read file data
        do {
            let data = try Data(contentsOf: fileURL)
            let mimeType = detectMIMEType(for: fileURL)
            let contentLength = data.count

            let response = URLResponse(
                url: url,
                mimeType: mimeType,
                expectedContentLength: contentLength,
                textEncodingName: textEncoding(for: mimeType)
            )

            urlSchemeTask.didReceive(response)
            urlSchemeTask.didReceive(data)
            urlSchemeTask.didFinish()

            logger.debug("served file path=\(fileURL.path) type=\(mimeType) size=\(contentLength)")
        } catch {
            logger.error("failed to read file path=\(fileURL.path) error=\(error.localizedDescription)")
            fail(urlSchemeTask, with: .cannotOpenFile)
        }
    }

    func webView(_ webView: WKWebView, stop urlSchemeTask: WKURLSchemeTask) {
        // Task was cancelled - nothing to clean up
        logger.debug("scheme task cancelled")
    }

    // MARK: - Private

    private func fail(_ task: WKURLSchemeTask, with code: URLError.Code) {
        let error = URLError(code)
        task.didFailWithError(error)
    }

    /// Detect MIME type based on file extension
    private func detectMIMEType(for fileURL: URL) -> String {
        let ext = fileURL.pathExtension.lowercased()

        switch ext {
        // HTML
        case "html", "htm":
            return "text/html"

        // CSS
        case "css":
            return "text/css"

        // JavaScript
        case "js", "mjs":
            return "application/javascript"

        // JSON
        case "json":
            return "application/json"

        // Images
        case "png":
            return "image/png"
        case "jpg", "jpeg":
            return "image/jpeg"
        case "gif":
            return "image/gif"
        case "svg":
            return "image/svg+xml"
        case "webp":
            return "image/webp"
        case "ico":
            return "image/x-icon"

        // Fonts
        case "woff":
            return "font/woff"
        case "woff2":
            return "font/woff2"
        case "ttf":
            return "font/ttf"
        case "otf":
            return "font/otf"
        case "eot":
            return "application/vnd.ms-fontobject"

        // Audio
        case "mp3":
            return "audio/mpeg"
        case "wav":
            return "audio/wav"
        case "ogg":
            return "audio/ogg"

        // Video
        case "mp4":
            return "video/mp4"
        case "webm":
            return "video/webm"

        // Data
        case "xml":
            return "application/xml"
        case "csv":
            return "text/csv"

        // Plain text
        case "txt", "text":
            return "text/plain"
        case "md", "markdown":
            return "text/markdown"

        // WebAssembly
        case "wasm":
            return "application/wasm"

        // Default
        default:
            return "application/octet-stream"
        }
    }

    /// Return text encoding for text-based MIME types
    private func textEncoding(for mimeType: String) -> String? {
        let textTypes = [
            "text/html",
            "text/css",
            "text/plain",
            "text/markdown",
            "text/csv",
            "application/javascript",
            "application/json",
            "application/xml",
            "image/svg+xml"
        ]

        return textTypes.contains(mimeType) ? "utf-8" : nil
    }
}

// MARK: - Error Response Helper

extension CanvasSchemeHandler {
    /// Create an error HTML response
    static func errorHTML(title: String, message: String) -> String {
        """
        <!DOCTYPE html>
        <html>
        <head>
            <meta charset="utf-8">
            <title>\(title)</title>
            <style>
                body {
                    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
                    display: flex;
                    align-items: center;
                    justify-content: center;
                    height: 100vh;
                    margin: 0;
                    background: linear-gradient(135deg, #1a1a2e 0%, #16213e 100%);
                    color: #eee;
                }
                .error-container {
                    text-align: center;
                    padding: 40px;
                    max-width: 500px;
                }
                h1 {
                    font-size: 24px;
                    margin-bottom: 16px;
                    color: #ff6b6b;
                }
                p {
                    color: #aaa;
                    line-height: 1.6;
                }
                code {
                    background: rgba(255,255,255,0.1);
                    padding: 2px 8px;
                    border-radius: 4px;
                    font-family: 'SF Mono', Monaco, monospace;
                }
            </style>
        </head>
        <body>
            <div class="error-container">
                <h1>\(title)</h1>
                <p>\(message)</p>
            </div>
        </body>
        </html>
        """
    }
}
