package config

import "time"

type ChannelsConfig struct {
	Telegram TelegramConfig `yaml:"telegram"`
	Discord  DiscordConfig  `yaml:"discord"`
	Slack    SlackConfig    `yaml:"slack"`
	WhatsApp WhatsAppConfig `yaml:"whatsapp"`
	Signal   SignalConfig   `yaml:"signal"`
	IMessage IMessageConfig `yaml:"imessage"`
	Matrix   MatrixConfig   `yaml:"matrix"`
	Teams    TeamsConfig    `yaml:"teams"`
	Email    EmailConfig    `yaml:"email"`

	Mattermost    MattermostConfig    `yaml:"mattermost"`
	NextcloudTalk NextcloudTalkConfig `yaml:"nextcloud_talk"`
	Zalo          ZaloConfig          `yaml:"zalo"`
	BlueBubbles   BlueBubblesConfig   `yaml:"bluebubbles"`

	HomeAssistant HomeAssistantConfig `yaml:"homeassistant"`
}

type ChannelPolicyConfig struct {
	// Policy controls access: "open", "allowlist", "pairing", or "disabled".
	Policy string `yaml:"policy"`
	// AllowFrom is a list of sender identifiers allowed for this policy.
	AllowFrom []string `yaml:"allow_from"`
}

// ChannelMarkdownConfig configures markdown processing for a channel.
type ChannelMarkdownConfig struct {
	// Tables specifies how to handle markdown tables: "off", "bullets", or "code".
	// - "off": Leave tables unchanged (for channels that support markdown tables)
	// - "bullets": Convert tables to bullet lists (for channels like Signal, WhatsApp)
	// - "code": Wrap tables in code blocks (for channels like Slack, Discord)
	// Default depends on channel type.
	Tables string `yaml:"tables"`
}

type TelegramConfig struct {
	Enabled  bool   `yaml:"enabled"`
	BotToken string `yaml:"bot_token"`
	Webhook  string `yaml:"webhook"`

	DM    ChannelPolicyConfig `yaml:"dm"`
	Group ChannelPolicyConfig `yaml:"group"`

	Markdown ChannelMarkdownConfig `yaml:"markdown"`
}

type DiscordConfig struct {
	Enabled  bool   `yaml:"enabled"`
	BotToken string `yaml:"bot_token"`
	AppID    string `yaml:"app_id"`

	DM    ChannelPolicyConfig `yaml:"dm"`
	Group ChannelPolicyConfig `yaml:"group"`

	Markdown ChannelMarkdownConfig `yaml:"markdown"`
}

type SlackConfig struct {
	Enabled       bool   `yaml:"enabled"`
	BotToken      string `yaml:"bot_token"`
	AppToken      string `yaml:"app_token"`
	SigningSecret string `yaml:"signing_secret"`
	// UploadAttachments enables Slack file uploads for outbound attachments.
	UploadAttachments bool `yaml:"upload_attachments"`

	DM    ChannelPolicyConfig `yaml:"dm"`
	Group ChannelPolicyConfig `yaml:"group"`

	Markdown ChannelMarkdownConfig `yaml:"markdown"`
	Canvas   SlackCanvasConfig     `yaml:"canvas"`
}

type SlackCanvasConfig struct {
	Enabled           bool                         `yaml:"enabled"`
	Command           string                       `yaml:"command"`
	ShortcutCallback  string                       `yaml:"shortcut_callback"`
	AllowedWorkspaces []string                     `yaml:"allowed_workspaces"`
	Role              string                       `yaml:"role"`
	DefaultRole       string                       `yaml:"default_role"`
	WorkspaceRoles    map[string]string            `yaml:"workspace_roles"`
	UserRoles         map[string]map[string]string `yaml:"user_roles"`
}

type WhatsAppConfig struct {
	Enabled      bool   `yaml:"enabled"`
	SessionPath  string `yaml:"session_path"`
	MediaPath    string `yaml:"media_path"`
	SyncContacts bool   `yaml:"sync_contacts"`

	DM    ChannelPolicyConfig `yaml:"dm"`
	Group ChannelPolicyConfig `yaml:"group"`

	Presence WhatsAppPresenceConfig `yaml:"presence"`
	Markdown ChannelMarkdownConfig  `yaml:"markdown"`
}

type WhatsAppPresenceConfig struct {
	SendReadReceipts bool `yaml:"send_read_receipts"`
	SendTyping       bool `yaml:"send_typing"`
	BroadcastOnline  bool `yaml:"broadcast_online"`
}

type SignalConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Account       string `yaml:"account"`
	SignalCLIPath string `yaml:"signal_cli_path"`
	ConfigDir     string `yaml:"config_dir"`

	DM    ChannelPolicyConfig `yaml:"dm"`
	Group ChannelPolicyConfig `yaml:"group"`

	Presence SignalPresenceConfig  `yaml:"presence"`
	Markdown ChannelMarkdownConfig `yaml:"markdown"`
}

type SignalPresenceConfig struct {
	SendReadReceipts bool `yaml:"send_read_receipts"`
	SendTyping       bool `yaml:"send_typing"`
}

type IMessageConfig struct {
	Enabled      bool   `yaml:"enabled"`
	DatabasePath string `yaml:"database_path"`
	PollInterval string `yaml:"poll_interval"`

	DM    ChannelPolicyConfig `yaml:"dm"`
	Group ChannelPolicyConfig `yaml:"group"`
}

type MatrixConfig struct {
	Enabled      bool     `yaml:"enabled"`
	Homeserver   string   `yaml:"homeserver"`
	UserID       string   `yaml:"user_id"`
	AccessToken  string   `yaml:"access_token"`
	DeviceID     string   `yaml:"device_id"`
	AllowedRooms []string `yaml:"allowed_rooms"`
	AllowedUsers []string `yaml:"allowed_users"`
	JoinOnInvite bool     `yaml:"join_on_invite"`

	DM    ChannelPolicyConfig `yaml:"dm"`
	Group ChannelPolicyConfig `yaml:"group"`
}

type TeamsConfig struct {
	Enabled      bool   `yaml:"enabled"`
	TenantID     string `yaml:"tenant_id"`
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	// WebhookURL is the public URL for receiving Teams notifications
	WebhookURL string `yaml:"webhook_url"`
	// PollInterval for checking messages when webhooks unavailable (default: 5s)
	PollInterval string `yaml:"poll_interval"`

	DM    ChannelPolicyConfig `yaml:"dm"`
	Group ChannelPolicyConfig `yaml:"group"`
}

type EmailConfig struct {
	Enabled      bool   `yaml:"enabled"`
	TenantID     string `yaml:"tenant_id"`
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	// UserEmail is the email address to monitor (for app-only auth)
	UserEmail string `yaml:"user_email"`
	// FolderID specifies which folder to monitor (default: inbox)
	FolderID string `yaml:"folder_id"`
	// IncludeRead determines whether to process already-read messages
	IncludeRead bool `yaml:"include_read"`
	// AutoMarkRead marks messages as read after processing
	AutoMarkRead bool `yaml:"auto_mark_read"`
	// PollInterval for checking new emails (default: 30s)
	PollInterval string `yaml:"poll_interval"`
}

type MattermostConfig struct {
	Enabled bool `yaml:"enabled"`

	// ServerURL is the Mattermost server URL (required).
	ServerURL string `yaml:"server_url"`

	// Token is the bot token for API calls (optional).
	// Either Token or (Username + Password) must be provided.
	Token string `yaml:"token"`

	// Username for login-based authentication (optional).
	Username string `yaml:"username"`

	// Password for login-based authentication (optional).
	Password string `yaml:"password"`

	// TeamName is the default team to operate in (optional).
	TeamName string `yaml:"team_name"`

	// RateLimit configures rate limiting for API calls (ops/sec).
	RateLimit float64 `yaml:"rate_limit"`

	// RateBurst configures burst capacity for rate limiting.
	RateBurst int `yaml:"rate_burst"`

	DM    ChannelPolicyConfig `yaml:"dm"`
	Group ChannelPolicyConfig `yaml:"group"`
}

type NextcloudTalkConfig struct {
	Enabled bool `yaml:"enabled"`

	// BaseURL is the Nextcloud server base URL (required).
	BaseURL string `yaml:"base_url"`

	// BotSecret is the bot secret for webhook verification (required).
	BotSecret string `yaml:"bot_secret"`

	// WebhookPort is the port for the webhook server (default: 8788).
	WebhookPort int `yaml:"webhook_port"`

	// WebhookHost is the host for the webhook server (default: 0.0.0.0).
	WebhookHost string `yaml:"webhook_host"`

	// WebhookPath is the path for the webhook endpoint (default: /nextcloud-talk-webhook).
	WebhookPath string `yaml:"webhook_path"`

	// RateLimit configures rate limiting for API calls (ops/sec).
	RateLimit float64 `yaml:"rate_limit"`

	// RateBurst configures burst capacity for rate limiting.
	RateBurst int `yaml:"rate_burst"`

	DM    ChannelPolicyConfig `yaml:"dm"`
	Group ChannelPolicyConfig `yaml:"group"`
}

type ZaloConfig struct {
	Enabled bool `yaml:"enabled"`

	// Token is the Zalo bot token (required).
	Token string `yaml:"token"`

	// WebhookURL is the public URL for webhook callbacks (optional).
	WebhookURL string `yaml:"webhook_url"`

	// WebhookSecret is the secret for validating webhook signatures (optional).
	WebhookSecret string `yaml:"webhook_secret"`

	// WebhookPath is the path for the webhook endpoint (default: /webhook/zalo).
	WebhookPath string `yaml:"webhook_path"`

	// PollTimeout is the long-polling timeout in seconds (default: 30).
	PollTimeout int `yaml:"poll_timeout"`

	DM    ChannelPolicyConfig `yaml:"dm"`
	Group ChannelPolicyConfig `yaml:"group"`
}

type BlueBubblesConfig struct {
	Enabled bool `yaml:"enabled"`

	// ServerURL is the BlueBubbles server URL (required).
	ServerURL string `yaml:"server_url"`

	// Password is the API password (required).
	Password string `yaml:"password"`

	// WebhookPath is the path for webhook callbacks (default: /webhook/bluebubbles).
	WebhookPath string `yaml:"webhook_path"`

	// Timeout is the HTTP timeout (default: 10s).
	Timeout string `yaml:"timeout"`

	DM    ChannelPolicyConfig `yaml:"dm"`
	Group ChannelPolicyConfig `yaml:"group"`
}

type HomeAssistantConfig struct {
	Enabled bool `yaml:"enabled"`

	// BaseURL is the Home Assistant instance URL (e.g., http://homeassistant:8123).
	BaseURL string `yaml:"base_url"`

	// Token is a long-lived access token.
	Token string `yaml:"token"`

	// Timeout is the request timeout when calling Home Assistant APIs.
	Timeout time.Duration `yaml:"timeout"`
}
