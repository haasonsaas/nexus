# Extension Unification Design

## Overview

This document addresses issue #98: unifying MCP, plugins, and skills into a single extension surface so operators and agents can reason about what is installed/enabled.

## Goals

1. Provide a **single registry** view of configured extensions (skills, plugins, MCP).
2. Offer **CLI visibility** for operators.
3. Establish a foundation for future shared policies and governance.

## Non-Goals

- Full runtime conversion of all extension types into a single execution runtime.
- New permission/RBAC system (future).

---

## Proposed Architecture

### Unified Extension Model
Each extension is represented as:
- `id`
- `name`
- `kind` (`skill`, `plugin`, `mcp`)
- `status` (`enabled`, `disabled`, `eligible`, `configured`, etc.)
- `source` (path, registry, transport)

### Registry
Introduce an `extensions` package to aggregate:
- Skills from `skills.Manager`
- Plugins from config entries (and registry metadata when available)
- MCP server configs

### CLI
Add `nexus extensions list` to show a unified view for operators.

---

## Rollout

1. Add extension registry + CLI list.
2. Expand to runtime policies (tool allowlists, sandboxing).
3. Extend with capability negotiation (MCP) and provenance metadata.
