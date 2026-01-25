# MCP Evolution Design

## Overview

This document addresses issue #114: MCP evolution with hot reload, capability negotiation, and tool composition.

## Goals

1. Support **hot reload** of MCP server configs without restarting the gateway.
2. Expose **capability metadata** across connected MCP servers.
3. Prepare for **tool composition** across multiple MCP sources.

## Non-Goals

- Full MCP federation or multi-hop tool routing (future).
- Complex per-tool policy enforcement (future; see tool policy work).

---

## 1) Hot Reload

### Proposal
Add `Manager.Reload(ctx, cfg)` to:
- Disconnect removed servers
- Connect newly added `auto_start` servers
- Update internal config

### Constraints
- Initial implementation does not attempt to diff config changes for existing servers.
- Reconnect on config changes can be added later.

---

## 2) Capability Negotiation

### Proposal
Expose a simple capability summary per server:
- tools available
- resources
- prompts
- supported features

This will be used by extension unification and policy layers.

---

## 3) Tool Composition

### Proposal
Introduce a tool composition layer that can:
- Merge tools with same name across servers
- Provide namespacing and conflict resolution
- Surface metadata about the source server

---

## Rollout

1. Add reload support + update CLI to trigger.
2. Add capability summary APIs.
3. Implement tool composition rules and conflict resolution.
