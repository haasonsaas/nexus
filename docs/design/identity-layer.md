# Identity Layer

## Summary
User identity above sessions with cross-channel mapping.

## Goals
- Map multiple channel identities to a single user identity
- Provide stable identity IDs for personalization and memory
- Support privacy controls and opt-out

## Non-goals
- Full IAM system or SSO provider integration

## Proposed Design
- Introduce Identity records with linked channel accounts
- Resolve identity during session creation and message intake
- Store consent and retention policy per identity

## Open Questions
- What is the minimal identity schema to start with?
