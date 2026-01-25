# Prompt Experiments (A/B Testing) Design

## Overview

Provides a lightweight experiment framework to compare prompts or model variants. Addresses issue #95.

## Goals

1. Deterministic assignment per subject (session/user).
2. Support system prompt overrides and model selection.
3. Simple config-driven experiments.

## Config

```yaml
experiments:
  experiments:
    - id: system-prompt-v2
      description: "Test new concise prompt"
      status: active
      allocation: 20
      variants:
        - id: control
          weight: 50
          config:
            system_prompt: "You are a helpful assistant."
        - id: treatment
          weight: 50
          config:
            system_prompt: "You are concise and direct."
```

## Implementation

- `internal/experiments` handles deterministic variant assignment via hashing.
- `gateway.systemPromptForMessage` applies system prompt overrides.
- `processing` and `grpc_service` apply per-request model overrides.

## Future Work

- Persist assignments for analytics dashboards.
- Add metrics collection and results endpoints.
- Support provider overrides.
