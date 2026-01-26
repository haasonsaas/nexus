# Channel-Native Features

## Summary
Use channel-native affordances instead of LCD responses.

## Goals
- Expose channel capabilities (threads, reactions, files, edits)
- Prefer native formatting per channel
- Make tool outputs aware of channel-specific features

## Non-goals
- Build new UI surfaces for every channel

## Proposed Design
- Introduce a capability map per adapter
- Extend message model with channel-native hints
- Provide adapters with helpers to format native payloads

## Open Questions
- How to negotiate capabilities for multi-channel responses?
