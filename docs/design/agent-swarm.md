# Agent Swarm

## Summary
Parallel multi-agent collaboration for complex tasks.

## Goals
- Orchestrate multiple agents with a coordinator
- Support task decomposition and result aggregation
- Provide shared memory and context between agents

## Non-goals
- Automatic auto-scaling of swarm nodes

## Proposed Design
- Define swarm job with subtask graph
- Use a coordinator agent to assign and merge results
- Store intermediate results in shared memory

## Open Questions
- How to prevent context leakage between unrelated subtasks?
