# Nexus Open Issues Plan (2026-01-25)

## Context
All open issues as of 2026-01-25 should receive design docs and follow-on implementation. This plan maps issues to design docs and groups related work to reduce duplication while keeping scope traceable.

## Goals
1) Produce design docs for every open issue (mapped below).
2) Implement changes in dependency order with tests and docs updates.
3) Track status across design + execution.

## Issue Inventory
- #115 Deep: Agentic Loop State Machine - Checkpointing, Parallelism & Recovery
- #114 Deep: MCP Evolution - Hot Reload, Capability Negotiation & Tool Composition
- #113 Deep: Steering System Evolution - Conditional Injection & Observability
- #112 Deep: Continuous Security Posture - Real-Time Monitoring & Auto-Remediation
- #111 Deep: VM Pool Lifecycle - Snapshots, Migration & Auto-Scaling
- #110 Deep: Canvas as Agent Workspace - Visual Reasoning & Collaboration
- #109 Deep: Skill Composition - Dependencies, Inheritance & Marketplace
- #108 Deep: Multi-Agent Shared Memory & Collective Intelligence
- #107 Deep: Attention Feed as Active Agent Input
- #106 Deep: Edge Mesh Architecture - Distributed Tool Execution
- #105 Deep: Intelligent Provider Routing - Cost, Quality & Predictive Health
- #104 Deep: Tape System Evolution - Diff-Based Replay & Regression Testing
- #103 Design: Structured Fact Extraction (Beyond Summarization)
- #102 Design: Horizontal Scaling Architecture
- #101 Design: Embrace Channel-Native Features (Beyond LCD)
- #100 Design: Hierarchical Memory Model
- #99 Design: User Identity Layer Above Sessions
- #98 Design: Unify Extension Mechanisms (MCP vs Plugins vs Skills)
- #97 Plugin sandboxing: isolate third-party plugin execution
- #96 RAG evaluation pipeline: measure retrieval quality
- #95 Prompt A/B testing: experiment with different prompts and models
- #94 Edge node mesh: capability-based distributed tool execution
- #93 Conversation analytics dashboard: insights from chat data
- #92 Agent swarm: parallel multi-agent collaboration on complex tasks
- #91 Skill-provided tools: allow skills to register JSON Schema tools
- #90 Agent memory consolidation: background job for long-term memory
- #89 DRY: Extract BaseHealthAdapter for common health/metrics tracking
- #88 DRY: Unified tool schema with provider-specific converters
- #87 DRY: Extract StreamManager for buffered streaming responses
- #86 DRY: Extract ReconnectingAdapter wrapper for channels
- #85 DRY: Extract BaseProvider for common LLM provider logic
- #84 DRY: Extract common patterns from providers and adapters
- #83 Conversation forking: parallel exploration
- #82 Pre-built RAG knowledge packs
- #81 Home Assistant conversation agent integration
- #80 Edge mesh: multi-node task routing
- #79 OpenTelemetry tracing and cost tracking
- #78 Local TTS for full voice loop
- #77 Auto-detect and prefer local Ollama instances
- #76 Smart LLM routing based on query complexity

## Design Doc Mapping
- `docs/design/agent-loop-state-machine.md` (#115)
- `docs/design/mcp-evolution.md` (#114)
- `docs/design/steering-system.md` (#113)
- `docs/design/security-posture.md` (#112)
- `docs/design/vm-pool-lifecycle.md` (#111)
- `docs/design/canvas-workspace.md` (#110)
- `docs/design/skills-composition.md` (#109, #91)
- `docs/design/multi-agent-memory.md` (#108, #107, #100, #90)
- `docs/design/edge-mesh.md` (#106, #94, #80)
- `docs/design/provider-routing.md` (#105, #76, #77)
- `docs/design/tape-system-evolution.md` (#104)
- `docs/design/structured-fact-extraction.md` (#103)
- `docs/design/horizontal-scaling.md` (#102)
- `docs/design/channel-native-features.md` (#101)
- `docs/design/identity-layer.md` (#99)
- `docs/design/extension-unification.md` (#98)
- `docs/design/plugin-sandboxing.md` (#97)
- `docs/design/rag-evaluation.md` (#96)
- `docs/design/prompt-experiments.md` (#95)
- `docs/design/analytics-dashboard.md` (#93)
- `docs/design/agent-swarm.md` (#92)
- `docs/design/adapter-refactors.md` (#89, #88, #87, #86, #85, #84)
- `docs/design/conversation-forking.md` (#83)
- `docs/design/knowledge-packs.md` (#82)
- `docs/design/home-assistant-integration.md` (#81)
- `docs/design/observability-tracing.md` (#79)
- `docs/design/local-tts.md` (#78)

## Execution Order (Draft)
1) Adapter/provider refactors (#84-#89) to unlock shared primitives.
2) Provider routing + local discovery (#105/#76/#77).
3) RAG evaluation + knowledge packs (#96/#82).
4) Memory architecture (hierarchical + consolidation + shared memory + attention feed).
5) Extensions (skills composition + unified extension model + MCP evolution).
6) Edge mesh + horizontal scaling + VM pool lifecycle.
7) Security posture + observability.
8) Channel-native features + analytics dashboard + Home Assistant integration.
9) Agent loop state machine + agent swarm + conversation forking + tape system evolution.
10) Local TTS.

## Status
- Design docs: provider routing, RAG evaluation, knowledge packs, prompt experiments, skills composition, adapter refactors, multi-agent memory, extension unification, MCP evolution, plugin sandboxing, edge mesh, horizontal scaling, VM pool lifecycle, security posture, observability tracing, agent loop state machine, steering system, canvas workspace, tape system evolution, structured fact extraction, channel-native features, identity layer, analytics dashboard, agent swarm, conversation forking, home assistant integration, local TTS drafted.
- Implementation: RAG eval phase 2 (LLM judge) complete; provider routing + Ollama provider + discovery complete; knowledge packs CLI complete; prompt experiments scaffolding complete; skill tool scaffolding complete; adapter refactors in progress (BaseHealthAdapter + Reconnector + StreamManager + tool converters + BaseProvider, migrated personal/email/telegram/nostr/mattermost/nextcloudtalk/slack/discord/teams/matrix + OpenRouter; Anthropic/Gemini tool converters added); structured fact extraction tool (regex-based) in progress; hierarchical memory search + attention feed injection + consolidation worker in progress; steering system conditional injection + trace complete; extensions list CLI complete; MCP reload helper complete; conversation forking CLI (branch list/fork/tree/merge/compare/history) in progress; identity session scoping (DM key builder + gRPC/proactive routing) in progress; edge mesh + horizontal scaling + VM pool lifecycle implementation in progress; security posture + tracing implementation in progress.
