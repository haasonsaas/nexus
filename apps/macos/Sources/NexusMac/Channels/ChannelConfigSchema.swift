import Foundation

// MARK: - Field Types

/// Defines the input type for a channel configuration field.
enum ChannelFieldType: String, Codable, Equatable {
    case text
    case password
    case number
    case toggle
    case select
    case multiSelect
}

// MARK: - Field Validation

/// Validation rules for a channel configuration field.
struct ChannelFieldValidation: Codable, Equatable {
    var minLength: Int?
    var maxLength: Int?
    var minValue: Double?
    var maxValue: Double?
    var pattern: String?
    var patternMessage: String?

    /// Validates a value against the validation rules.
    func validate(_ value: Any?) -> String? {
        guard let value else {
            return nil // Required check is handled separately
        }

        if let stringValue = value as? String {
            if let minLength, stringValue.count < minLength {
                return "Must be at least \(minLength) characters"
            }
            if let maxLength, stringValue.count > maxLength {
                return "Must be at most \(maxLength) characters"
            }
            if let pattern, let regex = try? NSRegularExpression(pattern: pattern) {
                let range = NSRange(stringValue.startIndex..., in: stringValue)
                if regex.firstMatch(in: stringValue, range: range) == nil {
                    return patternMessage ?? "Invalid format"
                }
            }
        }

        if let numberValue = value as? Double {
            if let minValue, numberValue < minValue {
                return "Must be at least \(Int(minValue))"
            }
            if let maxValue, numberValue > maxValue {
                return "Must be at most \(Int(maxValue))"
            }
        }

        if let intValue = value as? Int {
            if let minValue, Double(intValue) < minValue {
                return "Must be at least \(Int(minValue))"
            }
            if let maxValue, Double(intValue) > maxValue {
                return "Must be at most \(Int(maxValue))"
            }
        }

        return nil
    }
}

// MARK: - Select Option

/// An option for select and multi-select fields.
struct ChannelSelectOption: Identifiable, Codable, Equatable {
    var id: String { value }
    let value: String
    let label: String
}

// MARK: - Channel Field

/// Defines a single configuration field for a channel.
struct ChannelField: Identifiable, Codable, Equatable {
    let id: String
    let label: String
    let type: ChannelFieldType
    var placeholder: String?
    var required: Bool = false
    var options: [ChannelSelectOption]?
    var validation: ChannelFieldValidation?
    var helpText: String?
    var defaultValue: String?

    /// Creates a text field.
    static func text(
        id: String,
        label: String,
        placeholder: String? = nil,
        required: Bool = false,
        validation: ChannelFieldValidation? = nil,
        helpText: String? = nil
    ) -> ChannelField {
        ChannelField(
            id: id,
            label: label,
            type: .text,
            placeholder: placeholder,
            required: required,
            validation: validation,
            helpText: helpText
        )
    }

    /// Creates a password field.
    static func password(
        id: String,
        label: String,
        placeholder: String? = nil,
        required: Bool = false,
        helpText: String? = nil
    ) -> ChannelField {
        ChannelField(
            id: id,
            label: label,
            type: .password,
            placeholder: placeholder,
            required: required,
            helpText: helpText
        )
    }

    /// Creates a number field.
    static func number(
        id: String,
        label: String,
        placeholder: String? = nil,
        required: Bool = false,
        validation: ChannelFieldValidation? = nil,
        helpText: String? = nil,
        defaultValue: String? = nil
    ) -> ChannelField {
        ChannelField(
            id: id,
            label: label,
            type: .number,
            placeholder: placeholder,
            required: required,
            validation: validation,
            helpText: helpText,
            defaultValue: defaultValue
        )
    }

    /// Creates a toggle field.
    static func toggle(
        id: String,
        label: String,
        defaultValue: Bool = false,
        helpText: String? = nil
    ) -> ChannelField {
        ChannelField(
            id: id,
            label: label,
            type: .toggle,
            defaultValue: defaultValue ? "true" : "false",
            helpText: helpText
        )
    }

    /// Creates a select field.
    static func select(
        id: String,
        label: String,
        options: [ChannelSelectOption],
        required: Bool = false,
        helpText: String? = nil,
        defaultValue: String? = nil
    ) -> ChannelField {
        ChannelField(
            id: id,
            label: label,
            type: .select,
            required: required,
            options: options,
            helpText: helpText,
            defaultValue: defaultValue
        )
    }

    /// Creates a multi-select field.
    static func multiSelect(
        id: String,
        label: String,
        options: [ChannelSelectOption],
        helpText: String? = nil
    ) -> ChannelField {
        ChannelField(
            id: id,
            label: label,
            type: .multiSelect,
            options: options,
            helpText: helpText
        )
    }
}

// MARK: - Channel Schema

/// Defines the complete configuration schema for a channel type.
struct ChannelSchema: Identifiable {
    var id: String { channelType.rawValue }
    let channelType: ChannelsStore.Channel.ChannelType
    let displayName: String
    let iconName: String
    let fields: [ChannelField]
    let sections: [ChannelSchemaSection]?

    init(
        channelType: ChannelsStore.Channel.ChannelType,
        displayName: String,
        iconName: String,
        fields: [ChannelField],
        sections: [ChannelSchemaSection]? = nil
    ) {
        self.channelType = channelType
        self.displayName = displayName
        self.iconName = iconName
        self.fields = fields
        self.sections = sections
    }
}

/// A section within a channel configuration form.
struct ChannelSchemaSection: Identifiable {
    let id: String
    let title: String
    let fieldIds: [String]
}

// MARK: - Predefined Schemas

extension ChannelSchema {
    /// All available channel schemas.
    static let all: [ChannelSchema] = [
        .whatsapp,
        .telegram,
        .slack,
        .discord,
        .sms,
        .email
    ]

    /// Returns the schema for a given channel type.
    static func schema(for type: ChannelsStore.Channel.ChannelType) -> ChannelSchema {
        switch type {
        case .whatsapp: return .whatsapp
        case .telegram: return .telegram
        case .slack: return .slack
        case .discord: return .discord
        case .sms: return .sms
        case .email: return .email
        }
    }

    // MARK: - WhatsApp

    static let whatsapp = ChannelSchema(
        channelType: .whatsapp,
        displayName: "WhatsApp",
        iconName: "message.fill",
        fields: [
            .text(
                id: "phoneNumber",
                label: "Phone Number",
                placeholder: "+1234567890",
                required: true,
                validation: ChannelFieldValidation(
                    pattern: "^\\+[1-9]\\d{1,14}$",
                    patternMessage: "Enter phone number in E.164 format (e.g., +1234567890)"
                ),
                helpText: "Phone number in E.164 format"
            ),
            .password(
                id: "apiKey",
                label: "API Key",
                placeholder: "Enter your WhatsApp Business API key",
                required: true,
                helpText: "Your WhatsApp Business API key from Meta"
            ),
            .text(
                id: "webhookUrl",
                label: "Webhook URL",
                placeholder: "https://your-server.com/webhook",
                required: false,
                validation: ChannelFieldValidation(
                    pattern: "^https?://",
                    patternMessage: "Enter a valid URL"
                ),
                helpText: "URL to receive incoming message webhooks"
            ),
            .toggle(
                id: "enabled",
                label: "Enable Channel",
                defaultValue: true
            )
        ],
        sections: [
            ChannelSchemaSection(id: "auth", title: "Authentication", fieldIds: ["phoneNumber", "apiKey"]),
            ChannelSchemaSection(id: "webhooks", title: "Webhooks", fieldIds: ["webhookUrl"]),
            ChannelSchemaSection(id: "settings", title: "Settings", fieldIds: ["enabled"])
        ]
    )

    // MARK: - Telegram

    static let telegram = ChannelSchema(
        channelType: .telegram,
        displayName: "Telegram",
        iconName: "paperplane.fill",
        fields: [
            .password(
                id: "botToken",
                label: "Bot Token",
                placeholder: "123456789:ABCdefGHIjklMNOpqrSTUvwxYZ",
                required: true,
                helpText: "Token from @BotFather"
            ),
            .text(
                id: "chatId",
                label: "Chat ID",
                placeholder: "-1001234567890",
                required: false,
                helpText: "Default chat ID for sending messages"
            ),
            .select(
                id: "parseMode",
                label: "Parse Mode",
                options: [
                    ChannelSelectOption(value: "HTML", label: "HTML"),
                    ChannelSelectOption(value: "Markdown", label: "Markdown"),
                    ChannelSelectOption(value: "MarkdownV2", label: "Markdown V2")
                ],
                helpText: "Message formatting mode",
                defaultValue: "HTML"
            ),
            .toggle(
                id: "enabled",
                label: "Enable Channel",
                defaultValue: true
            )
        ],
        sections: [
            ChannelSchemaSection(id: "auth", title: "Authentication", fieldIds: ["botToken"]),
            ChannelSchemaSection(id: "config", title: "Configuration", fieldIds: ["chatId", "parseMode"]),
            ChannelSchemaSection(id: "settings", title: "Settings", fieldIds: ["enabled"])
        ]
    )

    // MARK: - Slack

    static let slack = ChannelSchema(
        channelType: .slack,
        displayName: "Slack",
        iconName: "number",
        fields: [
            .text(
                id: "workspace",
                label: "Workspace",
                placeholder: "your-workspace",
                required: true,
                helpText: "Slack workspace name"
            ),
            .password(
                id: "botToken",
                label: "Bot Token",
                placeholder: "xoxb-...",
                required: true,
                helpText: "Bot User OAuth Token (starts with xoxb-)"
            ),
            .text(
                id: "channel",
                label: "Default Channel",
                placeholder: "#general or C0123456789",
                required: false,
                helpText: "Default channel for posting messages"
            ),
            .password(
                id: "appToken",
                label: "App-Level Token",
                placeholder: "xapp-...",
                required: false,
                helpText: "App-Level Token for Socket Mode (starts with xapp-)"
            ),
            .toggle(
                id: "socketMode",
                label: "Socket Mode",
                defaultValue: false,
                helpText: "Use Socket Mode for real-time events"
            ),
            .toggle(
                id: "enabled",
                label: "Enable Channel",
                defaultValue: true
            )
        ],
        sections: [
            ChannelSchemaSection(id: "workspace", title: "Workspace", fieldIds: ["workspace"]),
            ChannelSchemaSection(id: "auth", title: "Authentication", fieldIds: ["botToken", "appToken"]),
            ChannelSchemaSection(id: "config", title: "Configuration", fieldIds: ["channel", "socketMode"]),
            ChannelSchemaSection(id: "settings", title: "Settings", fieldIds: ["enabled"])
        ]
    )

    // MARK: - Discord

    static let discord = ChannelSchema(
        channelType: .discord,
        displayName: "Discord",
        iconName: "gamecontroller.fill",
        fields: [
            .password(
                id: "botToken",
                label: "Bot Token",
                placeholder: "Enter your Discord bot token",
                required: true,
                helpText: "Bot token from Discord Developer Portal"
            ),
            .text(
                id: "guildId",
                label: "Guild ID",
                placeholder: "123456789012345678",
                required: true,
                validation: ChannelFieldValidation(
                    pattern: "^\\d{17,19}$",
                    patternMessage: "Enter a valid Discord Guild ID (17-19 digits)"
                ),
                helpText: "Server (guild) ID where the bot operates"
            ),
            .text(
                id: "channelId",
                label: "Channel ID",
                placeholder: "123456789012345678",
                required: false,
                validation: ChannelFieldValidation(
                    pattern: "^\\d{17,19}$",
                    patternMessage: "Enter a valid Discord Channel ID (17-19 digits)"
                ),
                helpText: "Default channel ID for posting messages"
            ),
            .multiSelect(
                id: "intents",
                label: "Gateway Intents",
                options: [
                    ChannelSelectOption(value: "GUILDS", label: "Guilds"),
                    ChannelSelectOption(value: "GUILD_MESSAGES", label: "Guild Messages"),
                    ChannelSelectOption(value: "DIRECT_MESSAGES", label: "Direct Messages"),
                    ChannelSelectOption(value: "MESSAGE_CONTENT", label: "Message Content")
                ],
                helpText: "Required bot intents (must match Developer Portal)"
            ),
            .toggle(
                id: "enabled",
                label: "Enable Channel",
                defaultValue: true
            )
        ],
        sections: [
            ChannelSchemaSection(id: "auth", title: "Authentication", fieldIds: ["botToken"]),
            ChannelSchemaSection(id: "server", title: "Server Configuration", fieldIds: ["guildId", "channelId"]),
            ChannelSchemaSection(id: "advanced", title: "Advanced", fieldIds: ["intents"]),
            ChannelSchemaSection(id: "settings", title: "Settings", fieldIds: ["enabled"])
        ]
    )

    // MARK: - SMS

    static let sms = ChannelSchema(
        channelType: .sms,
        displayName: "SMS",
        iconName: "bubble.left.fill",
        fields: [
            .select(
                id: "provider",
                label: "Provider",
                options: [
                    ChannelSelectOption(value: "twilio", label: "Twilio"),
                    ChannelSelectOption(value: "vonage", label: "Vonage (Nexmo)")
                ],
                required: true,
                helpText: "SMS provider to use",
                defaultValue: "twilio"
            ),
            .text(
                id: "accountSid",
                label: "Account SID",
                placeholder: "AC...",
                required: true,
                helpText: "Account SID from your provider dashboard"
            ),
            .password(
                id: "authToken",
                label: "Auth Token",
                placeholder: "Enter your auth token",
                required: true,
                helpText: "Auth token from your provider dashboard"
            ),
            .text(
                id: "fromNumber",
                label: "From Number",
                placeholder: "+1234567890",
                required: true,
                validation: ChannelFieldValidation(
                    pattern: "^\\+[1-9]\\d{1,14}$",
                    patternMessage: "Enter phone number in E.164 format"
                ),
                helpText: "Phone number to send messages from"
            ),
            .text(
                id: "webhookUrl",
                label: "Webhook URL",
                placeholder: "https://your-server.com/sms/webhook",
                required: false,
                validation: ChannelFieldValidation(
                    pattern: "^https?://",
                    patternMessage: "Enter a valid URL"
                ),
                helpText: "URL to receive incoming message webhooks"
            ),
            .toggle(
                id: "enabled",
                label: "Enable Channel",
                defaultValue: true
            )
        ],
        sections: [
            ChannelSchemaSection(id: "provider", title: "Provider", fieldIds: ["provider"]),
            ChannelSchemaSection(id: "auth", title: "Authentication", fieldIds: ["accountSid", "authToken"]),
            ChannelSchemaSection(id: "config", title: "Configuration", fieldIds: ["fromNumber", "webhookUrl"]),
            ChannelSchemaSection(id: "settings", title: "Settings", fieldIds: ["enabled"])
        ]
    )

    // MARK: - Email

    static let email = ChannelSchema(
        channelType: .email,
        displayName: "Email",
        iconName: "envelope.fill",
        fields: [
            .text(
                id: "smtpHost",
                label: "SMTP Host",
                placeholder: "smtp.example.com",
                required: true,
                helpText: "SMTP server hostname"
            ),
            .number(
                id: "smtpPort",
                label: "SMTP Port",
                placeholder: "587",
                required: true,
                validation: ChannelFieldValidation(
                    minValue: 1,
                    maxValue: 65535
                ),
                helpText: "SMTP server port (typically 25, 465, or 587)",
                defaultValue: "587"
            ),
            .text(
                id: "username",
                label: "Username",
                placeholder: "user@example.com",
                required: true,
                helpText: "SMTP authentication username"
            ),
            .password(
                id: "password",
                label: "Password",
                placeholder: "Enter your SMTP password",
                required: true,
                helpText: "SMTP authentication password or app password"
            ),
            .text(
                id: "fromAddress",
                label: "From Address",
                placeholder: "noreply@example.com",
                required: true,
                validation: ChannelFieldValidation(
                    pattern: "^[^@]+@[^@]+\\.[^@]+$",
                    patternMessage: "Enter a valid email address"
                ),
                helpText: "Email address to send from"
            ),
            .text(
                id: "fromName",
                label: "From Name",
                placeholder: "Nexus AI",
                required: false,
                helpText: "Display name for sent emails"
            ),
            .toggle(
                id: "useTLS",
                label: "Use TLS",
                defaultValue: true,
                helpText: "Enable TLS/STARTTLS encryption"
            ),
            .toggle(
                id: "enabled",
                label: "Enable Channel",
                defaultValue: true
            )
        ],
        sections: [
            ChannelSchemaSection(id: "server", title: "SMTP Server", fieldIds: ["smtpHost", "smtpPort", "useTLS"]),
            ChannelSchemaSection(id: "auth", title: "Authentication", fieldIds: ["username", "password"]),
            ChannelSchemaSection(id: "sender", title: "Sender", fieldIds: ["fromAddress", "fromName"]),
            ChannelSchemaSection(id: "settings", title: "Settings", fieldIds: ["enabled"])
        ]
    )
}

// MARK: - Config Dictionary Extensions

extension Dictionary where Key == String, Value == Any {
    /// Gets a string value from the config.
    func getString(_ key: String) -> String {
        (self[key] as? String) ?? ""
    }

    /// Gets a boolean value from the config.
    func getBool(_ key: String) -> Bool {
        if let boolValue = self[key] as? Bool {
            return boolValue
        }
        if let stringValue = self[key] as? String {
            return stringValue.lowercased() == "true"
        }
        return false
    }

    /// Gets a number value from the config.
    func getNumber(_ key: String) -> Int? {
        if let intValue = self[key] as? Int {
            return intValue
        }
        if let stringValue = self[key] as? String {
            return Int(stringValue)
        }
        return nil
    }

    /// Gets a string array from the config (for multi-select).
    func getStringArray(_ key: String) -> [String] {
        (self[key] as? [String]) ?? []
    }
}
