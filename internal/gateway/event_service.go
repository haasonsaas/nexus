// Package gateway provides the main Nexus gateway server.
//
// event_service.go implements the EventService gRPC handlers.
package gateway

import (
	"context"
	"strings"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/haasonsaas/nexus/internal/observability"
	proto "github.com/haasonsaas/nexus/pkg/proto"
)

// eventService implements the proto.EventServiceServer interface.
type eventService struct {
	proto.UnimplementedEventServiceServer
	server *Server
}

// newEventService creates a new event service handler.
func newEventService(s *Server) *eventService {
	return &eventService{server: s}
}

// GetEvents retrieves events by run ID or session ID.
func (e *eventService) GetEvents(ctx context.Context, req *proto.GetEventsRequest) (*proto.GetEventsResponse, error) {
	store := e.server.EventStore()
	if store == nil {
		return &proto.GetEventsResponse{Events: []*proto.TimelineEvent{}, TotalCount: 0}, nil
	}

	var events []*observability.Event
	var err error

	// Query by run ID or session ID
	if req.RunId != "" {
		events, err = store.GetByRunID(req.RunId)
	} else if req.SessionId != "" {
		events, err = store.GetBySessionID(req.SessionId)
	} else {
		// Return empty if no query specified
		return &proto.GetEventsResponse{Events: []*proto.TimelineEvent{}, TotalCount: 0}, nil
	}

	if err != nil {
		return nil, err
	}

	// Apply type filter if specified
	if req.TypeFilter != "" {
		filtered := make([]*observability.Event, 0, len(events))
		for _, ev := range events {
			if strings.Contains(string(ev.Type), req.TypeFilter) {
				filtered = append(filtered, ev)
			}
		}
		events = filtered
	}

	// Apply limit
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 100
	}
	if len(events) > limit {
		events = events[:limit]
	}

	// Convert to proto messages
	protoEvents := make([]*proto.TimelineEvent, 0, len(events))
	for _, ev := range events {
		protoEvents = append(protoEvents, eventToProto(ev))
	}

	return &proto.GetEventsResponse{
		Events:     protoEvents,
		TotalCount: clampNonNegativeIntToInt32(len(events)),
	}, nil
}

// GetTimeline retrieves a formatted timeline for a run.
func (e *eventService) GetTimeline(ctx context.Context, req *proto.GetTimelineRequest) (*proto.GetTimelineResponse, error) {
	store := e.server.EventStore()
	if store == nil {
		return &proto.GetTimelineResponse{Formatted: "No events found"}, nil
	}

	var events []*observability.Event
	var err error

	if req.RunId != "" {
		events, err = store.GetByRunID(req.RunId)
	} else if req.SessionId != "" {
		events, err = store.GetBySessionID(req.SessionId)
	} else {
		return &proto.GetTimelineResponse{Formatted: "No run_id or session_id specified"}, nil
	}

	if err != nil {
		return nil, err
	}

	// Build timeline
	timeline := observability.BuildTimeline(events)
	formatted := observability.FormatTimeline(timeline)

	// Convert events to proto
	protoEvents := make([]*proto.TimelineEvent, 0, len(events))
	for _, ev := range events {
		protoEvents = append(protoEvents, eventToProto(ev))
	}

	resp := &proto.GetTimelineResponse{
		RunId:      timeline.RunID,
		SessionId:  timeline.SessionID,
		Events:     protoEvents,
		Formatted:  formatted,
		DurationMs: timeline.Duration.Milliseconds(),
	}

	if !timeline.StartTime.IsZero() {
		resp.StartTime = timestamppb.New(timeline.StartTime)
	}
	if !timeline.EndTime.IsZero() {
		resp.EndTime = timestamppb.New(timeline.EndTime)
	}

	if timeline.Summary != nil {
		resp.Summary = &proto.TimelineSummary{
			TotalEvents:     clampNonNegativeIntToInt32(timeline.Summary.TotalEvents),
			ErrorCount:      clampNonNegativeIntToInt32(timeline.Summary.ErrorCount),
			ToolCalls:       clampNonNegativeIntToInt32(timeline.Summary.ToolCalls),
			LlmCalls:        clampNonNegativeIntToInt32(timeline.Summary.LLMCalls),
			EdgeEvents:      clampNonNegativeIntToInt32(timeline.Summary.EdgeEvents),
			TotalDurationMs: timeline.Summary.TotalDuration.Milliseconds(),
		}
	}

	return resp, nil
}

// eventToProto converts an observability Event to a proto TimelineEvent.
func eventToProto(ev *observability.Event) *proto.TimelineEvent {
	pe := &proto.TimelineEvent{
		Id:          ev.ID,
		Type:        string(ev.Type),
		Timestamp:   timestamppb.New(ev.Timestamp),
		RunId:       ev.RunID,
		SessionId:   ev.SessionID,
		ToolCallId:  ev.ToolCallID,
		EdgeId:      ev.EdgeID,
		Name:        ev.Name,
		Description: ev.Description,
		DurationMs:  ev.Duration.Milliseconds(),
		Error:       ev.Error,
	}

	// Convert data map to string map
	if ev.Data != nil {
		pe.Data = make(map[string]string, len(ev.Data))
		for k, v := range ev.Data {
			if v == nil {
				continue
			}
			switch val := v.(type) {
			case string:
				pe.Data[k] = val
			default:
				// Skip complex types for now - they can be JSON encoded if needed
			}
		}
	}

	return pe
}
