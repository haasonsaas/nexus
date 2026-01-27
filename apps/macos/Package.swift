// swift-tools-version: 5.9
// Package manifest for the Nexus macOS companion (menu bar app).

import PackageDescription

let package = Package(
    name: "NexusMac",
    platforms: [
        .macOS(.v14),
    ],
    products: [
        .executable(name: "NexusMac", targets: ["NexusMac"]),
    ],
    dependencies: [
        .package(url: "https://github.com/apple/swift-log.git", from: "1.5.0"),
    ],
    targets: [
        .target(
            name: "NexusMacObjC",
            path: "Sources/NexusMacObjC",
            publicHeadersPath: "include"
        ),
        .executableTarget(
            name: "NexusMac",
            dependencies: [
                "NexusMacObjC",
                .product(name: "Logging", package: "swift-log"),
            ],
            path: "Sources/NexusMac"
        ),
        .testTarget(
            name: "NexusMacTests",
            dependencies: ["NexusMac"]
        ),
    ]
)
