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

### Configuration (YAML)
```yaml
steering:
  enabled: true
  rules:
    - id: "priority-accounts"
      name: "Priority account tone"
      prompt: "When responding to priority accounts, be extra concise and confirm next steps."
      priority: 10
      tags: ["priority"]
      channels: ["slack", "discord"]
```

### Matching Rules
- Rules match on: role, channel, agent, tags, message content substrings, metadata key/value pairs, and optional RFC3339 time windows.
- Unspecified conditions are treated as wildcards.
- Matching rules inject their `prompt` into the system prompt as a "Steering directives" section.

### Observability
- Matched rules are attached to outbound message metadata (`steering_rules`) with rule id/name/priority and match reasons.

## Open Questions
- Where should steering traces be stored (audit log vs session metadata)?
