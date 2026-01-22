package gateway

import (
	"fmt"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/haasonsaas/nexus/pkg/models"
	proto "github.com/haasonsaas/nexus/pkg/proto"
)

func channelFromProto(channel proto.ChannelType) models.ChannelType {
	switch channel {
	case proto.ChannelType_CHANNEL_TYPE_TELEGRAM:
		return models.ChannelTelegram
	case proto.ChannelType_CHANNEL_TYPE_DISCORD:
		return models.ChannelDiscord
	case proto.ChannelType_CHANNEL_TYPE_SLACK:
		return models.ChannelSlack
	case proto.ChannelType_CHANNEL_TYPE_API:
		return models.ChannelAPI
	default:
		return models.ChannelAPI
	}
}

func channelToProto(channel models.ChannelType) proto.ChannelType {
	switch channel {
	case models.ChannelTelegram:
		return proto.ChannelType_CHANNEL_TYPE_TELEGRAM
	case models.ChannelDiscord:
		return proto.ChannelType_CHANNEL_TYPE_DISCORD
	case models.ChannelSlack:
		return proto.ChannelType_CHANNEL_TYPE_SLACK
	case models.ChannelAPI:
		return proto.ChannelType_CHANNEL_TYPE_API
	default:
		return proto.ChannelType_CHANNEL_TYPE_UNSPECIFIED
	}
}

func directionToProto(direction models.Direction) proto.Direction {
	switch direction {
	case models.DirectionInbound:
		return proto.Direction_DIRECTION_INBOUND
	case models.DirectionOutbound:
		return proto.Direction_DIRECTION_OUTBOUND
	default:
		return proto.Direction_DIRECTION_UNSPECIFIED
	}
}

func roleToProto(role models.Role) proto.Role {
	switch role {
	case models.RoleUser:
		return proto.Role_ROLE_USER
	case models.RoleAssistant:
		return proto.Role_ROLE_ASSISTANT
	case models.RoleSystem:
		return proto.Role_ROLE_SYSTEM
	case models.RoleTool:
		return proto.Role_ROLE_TOOL
	default:
		return proto.Role_ROLE_UNSPECIFIED
	}
}

func connectionStatusToProto(status models.ConnectionStatus) proto.ConnectionStatus {
	switch status {
	case models.ConnectionStatusConnected:
		return proto.ConnectionStatus_CONNECTION_STATUS_CONNECTED
	case models.ConnectionStatusDisconnected:
		return proto.ConnectionStatus_CONNECTION_STATUS_DISCONNECTED
	case models.ConnectionStatusError:
		return proto.ConnectionStatus_CONNECTION_STATUS_ERROR
	case models.ConnectionStatusConnecting:
		return proto.ConnectionStatus_CONNECTION_STATUS_CONNECTING
	default:
		return proto.ConnectionStatus_CONNECTION_STATUS_UNSPECIFIED
	}
}

func metadataToProto(metadata map[string]any) map[string]string {
	if metadata == nil {
		return nil
	}
	out := make(map[string]string, len(metadata))
	for key, value := range metadata {
		out[key] = fmt.Sprint(value)
	}
	return out
}

func metadataFromProto(metadata map[string]string) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]any, len(metadata))
	for key, value := range metadata {
		out[key] = value
	}
	return out
}

func connectionToProto(conn *models.ChannelConnection) *proto.ChannelConnection {
	if conn == nil {
		return nil
	}
	config := map[string]string{}
	for k, v := range conn.Config {
		config[k] = fmt.Sprint(v)
	}
	return &proto.ChannelConnection{
		Id:             conn.ID,
		UserId:         conn.UserID,
		ChannelType:    channelToProto(conn.ChannelType),
		ChannelId:      conn.ChannelID,
		Status:         connectionStatusToProto(conn.Status),
		Config:         config,
		ConnectedAt:    timestampToProto(conn.ConnectedAt),
		LastActivityAt: timestampToProto(conn.LastActivityAt),
	}
}

func timestampToProto(ts time.Time) *timestamppb.Timestamp {
	if ts.IsZero() {
		return nil
	}
	return timestamppb.New(ts)
}

func attachmentsFromProto(attachments []*proto.Attachment) []models.Attachment {
	if len(attachments) == 0 {
		return nil
	}
	out := make([]models.Attachment, 0, len(attachments))
	for _, att := range attachments {
		if att == nil {
			continue
		}
		out = append(out, models.Attachment{
			ID:       att.Id,
			Type:     att.Type,
			URL:      att.Url,
			Filename: att.Filename,
			MimeType: att.MimeType,
			Size:     att.Size,
		})
	}
	return out
}

func toolResultsToProto(results []models.ToolResult) []*proto.ToolResult {
	if len(results) == 0 {
		return nil
	}
	out := make([]*proto.ToolResult, 0, len(results))
	for _, result := range results {
		out = append(out, &proto.ToolResult{
			ToolCallId: result.ToolCallID,
			Content:    result.Content,
			IsError:    result.IsError,
		})
	}
	return out
}

func messageToProto(msg *models.Message) *proto.Message {
	if msg == nil {
		return nil
	}
	out := &proto.Message{
		Id:          msg.ID,
		SessionId:   msg.SessionID,
		Channel:     channelToProto(msg.Channel),
		ChannelId:   msg.ChannelID,
		Direction:   directionToProto(msg.Direction),
		Role:        roleToProto(msg.Role),
		Content:     msg.Content,
		Metadata:    metadataToProto(msg.Metadata),
		CreatedAt:   timestampToProto(msg.CreatedAt),
		ToolResults: toolResultsToProto(msg.ToolResults),
	}
	if len(msg.Attachments) > 0 {
		attachments := make([]*proto.Attachment, 0, len(msg.Attachments))
		for _, att := range msg.Attachments {
			attachments = append(attachments, &proto.Attachment{
				Id:       att.ID,
				Type:     att.Type,
				Url:      att.URL,
				Filename: att.Filename,
				MimeType: att.MimeType,
				Size:     att.Size,
			})
		}
		out.Attachments = attachments
	}
	return out
}

func sessionToProto(session *models.Session) *proto.Session {
	if session == nil {
		return nil
	}
	return &proto.Session{
		Id:        session.ID,
		AgentId:   session.AgentID,
		Channel:   channelToProto(session.Channel),
		ChannelId: session.ChannelID,
		Key:       session.Key,
		Title:     session.Title,
		Metadata:  metadataToProto(session.Metadata),
		CreatedAt: timestampToProto(session.CreatedAt),
		UpdatedAt: timestampToProto(session.UpdatedAt),
	}
}
