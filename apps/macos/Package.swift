// swift-tools-version: 5.9
// Package manifest for the Nexus macOS companion (menu bar app).

import PackageDescription

let package = Package(
    name: "NexusMac",
    platforms: [
        .macOS(.v13),
    ],
    products: [
        .executable(name: "NexusMac", targets: ["NexusMac"]),
    ],
    dependencies: [],
    targets: [
        .executableTarget(
            name: "NexusMac",
            path: "Sources/NexusMac"
        ),
    ]
)
