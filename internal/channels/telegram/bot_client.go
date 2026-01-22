package telegram

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// BotClient defines the interface for Telegram bot operations.
// This interface allows for mock injection in tests while wrapping
// the actual bot.Bot methods used by the adapter.
type BotClient interface {
	// SendMessage sends a text message to a chat.
	SendMessage(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error)

	// SendPhoto sends a photo to a chat.
	SendPhoto(ctx context.Context, params *bot.SendPhotoParams) (*models.Message, error)

	// SendDocument sends a document to a chat.
	SendDocument(ctx context.Context, params *bot.SendDocumentParams) (*models.Message, error)

	// SendAudio sends an audio file to a chat.
	SendAudio(ctx context.Context, params *bot.SendAudioParams) (*models.Message, error)

	// GetFile retrieves file information for downloading.
	GetFile(ctx context.Context, params *bot.GetFileParams) (*models.File, error)

	// GetMe returns information about the bot.
	GetMe(ctx context.Context) (*models.User, error)

	// SetWebhook configures a webhook for receiving updates.
	SetWebhook(ctx context.Context, params *bot.SetWebhookParams) (bool, error)

	// RegisterHandler registers a handler for a specific message type.
	RegisterHandler(handlerType bot.HandlerType, pattern string, matchType bot.MatchType, handler bot.HandlerFunc)

	// Start begins the bot (for long polling mode).
	Start(ctx context.Context)

	// StartWebhook starts the webhook server.
	StartWebhook(ctx context.Context)
}

// realBotClient wraps a *bot.Bot to implement BotClient.
type realBotClient struct {
	bot *bot.Bot
}

// newRealBotClient creates a new realBotClient wrapping the given bot.
func newRealBotClient(b *bot.Bot) BotClient {
	return &realBotClient{bot: b}
}

func (r *realBotClient) SendMessage(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error) {
	return r.bot.SendMessage(ctx, params)
}

func (r *realBotClient) SendPhoto(ctx context.Context, params *bot.SendPhotoParams) (*models.Message, error) {
	return r.bot.SendPhoto(ctx, params)
}

func (r *realBotClient) SendDocument(ctx context.Context, params *bot.SendDocumentParams) (*models.Message, error) {
	return r.bot.SendDocument(ctx, params)
}

func (r *realBotClient) SendAudio(ctx context.Context, params *bot.SendAudioParams) (*models.Message, error) {
	return r.bot.SendAudio(ctx, params)
}

func (r *realBotClient) GetFile(ctx context.Context, params *bot.GetFileParams) (*models.File, error) {
	return r.bot.GetFile(ctx, params)
}

func (r *realBotClient) GetMe(ctx context.Context) (*models.User, error) {
	return r.bot.GetMe(ctx)
}

func (r *realBotClient) SetWebhook(ctx context.Context, params *bot.SetWebhookParams) (bool, error) {
	return r.bot.SetWebhook(ctx, params)
}

func (r *realBotClient) RegisterHandler(handlerType bot.HandlerType, pattern string, matchType bot.MatchType, handler bot.HandlerFunc) {
	r.bot.RegisterHandler(handlerType, pattern, matchType, handler)
}

func (r *realBotClient) Start(ctx context.Context) {
	r.bot.Start(ctx)
}

func (r *realBotClient) StartWebhook(ctx context.Context) {
	r.bot.StartWebhook(ctx)
}
