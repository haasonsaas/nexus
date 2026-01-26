# Home Assistant Integration

## Summary
Integrate Nexus with Home Assistant conversations and services.

## Goals
- Provide a Home Assistant conversation agent
- Enable entity control and event subscriptions
- Support long-running automation tasks

## Non-goals
- Custom HA UI components in the first phase

## Proposed Design
- Implement a Home Assistant webhook adapter
- Map HA intents to tool calls
- Add config for HA base URL and token

## Open Questions
- Should we support both REST and WebSocket integrations?
