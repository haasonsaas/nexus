// Package gateway provides the main Nexus gateway server.
//
// provisioning_service.go implements the ProvisioningService gRPC handlers for channel setup flows.
package gateway

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	proto "github.com/haasonsaas/nexus/pkg/proto"
)

// provisioningService implements the proto.ProvisioningServiceServer interface.
type provisioningService struct {
	proto.UnimplementedProvisioningServiceServer
	server   *Server
	sessions *provisioningSessionStore
}

// newProvisioningService creates a new provisioning service handler.
func newProvisioningService(s *Server) *provisioningService {
	return &provisioningService{
		server:   s,
		sessions: newProvisioningSessionStore(),
	}
}

// provisioningSessionStore manages active provisioning sessions.
type provisioningSessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*provisioningSession
}

func newProvisioningSessionStore() *provisioningSessionStore {
	return &provisioningSessionStore{
		sessions: make(map[string]*provisioningSession),
	}
}

// provisioningSession is the internal representation of a provisioning session.
type provisioningSession struct {
	ID               string
	ChannelType      string
	Status           proto.ProvisioningStatus
	Steps            []*proto.ProvisioningStep
	CurrentStepIndex int
	Error            string
	EdgeID           string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ExpiresAt        time.Time
	Data             map[string]string
}

func (s *provisioningSessionStore) Create(session *provisioningSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
}

func (s *provisioningSessionStore) Get(id string) *provisioningSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[id]
}

func (s *provisioningSessionStore) Update(session *provisioningSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session.UpdatedAt = time.Now()
	s.sessions[session.ID] = session
}

func (s *provisioningSessionStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// StartProvisioning begins a new provisioning session for a channel.
func (s *provisioningService) StartProvisioning(ctx context.Context, req *proto.StartProvisioningRequest) (*proto.StartProvisioningResponse, error) {
	if req.ChannelType == "" {
		return nil, fmt.Errorf("channel_type is required")
	}

	// Get provisioning requirements for this channel type
	reqs := s.getRequirementsForChannel(req.ChannelType)
	if reqs == nil {
		return nil, fmt.Errorf("unknown channel type: %s", req.ChannelType)
	}

	now := time.Now()
	session := &provisioningSession{
		ID:               uuid.NewString(),
		ChannelType:      req.ChannelType,
		Status:           proto.ProvisioningStatus_PROVISIONING_STATUS_IN_PROGRESS,
		Steps:            reqs.Steps,
		CurrentStepIndex: 0,
		EdgeID:           req.EdgeId,
		CreatedAt:        now,
		UpdatedAt:        now,
		ExpiresAt:        now.Add(30 * time.Minute), // Sessions expire after 30 minutes
		Data:             req.Config,
	}

	// Set first step to active
	if len(session.Steps) > 0 {
		session.Steps[0].Status = proto.ProvisioningStepStatus_PROVISIONING_STEP_STATUS_ACTIVE
	}

	s.sessions.Create(session)

	s.server.logger.Info("provisioning session started",
		"session_id", session.ID,
		"channel_type", session.ChannelType,
		"steps", len(session.Steps),
	)

	return &proto.StartProvisioningResponse{
		Session: provisioningSessionToProto(session),
	}, nil
}

// GetProvisioningStatus retrieves the current status of a provisioning session.
func (s *provisioningService) GetProvisioningStatus(ctx context.Context, req *proto.GetProvisioningStatusRequest) (*proto.GetProvisioningStatusResponse, error) {
	session := s.sessions.Get(req.SessionId)
	if session == nil {
		return nil, fmt.Errorf("provisioning session not found: %s", req.SessionId)
	}

	// Check expiration
	if time.Now().After(session.ExpiresAt) {
		session.Status = proto.ProvisioningStatus_PROVISIONING_STATUS_EXPIRED
		s.sessions.Update(session)
	}

	return &proto.GetProvisioningStatusResponse{
		Session: provisioningSessionToProto(session),
	}, nil
}

// SubmitProvisioningStep submits data for a provisioning step.
func (s *provisioningService) SubmitProvisioningStep(ctx context.Context, req *proto.SubmitProvisioningStepRequest) (*proto.SubmitProvisioningStepResponse, error) {
	session := s.sessions.Get(req.SessionId)
	if session == nil {
		return nil, fmt.Errorf("provisioning session not found: %s", req.SessionId)
	}

	if session.Status != proto.ProvisioningStatus_PROVISIONING_STATUS_IN_PROGRESS {
		return nil, fmt.Errorf("session is not in progress")
	}

	// Find the step
	stepIdx := -1
	for i, step := range session.Steps {
		if step.Id == req.StepId {
			stepIdx = i
			break
		}
	}
	if stepIdx == -1 {
		return nil, fmt.Errorf("step not found: %s", req.StepId)
	}

	// Mark step as completed
	session.Steps[stepIdx].Status = proto.ProvisioningStepStatus_PROVISIONING_STEP_STATUS_COMPLETED

	// Store submitted data
	if session.Data == nil {
		session.Data = make(map[string]string)
	}
	for k, v := range req.Data {
		session.Data[k] = v
	}

	// Move to next step
	session.CurrentStepIndex = stepIdx + 1
	if session.CurrentStepIndex < len(session.Steps) {
		session.Steps[session.CurrentStepIndex].Status = proto.ProvisioningStepStatus_PROVISIONING_STEP_STATUS_ACTIVE
	} else {
		// All steps completed
		session.Status = proto.ProvisioningStatus_PROVISIONING_STATUS_COMPLETED

		// TODO: Actually provision the channel with the collected data
		s.server.logger.Info("provisioning completed",
			"session_id", session.ID,
			"channel_type", session.ChannelType,
		)
	}

	s.sessions.Update(session)

	return &proto.SubmitProvisioningStepResponse{
		Session: provisioningSessionToProto(session),
	}, nil
}

// CancelProvisioning cancels an active provisioning session.
func (s *provisioningService) CancelProvisioning(ctx context.Context, req *proto.CancelProvisioningRequest) (*proto.CancelProvisioningResponse, error) {
	session := s.sessions.Get(req.SessionId)
	if session == nil {
		return nil, fmt.Errorf("provisioning session not found: %s", req.SessionId)
	}

	session.Status = proto.ProvisioningStatus_PROVISIONING_STATUS_CANCELLED
	s.sessions.Update(session)

	s.server.logger.Info("provisioning cancelled",
		"session_id", session.ID,
		"channel_type", session.ChannelType,
	)

	return &proto.CancelProvisioningResponse{Success: true}, nil
}

// GetProvisioningRequirements returns the provisioning requirements for a channel type.
func (s *provisioningService) GetProvisioningRequirements(ctx context.Context, req *proto.GetProvisioningRequirementsRequest) (*proto.GetProvisioningRequirementsResponse, error) {
	if req.ChannelType != "" {
		reqs := s.getRequirementsForChannel(req.ChannelType)
		if reqs == nil {
			return nil, fmt.Errorf("unknown channel type: %s", req.ChannelType)
		}
		return &proto.GetProvisioningRequirementsResponse{
			Requirements: []*proto.ProvisioningRequirements{reqs},
		}, nil
	}

	// Return all requirements
	allReqs := s.getAllRequirements()
	return &proto.GetProvisioningRequirementsResponse{
		Requirements: allReqs,
	}, nil
}

// getRequirementsForChannel returns provisioning requirements for a specific channel type.
func (s *provisioningService) getRequirementsForChannel(channelType string) *proto.ProvisioningRequirements {
	allReqs := s.getAllRequirements()
	for _, req := range allReqs {
		if req.ChannelType == channelType {
			return req
		}
	}
	return nil
}

// getAllRequirements returns provisioning requirements for all supported channels.
func (s *provisioningService) getAllRequirements() []*proto.ProvisioningRequirements {
	return []*proto.ProvisioningRequirements{
		{
			ChannelType:   "telegram",
			DisplayName:   "Telegram",
			Description:   "Connect a Telegram bot",
			RequiresEdge:  false,
			EstimatedTime: "2 minutes",
			DocsUrl:       "https://core.telegram.org/bots#how-do-i-create-a-bot",
			Steps: []*proto.ProvisioningStep{
				{
					Id:          "token",
					Type:        proto.ProvisioningStepType_PROVISIONING_STEP_TYPE_TOKEN_ENTRY,
					Title:       "Enter Bot Token",
					Description: "Enter your Telegram bot token from @BotFather",
					Status:      proto.ProvisioningStepStatus_PROVISIONING_STEP_STATUS_PENDING,
					InputFields: []*proto.ProvisioningInputField{
						{
							Name:        "bot_token",
							Label:       "Bot Token",
							Type:        "password",
							Required:    true,
							Placeholder: "123456789:ABCdefGHIjklMNOpqrsTUVwxyZ",
							HelpText:    "Get this from @BotFather on Telegram",
						},
					},
				},
			},
		},
		{
			ChannelType:   "discord",
			DisplayName:   "Discord",
			Description:   "Connect a Discord bot",
			RequiresEdge:  false,
			EstimatedTime: "5 minutes",
			DocsUrl:       "https://discord.com/developers/docs/intro",
			Steps: []*proto.ProvisioningStep{
				{
					Id:          "token",
					Type:        proto.ProvisioningStepType_PROVISIONING_STEP_TYPE_TOKEN_ENTRY,
					Title:       "Enter Bot Token",
					Description: "Enter your Discord bot token from the Developer Portal",
					Status:      proto.ProvisioningStepStatus_PROVISIONING_STEP_STATUS_PENDING,
					InputFields: []*proto.ProvisioningInputField{
						{
							Name:     "bot_token",
							Label:    "Bot Token",
							Type:     "password",
							Required: true,
							HelpText: "Get this from Discord Developer Portal",
						},
						{
							Name:     "application_id",
							Label:    "Application ID",
							Type:     "text",
							Required: true,
							HelpText: "Your Discord application ID",
						},
					},
				},
			},
		},
		{
			ChannelType:   "slack",
			DisplayName:   "Slack",
			Description:   "Connect to a Slack workspace",
			RequiresEdge:  false,
			EstimatedTime: "5 minutes",
			DocsUrl:       "https://api.slack.com/apps",
			Steps: []*proto.ProvisioningStep{
				{
					Id:          "tokens",
					Type:        proto.ProvisioningStepType_PROVISIONING_STEP_TYPE_TOKEN_ENTRY,
					Title:       "Enter Slack Tokens",
					Description: "Enter your Slack app tokens",
					Status:      proto.ProvisioningStepStatus_PROVISIONING_STEP_STATUS_PENDING,
					InputFields: []*proto.ProvisioningInputField{
						{
							Name:        "bot_token",
							Label:       "Bot Token",
							Type:        "password",
							Required:    true,
							Placeholder: "xoxb-...",
							HelpText:    "OAuth bot token starting with xoxb-",
						},
						{
							Name:        "app_token",
							Label:       "App Token",
							Type:        "password",
							Required:    true,
							Placeholder: "xapp-...",
							HelpText:    "App-level token for Socket Mode",
						},
					},
				},
			},
		},
		{
			ChannelType:   "whatsapp",
			DisplayName:   "WhatsApp",
			Description:   "Connect WhatsApp via QR code",
			RequiresEdge:  true,
			EstimatedTime: "3 minutes",
			Steps: []*proto.ProvisioningStep{
				{
					Id:           "qr",
					Type:         proto.ProvisioningStepType_PROVISIONING_STEP_TYPE_QR_CODE,
					Title:        "Scan QR Code",
					Description:  "Scan the QR code with WhatsApp on your phone",
					Status:       proto.ProvisioningStepStatus_PROVISIONING_STEP_STATUS_PENDING,
					RequiresEdge: true,
					Data: map[string]string{
						"instructions": "1. Open WhatsApp on your phone\n2. Tap Menu > Linked Devices\n3. Tap 'Link a Device'\n4. Scan the QR code",
					},
				},
				{
					Id:          "wait",
					Type:        proto.ProvisioningStepType_PROVISIONING_STEP_TYPE_WAIT,
					Title:       "Connecting...",
					Description: "Waiting for WhatsApp connection to complete",
					Status:      proto.ProvisioningStepStatus_PROVISIONING_STEP_STATUS_PENDING,
				},
			},
		},
		{
			ChannelType:   "signal",
			DisplayName:   "Signal",
			Description:   "Connect Signal messenger",
			RequiresEdge:  true,
			EstimatedTime: "5 minutes",
			Steps: []*proto.ProvisioningStep{
				{
					Id:           "phone",
					Type:         proto.ProvisioningStepType_PROVISIONING_STEP_TYPE_PHONE_NUMBER,
					Title:        "Enter Phone Number",
					Description:  "Enter your Signal phone number",
					Status:       proto.ProvisioningStepStatus_PROVISIONING_STEP_STATUS_PENDING,
					RequiresEdge: true,
					InputFields: []*proto.ProvisioningInputField{
						{
							Name:        "phone_number",
							Label:       "Phone Number",
							Type:        "tel",
							Required:    true,
							Placeholder: "+1234567890",
							HelpText:    "Include country code",
						},
					},
				},
				{
					Id:           "verify",
					Type:         proto.ProvisioningStepType_PROVISIONING_STEP_TYPE_VERIFICATION,
					Title:        "Enter Verification Code",
					Description:  "Enter the code sent to your phone",
					Status:       proto.ProvisioningStepStatus_PROVISIONING_STEP_STATUS_PENDING,
					RequiresEdge: true,
					InputFields: []*proto.ProvisioningInputField{
						{
							Name:     "code",
							Label:    "Verification Code",
							Type:     "text",
							Required: true,
							Pattern:  "^[0-9]{6}$",
						},
					},
				},
			},
		},
	}
}

// provisioningSessionToProto converts an internal provisioning session to a proto message.
func provisioningSessionToProto(s *provisioningSession) *proto.ProvisioningSession {
	ps := &proto.ProvisioningSession{
		Id:               s.ID,
		ChannelType:      s.ChannelType,
		Status:           s.Status,
		Steps:            s.Steps,
		CurrentStepIndex: int32(s.CurrentStepIndex),
		Error:            s.Error,
		EdgeId:           s.EdgeID,
		CreatedAt:        timestamppb.New(s.CreatedAt),
		UpdatedAt:        timestamppb.New(s.UpdatedAt),
		ExpiresAt:        timestamppb.New(s.ExpiresAt),
		Data:             s.Data,
	}

	if s.CurrentStepIndex < len(s.Steps) {
		ps.CurrentStep = s.Steps[s.CurrentStepIndex]
	}

	return ps
}
