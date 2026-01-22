# Clawdbot Deep Dive (2026-01-22)

## Scope & Sources
Reviewed local Clawdbot reference repo (`/home/developer/clawdbot-reference`). Focused on configuration schema, plugin validation, tool policy, sandboxing, security audit, and agent state isolation.

Key files reviewed:
- `src/config/zod-schema.ts`
- `src/config/zod-schema.agent-runtime.ts`
- `src/config/validation.ts`
- `src/config/agent-dirs.ts`
- `src/plugins/manifest-registry.ts`
- `src/security/audit.ts`
- `src/auto-reply/status.ts`

## High-Impact Findings
### 1) Config validation is strict and layered
- Uses Zod schemas with `.strict()` and explicit defaults to reject unknown keys early.
- Validation is two-stage: base schema validation, then plugin-aware validation.
- Adds extra validation for multi-agent collisions (duplicate `agentDir`).

Why it matters for Nexus:
- Our config validation is strict, but we do not currently validate agentDir collisions or run plugin schema validation as a second pass.

### 2) Plugin manifests are first-class for validation
- Plugin discovery builds a registry of manifests with cached schema keys (mtime-based).
- Validation enforces:
  - plugin IDs must exist in registry (entries/allow/deny/slots)
  - channel IDs must be known, including ones registered by plugin manifests
  - plugin config must conform to JSON schema (even if disabled but config exists)
  - missing schema is an error if config is present or plugin enabled

Why it matters for Nexus:
- We have manifest discovery + validation, but can tighten “missing schema” and unknown channel id checks during config validation, plus add a short-lived manifest cache.

### 3) Per-agent sandbox + tool policy is explicit and deep
- Agent-level override of sandbox fields with clear precedence.
- Per-agent tool policy can override global policy, including:
  - allow/deny
  - provider-specific allow/deny
  - exec policy (host, security, ask, timeouts)
  - elevated allowFrom by provider

Why it matters for Nexus:
- We have global tool policy and approvals, but per-agent tool policy and provider-level allow/deny are still missing.

### 4) “Elevated” execution is a runtime state with allowFrom gating
- Users can toggle elevated mode with directives (e.g., `/elevated on`).
- Elevated execution is only allowed if sender matches allowFrom allowlists.
- Config supports explicit allowFrom lists per provider/channel.

Why it matters for Nexus:
- We have approvals + policy but no per-channel sender allowlists for elevated modes.
- This is a key safety gap for multi-channel deployments.

### 5) Agent state isolation is enforced with agentDir uniqueness
- `agentDir` defaults to per-agent state dir, but can be overridden.
- Duplicate `agentDir` across agents is rejected because it causes auth/session collisions.

Why it matters for Nexus:
- Nexus multi-agent should enforce unique state directories; we currently do not.

### 6) Security audit is deep and prescriptive
- Checks config file and state directory permissions (world/group readable/writable).
- Flags insecure gateway binding without auth.
- Performs channel policy audits (e.g., open DMs, missing allowlists).

Why it matters for Nexus:
- Our doctor audits focus on service/systemd/ports and channel health; we should add filesystem + auth-safety audits.

### 7) Status surfaces operational context
- Status output includes runtime mode (direct vs sandbox), compaction counts, and queue state.
- Pulls token usage from session logs when prompt sizes exceed cached meta.

Why it matters for Nexus:
- We can expand `nexus status` to expose runtime mode, compactions, queue depth, and tool gating state.

## Concrete Adoption Candidates for Nexus
1) **AgentDir collision detection**
   - Add validation in config parsing for duplicate per-agent state paths.

2) **Plugin-aware config validation pass**
   - Enforce manifest existence + schema validity.
   - Treat missing schema as error when config present or plugin enabled.

3) **Per-agent tool policy overrides**
   - Allow per-agent allow/deny + provider-specific policies.
   - Add exec policy overrides per agent (host/security/ask/timeouts).

4) **Elevated execution allowFrom gating**
   - Add per-channel sender allowlists for elevated exec/tool gating.
   - Add runtime state toggle with safe defaults (off in groups unless mention).

5) **Security audit extensions**
   - File permission checks for config + state dir.
   - Gateway bind/auth risk checks.
   - Channel allowlist sanity checks.

6) **Plugin manifest registry cache**
   - Short TTL cache keyed by workspace + load paths; mtime‑based schema cache key.

## Suggested Next Work (if prioritized)
- Implement agentDir collision detection + tests.
- Add plugin-aware config validation pass + schema enforcement.
- Add per-agent tool policy with precedence rules (match Clawdbot schema semantics).
- Extend doctor audits for filesystem perms and gateway auth.

## Notes
- Clawdbot’s test suite is exhaustive around config semantics. Emulate with targeted unit tests for each policy precedence rule.
- Many safety behaviors default to deny/allowlist in group contexts; worth aligning Nexus defaults for public deployments.
## Updates
- 2026-01-22: Elevated allowFrom gating + approval policy wiring landed in Nexus (see `docs/shortcomings-plan.md`).
