# Home Assistant Integration

## Summary
Integrate Nexus with Home Assistant conversations and services.

## Goals
- Provide a Home Assistant conversation agent via a simple REST endpoint
- Enable entity control via Home Assistant REST API tools
- Support long-running automation tasks (future)

## Non-goals
- Custom HA UI components in the first phase
- Full HA event subscription / websocket support (future)

## Proposed Design
- Add `channels.homeassistant` config for HA base URL and token
- Expose an authenticated REST endpoint for HA conversations
- Register HA tools for state inspection and service calls

## Open Questions
- Should we support both REST and WebSocket integrations?

## Configuration

```yaml
channels:
  homeassistant:
    enabled: true
    base_url: http://homeassistant:8123
    token: ${HOME_ASSISTANT_TOKEN}
    timeout: 10s
```

## Conversation Endpoint

`POST /api/v1/ha/conversation` (requires `X-API-Key` or `Authorization: Bearer <jwt>`)

Request body:
```json
{
  "text": "Turn off the kitchen lights",
  "conversation_id": "optional-stable-id",
  "session_id": "optional-existing-session",
  "user_id": "optional-subject-id"
}
```

Response body:
```json
{
  "response": "Done, kitchen lights are off.",
  "session_id": "…",
  "message_id": "…"
}
```

## Tools

- `ha_call_service` — call `domain/service` with `service_data`
- `ha_get_state` — get a single entity state
- `ha_list_entities` — list entity summaries (optional domain filter)
