package slack

import (
	"context"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

// SlackAPIClient defines the interface for Slack API operations used by the adapter.
// This interface allows for mock injection during testing.
type SlackAPIClient interface {
	// Authentication
	AuthTest() (*slack.AuthTestResponse, error)
	AuthTestContext(ctx context.Context) (*slack.AuthTestResponse, error)

	// Messaging
	PostMessage(channelID string, options ...slack.MsgOption) (string, string, error)
	PostMessageContext(ctx context.Context, channelID string, options ...slack.MsgOption) (string, string, error)
	UpdateMessageContext(ctx context.Context, channelID, timestamp string, options ...slack.MsgOption) (string, string, string, error)

	// Reactions
	AddReaction(name string, item slack.ItemRef) error
	AddReactionContext(ctx context.Context, name string, item slack.ItemRef) error

	// File operations
	UploadFileV2(params slack.UploadFileV2Parameters) (*slack.FileSummary, error)
	UploadFileV2Context(ctx context.Context, params slack.UploadFileV2Parameters) (*slack.FileSummary, error)

	// User info
	GetUserInfo(userID string) (*slack.User, error)
	GetUserInfoContext(ctx context.Context, userID string) (*slack.User, error)

	// Conversations
	GetConversationInfo(input *slack.GetConversationInfoInput) (*slack.Channel, error)
	GetConversationInfoContext(ctx context.Context, input *slack.GetConversationInfoInput) (*slack.Channel, error)
}

// SocketModeClient defines the interface for Socket Mode operations.
// This interface allows for mock injection during testing.
type SocketModeClient interface {
	// Run starts the Socket Mode client
	Run() error

	// Ack acknowledges an event
	Ack(req socketmode.Request, payload ...interface{})

	// Events returns the channel for receiving events
	Events() <-chan socketmode.Event
}

// Ensure slack.Client implements SlackAPIClient
var _ SlackAPIClient = (*slack.Client)(nil)

// MockSlackClient is a test double for SlackAPIClient.
type MockSlackClient struct {
	AuthTestFunc             func() (*slack.AuthTestResponse, error)
	AuthTestContextFunc      func(ctx context.Context) (*slack.AuthTestResponse, error)
	PostMessageFunc          func(channelID string, options ...slack.MsgOption) (string, string, error)
	PostMessageContextFunc   func(ctx context.Context, channelID string, options ...slack.MsgOption) (string, string, error)
	UpdateMessageContextFunc func(ctx context.Context, channelID, timestamp string, options ...slack.MsgOption) (string, string, string, error)
	AddReactionFunc          func(name string, item slack.ItemRef) error
	AddReactionContextFunc   func(ctx context.Context, name string, item slack.ItemRef) error
	UploadFileV2Func         func(params slack.UploadFileV2Parameters) (*slack.FileSummary, error)
	UploadFileV2ContextFunc  func(ctx context.Context, params slack.UploadFileV2Parameters) (*slack.FileSummary, error)
	GetUserInfoFunc          func(userID string) (*slack.User, error)
	GetUserInfoContextFunc   func(ctx context.Context, userID string) (*slack.User, error)
	GetConversationInfoFunc  func(input *slack.GetConversationInfoInput) (*slack.Channel, error)
	GetConversationInfoCtxFn func(ctx context.Context, input *slack.GetConversationInfoInput) (*slack.Channel, error)
}

func (m *MockSlackClient) AuthTest() (*slack.AuthTestResponse, error) {
	if m.AuthTestFunc != nil {
		return m.AuthTestFunc()
	}
	return &slack.AuthTestResponse{UserID: "U12345", Team: "TestTeam"}, nil
}

func (m *MockSlackClient) AuthTestContext(ctx context.Context) (*slack.AuthTestResponse, error) {
	if m.AuthTestContextFunc != nil {
		return m.AuthTestContextFunc(ctx)
	}
	return m.AuthTest()
}

func (m *MockSlackClient) PostMessage(channelID string, options ...slack.MsgOption) (string, string, error) {
	if m.PostMessageFunc != nil {
		return m.PostMessageFunc(channelID, options...)
	}
	return channelID, "1234567890.123456", nil
}

func (m *MockSlackClient) PostMessageContext(ctx context.Context, channelID string, options ...slack.MsgOption) (string, string, error) {
	if m.PostMessageContextFunc != nil {
		return m.PostMessageContextFunc(ctx, channelID, options...)
	}
	return m.PostMessage(channelID, options...)
}

func (m *MockSlackClient) UpdateMessageContext(ctx context.Context, channelID, timestamp string, options ...slack.MsgOption) (string, string, string, error) {
	if m.UpdateMessageContextFunc != nil {
		return m.UpdateMessageContextFunc(ctx, channelID, timestamp, options...)
	}
	return channelID, timestamp, "", nil
}

func (m *MockSlackClient) AddReaction(name string, item slack.ItemRef) error {
	if m.AddReactionFunc != nil {
		return m.AddReactionFunc(name, item)
	}
	return nil
}

func (m *MockSlackClient) AddReactionContext(ctx context.Context, name string, item slack.ItemRef) error {
	if m.AddReactionContextFunc != nil {
		return m.AddReactionContextFunc(ctx, name, item)
	}
	return m.AddReaction(name, item)
}

func (m *MockSlackClient) UploadFileV2(params slack.UploadFileV2Parameters) (*slack.FileSummary, error) {
	if m.UploadFileV2Func != nil {
		return m.UploadFileV2Func(params)
	}
	return &slack.FileSummary{ID: "F12345"}, nil
}

func (m *MockSlackClient) UploadFileV2Context(ctx context.Context, params slack.UploadFileV2Parameters) (*slack.FileSummary, error) {
	if m.UploadFileV2ContextFunc != nil {
		return m.UploadFileV2ContextFunc(ctx, params)
	}
	return m.UploadFileV2(params)
}

func (m *MockSlackClient) GetUserInfo(userID string) (*slack.User, error) {
	if m.GetUserInfoFunc != nil {
		return m.GetUserInfoFunc(userID)
	}
	return &slack.User{ID: userID, Name: "testuser"}, nil
}

func (m *MockSlackClient) GetUserInfoContext(ctx context.Context, userID string) (*slack.User, error) {
	if m.GetUserInfoContextFunc != nil {
		return m.GetUserInfoContextFunc(ctx, userID)
	}
	return m.GetUserInfo(userID)
}

func (m *MockSlackClient) GetConversationInfo(input *slack.GetConversationInfoInput) (*slack.Channel, error) {
	if m.GetConversationInfoFunc != nil {
		return m.GetConversationInfoFunc(input)
	}
	return &slack.Channel{GroupConversation: slack.GroupConversation{Name: "test-channel"}}, nil
}

func (m *MockSlackClient) GetConversationInfoContext(ctx context.Context, input *slack.GetConversationInfoInput) (*slack.Channel, error) {
	if m.GetConversationInfoCtxFn != nil {
		return m.GetConversationInfoCtxFn(ctx, input)
	}
	return m.GetConversationInfo(input)
}

// MockSocketModeClient is a test double for SocketModeClient.
type MockSocketModeClient struct {
	RunFunc    func() error
	AckFunc    func(req socketmode.Request, payload ...interface{})
	EventsChan chan socketmode.Event
}

func NewMockSocketModeClient() *MockSocketModeClient {
	return &MockSocketModeClient{
		EventsChan: make(chan socketmode.Event, 100),
	}
}

func (m *MockSocketModeClient) Run() error {
	if m.RunFunc != nil {
		return m.RunFunc()
	}
	// Block forever by default (simulate real socket mode behavior)
	select {}
}

func (m *MockSocketModeClient) Ack(req socketmode.Request, payload ...interface{}) {
	if m.AckFunc != nil {
		m.AckFunc(req, payload...)
	}
}

func (m *MockSocketModeClient) Events() <-chan socketmode.Event {
	return m.EventsChan
}

// Close closes the events channel for cleanup
func (m *MockSocketModeClient) Close() {
	close(m.EventsChan)
}
