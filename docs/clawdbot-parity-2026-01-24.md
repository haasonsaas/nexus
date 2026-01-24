# Clawdbot ↔ Nexus Parity Deep Dive (2026-01-24)

## Scope
- **Clawdbot ref:** `clawdbot@00fd57b8f` (2026-01-24 pull)
- **Nexus ref:** `nexus@5148052` (main)
- **Sources:** Clawdbot docs + tool inventory (`src/agents/tool-display.json`), extensions list, agent/tool policy, sandbox + config; Nexus codebase + docs.

## Executive Summary (How We Stack Up)
**Nexus strengths (ahead or unique):**
- **MCP bridge + tool aliasing** (MCP servers, resource/prompt bridges, policy alias mapping) are first-class.
- **RAG pipeline** (pgvector backend, upload/search/delete tools, chunking config) is more mature than Clawdbot’s memory plugins.
- **ServiceNow + reminder/task tools** are present; Clawdbot has no native equivalents.
- **Go runtime stability**: strong typed infra, CockroachDB backends, gRPC services.

**Clawdbot strengths (parity gaps for Nexus):**
- **Full tool surface** (exec/process/read/write/edit/apply_patch, web_fetch, message tooling, sessions tools, canvas tool, nodes tool, cron tool, gateway tool).
- **Config control plane** (JSON5, $include, config schema exposure, config apply/patch RPC, UI hints).
- **Provider breadth + model discovery** (Bedrock discovery, many providers, model selection/compat/fallback).
- **Plugin/extension ecosystem** (channels + auth + memory + diagnostics + voice + lobster, etc.).
- **Session safety utilities** (transcript repair, tool-result guard, write locks).

**Biggest parity blockers:**
1) **Core tool surface** in Nexus (missing read/write/edit/exec/process/apply_patch/web_fetch/canvas/message/sessions tools).
2) **Channel/plugin breadth** (missing Mattermost/Nextcloud/Nostr/Tlon/Zalo/BlueBubbles/etc.).
3) **Provider + auth profile depth** (model discovery, provider-specific auth flows, profiles/rotation).
4) **Config control plane** (schema/UI hints, config apply/patch, JSON5 + includes).

---

## Parity Matrix (Deep Dive)
Legend: ✅ parity, 🟡 partial, ❌ missing

### 1) Configuration + Control Plane
| Feature | Clawdbot | Nexus | Status | Gap / Notes | Priority |
|---|---|---|---|---|---|
| Strict config validation | Zod + plugin-aware | YAML strict + schema validation | ✅ | Nexus strict, but plugin-aware 2nd pass is lighter. | P1 |
| Config schema exposure (UI forms) | JSON Schema + UI hints | ❌ | No schema exposure to UIs. | P1 |
| Config apply/patch RPC | `config.apply`, `config.patch` | ❌ | Missing hot apply + restart. | P1 |
| JSON5 config | ✅ | ❌ (YAML) | Optional parity; might keep YAML. | P2 |
| `$include` for config | ✅ | ❌ | No includes; large configs harder to manage. | P1 |
| Per-agent config overlay | ✅ | 🟡 | Some per-agent settings via AGENTS.md; less control than Clawdbot. | P1 |
| Config doctor + repairs | ✅ | ✅ | Nexus doctor exists (audit + repairs). | P2 |

### 2) Tool Surface (Core Tools)
| Tool | Clawdbot | Nexus | Status | Gap / Notes | Priority |
|---|---|---|---|---|---|
| `exec` / `bash` | ✅ | ❌ | No host exec tool; only sandbox `execute_code`. | P0 |
| `process` | ✅ | ❌ | No background process tool. | P0 |
| `read` / `write` / `edit` | ✅ | ❌ | No FS tools. | P0 |
| `apply_patch` | ✅ | ❌ | No patch tool. | P0 |
| `web_fetch` | ✅ | ❌ | Missing; only `web_search`. | P0 |
| `browser` | ✅ | ✅ | Browser tool exists. | P1 |
| `canvas` | ✅ | ❌ | Host exists; no tool to drive it. | P1 |
| `nodes` | ✅ | 🟡 | Edge tools exist; missing unified nodes tool API. | P1 |
| `cron` | ✅ | 🟡 | Cron scheduler exists; no `cron` tool. | P1 |
| `gateway` tool | ✅ | ❌ | Missing restart/tooling. | P2 |
| `message` tool | ✅ | ❌ | Missing cross-channel message tool. | P1 |
| Session tools (`sessions_*`) | ✅ | 🟡 | Subagent tools exist; sessions list/history/send/status missing. | P1 |
| Memory tools (`memory_search`, `memory_get`) | ✅ | 🟡 | search exists, get missing. | P1 |

### 3) Tool Policy + Safety
| Feature | Clawdbot | Nexus | Status | Gap / Notes | Priority |
|---|---|---|---|---|---|
| Global allow/deny | ✅ | ✅ | Implemented. | P2 |
| Tool profiles + groups | ✅ | ✅ | Implemented but duplicate group definitions exist. | P2 |
| Provider-specific policies | ✅ | ✅ | Implemented. | P2 |
| Wildcard allow/deny | ✅ | 🟡 | MCP wildcards exist; core tool wildcards missing. | P0 |
| Per-agent tool policy | ✅ | 🟡 | Partial (runtime policy can be scoped); needs config parity. | P1 |
| Subagent tool policy | ✅ | ❌ | No default denylist for subagents. | P1 |
| Sandbox tool allowlists | ✅ | ❌ | Missing sandbox-specific tool policy layer. | P1 |
| Tool display metadata | ✅ | ❌ | No tool display registry for UI. | P2 |
| Tool result guard | ✅ | ❌ | Missing tool result safety gate. | P2 |

### 4) Sandbox + Execution
| Feature | Clawdbot | Nexus | Status | Gap / Notes | Priority |
|---|---|---|---|---|---|
| Sandbox modes (off/all/non-main) | ✅ | ✅ | Implemented. | P2 |
| Sandbox scopes (agent/session/shared) | ✅ | ✅ | Implemented. | P2 |
| Exec approvals | ✅ | ✅ | Implemented. | P2 |
| Host execution + allowFrom | ✅ | 🟡 | Elevated gating exists; no general exec tool yet. | P0 |
| Firecracker support | ❌ | ✅ | Nexus has Firecracker backend. | P3 |

### 5) Channels + Messaging Integrations
| Channel | Clawdbot | Nexus | Status | Notes | Priority |
|---|---|---|---|---|---|
| WhatsApp | ✅ | ✅ | parity | P2 |
| Telegram | ✅ | ✅ | parity | P2 |
| Discord | ✅ | ✅ | parity | P2 |
| Slack | ✅ | ✅ | parity | P2 |
| Signal | ✅ | ✅ | parity | P2 |
| Matrix | ✅ | ✅ | parity | P2 |
| iMessage | ✅ | ✅ | parity | P2 |
| Microsoft Teams | ✅ | 🟡 | `teams` exists; features unknown vs Clawdbot `msteams`. | P2 |
| Mattermost | ✅ | ❌ | Missing. | P1 |
| Nextcloud Talk | ✅ | ❌ | Missing. | P1 |
| Nostr | ✅ | ❌ | Missing. | P1 |
| Tlon (Urbit) | ✅ | ❌ | Missing. | P1 |
| Zalo | ✅ | ❌ | Missing. | P1 |
| ZaloUser | ✅ | ❌ | Missing. | P1 |
| BlueBubbles | ✅ | ❌ | Missing (alt iMessage). | P2 |
| Email | ❌ | ✅ | Nexus-only feature. | P3 |

### 6) Extensions / Plugins
| Extension | Clawdbot | Nexus | Status | Notes | Priority |
|---|---|---|---|---|---|
| Plugin install system | ✅ | ✅ | Nexus has plugin registry + validation. | P2 |
| diagnostics-otel | ✅ | ❌ | Missing OTel plugin parity. | P2 |
| memory-core / memory-lancedb | ✅ | ❌ | Missing plugin memory providers; Nexus has built-in memory + RAG. | P2 |
| voice-call | ✅ | ❌ | Missing. | P3 |
| lobster | ✅ | ❌ | Missing workflow runtime. | P2 |
| llm-task | ✅ | ❌ | Missing JSON-only task tool. | P1 |
| copilot-proxy | ✅ | ❌ | Missing provider auth proxy. | P2 |
| qwen portal auth | ✅ | ❌ | Missing. | P2 |
| google antigravity / gemini CLI auth | ✅ | ❌ | Missing. | P2 |

### 7) Providers + Model Management
| Feature | Clawdbot | Nexus | Status | Notes | Priority |
|---|---|---|---|---|---|
| Providers breadth | ✅ (many) | 🟡 (Anthropic, OpenAI, Google) | ❌ | Missing Bedrock/OpenRouter/etc. | P1 |
| Bedrock discovery | ✅ | ❌ | Missing. | P1 |
| Model selection/fallback | ✅ | 🟡 | Basic fallback exists; not full parity. | P1 |
| Auth profiles + rotation | ✅ | ❌ | Missing. | P1 |
| Model catalog persistence | ✅ | 🟡 | Minimal. | P2 |

### 8) Sessions, Memory, & Safety
| Feature | Clawdbot | Nexus | Status | Notes | Priority |
|---|---|---|---|---|---|
| Compaction | ✅ | ✅ | parity | P2 |
| Context pruning | ✅ | ✅ | parity (recently added) | P2 |
| Transcript repair | ✅ | ❌ | Missing. | P2 |
| Session write lock | ✅ | ❌ | Missing. | P2 |
| Session origin metadata | ✅ | ❌ | Missing (provider/from/to/accountId/threadId). | P1 |
| Memory search | ✅ | ✅ | parity | P2 |
| Memory get | ✅ | ❌ | Missing. | P1 |
| Memory daily logs | ✅ | ✅ | parity | P2 |

### 9) Observability + Diagnostics
| Feature | Clawdbot | Nexus | Status | Notes | Priority |
|---|---|---|---|---|---|
| OTEL diagnostics | ✅ | 🟡 | Observability exists; missing OTel plugin parity. | P2 |
| Security audit (file perms, bind risks) | ✅ | 🟡 | Some audits exist; missing filesystem + bind checks. | P2 |
| Status details | ✅ | 🟡 | Nexus status exists, lacks compaction + queue metrics. | P3 |

### 10) UI / Control Surfaces
| Feature | Clawdbot | Nexus | Status | Notes | Priority |
|---|---|---|---|---|---|
| Control UI | ✅ | ❌ | Missing; Nexus only exposes gRPC/HTTP. | P3 |
| Canvas tool + UI | ✅ | 🟡 | Canvas host exists, no tool API. | P1 |
| Node UI (mac app) | ✅ | ❌ | Missing. | P3 |

---

## “Bring Everything In” Plan (Phased)

### Phase 0 — Tool Policy & Core Glue (P0)
- Add wildcard matching for allow/deny in tool policy (match `*`, `web_*`, etc.).
- Add `web_fetch` tool with SSRF guard + max-char limits (parity baseline).
- Align tool naming aliases with Clawdbot (`web_fetch`, `web_search`, etc.).

### Phase 1 — Core Tool Surface (P1)
- Implement file tools: `read`, `write`, `edit`, `apply_patch`.
- Add `exec` + `process` tools with sandbox/approval gating.
- Add `message` tool to unify channel actions.
- Add sessions tools: `sessions_list`, `sessions_history`, `sessions_send`, `session_status`.
- Add `memory_get` tool.
- Add `canvas` tool to drive Canvas Host.
- Add `cron` tool around existing scheduler.

### Phase 2 — Config & Plugins (P1–P2)
- Add config `$include` support and JSON5 loader (optional dual-format).
- Add config schema endpoint + UI hints.
- Add `config.apply` / `config.patch` RPC.
- Port extension-style plugins: `llm-task`, `lobster`, `diagnostics-otel`.

### Phase 3 — Providers & Auth (P1–P2)
- Add Bedrock discovery + provider registry.
- Add OpenRouter/Azure/other provider clients.
- Add auth profiles + rotation and per-provider OAuth flows.

### Phase 4 — Channels (P1–P2)
- Implement missing channels: Mattermost, Nextcloud Talk, Nostr, Tlon, Zalo, ZaloUser, BlueBubbles.

### Phase 5 — Session Safety & UX (P2–P3)
- Add transcript repair + session write locks.
- Add session origin metadata.
- Extend status output with compaction + queue state.
- Add Control UI / node UI parity.

---

## Immediate Work in This Branch
- ✅ Wildcard matching for tool allow/deny.
- ✅ Baseline `web_fetch` tool (SSRF-safe, content extraction) and registration.
- ✅ Config + docs updated to surface `web_fetch`.

---

## Notes
- Some parity items (providers, auth profiles, UI, missing channels) require larger design decisions and should be staged to avoid destabilizing the gateway.
- Nexus already has components Clawdbot doesn’t; those should be preserved and potentially surfaced as optional tools (RAG, ServiceNow, MCP).
