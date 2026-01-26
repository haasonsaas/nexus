# Steering System

## Summary
Conditional prompt injection and observability for steering policies.

## Goals
- Allow steering rules to inject prompts based on context
- Provide visibility into which rules fired and why
- Support per-agent, per-channel, and global steering scopes

## Non-goals
- Full policy language runtime (start with simple predicates)

## Proposed Design
- Define SteeringRule with conditions (role, channel, tags, time)
- Evaluate rules at prompt build time with an explain trace
- Attach steering metadata to responses for auditability

## Open Questions
- Where should steering traces be stored (audit log vs session metadata)?
