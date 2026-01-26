# Agent Swarm

## Summary
Parallel multi-agent collaboration for complex tasks.

## Goals
- Execute multiple agents in parallel when dependencies allow.
- Support explicit dependency graphs (`depends_on`) and role hints (`swarm_role`).
- Provide a shared context channel for publishing intermediate results.

## Non-goals
- Automatic auto-scaling of swarm nodes

## Current State (Implemented scaffolding)

Code lives in `internal/multiagent/swarm.go`:

- `SwarmConfig` embedded in `MultiAgentConfig` as `swarm:`
- Per-agent fields in `AgentDefinition`:
  - `swarm_role` (gatherer/processor/synthesizer/validator)
  - `depends_on` (agent IDs)
  - `can_trigger` (agent IDs; reserved for future)
- `BuildDependencyGraph(agents)`:
  - Validates dependencies
  - Produces stage-ordered execution plan
  - Detects cycles
- `Swarm.Execute(...)`:
  - Executes dependency stages with bounded parallelism
  - Publishes outputs into a shared context (`InMemorySwarmContext`)

This is intentionally a library-layer primitive; wiring into the gateway/runtime is a follow-up.

## Configuration (YAML)

```yaml
swarm:
  enabled: true
  max_parallel_agents: 5
  shared_context:
    backend: memory
    ttl: 1h

agents:
  - id: researcher
    swarm_role: gatherer

  - id: analyst
    swarm_role: processor
    depends_on: [researcher]

  - id: writer
    swarm_role: synthesizer
    depends_on: [analyst]
```

## Next Steps

1) Integrate swarm execution into `Orchestrator.Process` behind config gating.
2) Add a coordinator agent that decomposes tasks and synthesizes results.
3) Expand shared context backends (Redis) and data model (keyed values + pubsub).

## Open Questions
- How to prevent context leakage between unrelated subtasks?
