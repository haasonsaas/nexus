import Foundation
import OSLog

/// Manages messaging channel integrations (WhatsApp, Telegram, etc).
/// Enables AI agents to communicate through external messaging platforms.
@MainActor
@Observable
final class ChannelsStore {
    static let shared = ChannelsStore()

    private let logger = Logger(subsystem: "com.nexus.mac", category: "channels")

    private(set) var channels: [Channel] = []
    private(set) var isLoading = false

    struct Channel: Identifiable, Codable, Equatable {
        let id: String
        var type: ChannelType
        var name: String
        var status: ChannelStatus
        var config: ChannelConfig
        var lastMessageAt: Date?
        var messageCount: Int

        enum ChannelType: String, Codable {
            case whatsapp
            case telegram
            case slack
            case discord
            case sms
            case email
        }

        enum ChannelStatus: String, Codable {
            case disconnected
            case connecting
            case connected
            case error
            case needsAuth
        }

        struct ChannelConfig: Codable, Equatable {
            var phoneNumber: String?
            var botToken: String?
            var webhookUrl: String?
            var apiKey: String?
            var enabled: Bool
        }
    }

    // MARK: - Channel Management

    /// Load channels from gateway
    func loadChannels() async {
        isLoading = true
        defer { isLoading = false }

        do {
            let data = try await ControlChannel.shared.request(method: "channels.status")
            let response = try JSONDecoder().decode(ChannelsResponse.self, from: data)
            channels = response.channels
            logger.info("loaded \(self.channels.count) channels")
        } catch {
            logger.error("failed to load channels: \(error.localizedDescription)")
        }
    }

    /// Add a new channel
    func addChannel(_ channel: Channel) async throws {
        let params: [String: AnyHashable] = [
            "type": channel.type.rawValue,
            "name": channel.name,
            "config": try JSONEncoder().encode(channel.config).base64EncodedString()
        ]

        _ = try await ControlChannel.shared.request(
            method: "channels.add",
            params: params
        )

        await loadChannels()
        logger.info("channel added type=\(channel.type.rawValue)")
    }

    /// Update channel configuration
    func updateChannel(id: String, config: Channel.ChannelConfig) async throws {
        let params: [String: AnyHashable] = [
            "channelId": id,
            "config": try JSONEncoder().encode(config).base64EncodedString()
        ]

        _ = try await ControlChannel.shared.request(
            method: "channels.update",
            params: params
        )

        await loadChannels()
    }

    /// Remove a channel
    func removeChannel(id: String) async throws {
        _ = try await ControlChannel.shared.request(
            method: "channels.remove",
            params: ["channelId": id]
        )

        channels.removeAll { $0.id == id }
        logger.info("channel removed id=\(id)")
    }

    /// Connect a channel
    func connect(channelId: String) async throws {
        if let index = channels.firstIndex(where: { $0.id == channelId }) {
            channels[index].status = .connecting
        }

        do {
            _ = try await ControlChannel.shared.request(
                method: "channels.connect",
                params: ["channelId": channelId]
            )

            if let index = channels.firstIndex(where: { $0.id == channelId }) {
                channels[index].status = .connected
            }
            logger.info("channel connected id=\(channelId)")
        } catch {
            if let index = channels.firstIndex(where: { $0.id == channelId }) {
                channels[index].status = .error
            }
            throw error
        }
    }

    /// Disconnect a channel
    func disconnect(channelId: String) async throws {
        _ = try await ControlChannel.shared.request(
            method: "channels.disconnect",
            params: ["channelId": channelId]
        )

        if let index = channels.firstIndex(where: { $0.id == channelId }) {
            channels[index].status = .disconnected
        }
        logger.info("channel disconnected id=\(channelId)")
    }

    /// Send message through a channel
    func sendMessage(channelId: String, to: String, message: String) async throws {
        _ = try await ControlChannel.shared.request(
            method: "channels.send",
            params: [
                "channelId": channelId,
                "to": to,
                "message": message
            ]
        )
        logger.debug("message sent via channel=\(channelId)")
    }

    /// Get QR code for WhatsApp pairing
    func getWhatsAppQR(channelId: String) async throws -> String? {
        let data = try await ControlChannel.shared.request(
            method: "channels.whatsapp.qr",
            params: ["channelId": channelId]
        )

        let response = try JSONDecoder().decode(WhatsAppQRResponse.self, from: data)
        return response.qrCode
    }

    // MARK: - Channel Status

    /// Get channel by ID
    func channel(id: String) -> Channel? {
        channels.first { $0.id == id }
    }

    /// Get channels by type
    func channels(ofType type: Channel.ChannelType) -> [Channel] {
        channels.filter { $0.type == type }
    }

    /// Check if any channel is connected
    var hasConnectedChannel: Bool {
        channels.contains { $0.status == .connected }
    }
}

// MARK: - Response Models

private struct ChannelsResponse: Codable {
    let channels: [ChannelsStore.Channel]
}

private struct WhatsAppQRResponse: Codable {
    let qrCode: String?
}
