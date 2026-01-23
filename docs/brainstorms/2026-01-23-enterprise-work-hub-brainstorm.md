---
date: 2026-01-23
topic: enterprise-work-hub
---

# Enterprise Work Hub: Unified ServiceNow, Teams, Email + More

## What We're Building

Nexus as the **unified command layer** for enterprise knowledge workers. Instead of context-switching between ServiceNow, Teams, Email, Slack, and other tools, users interact with Nexus in natural language and it handles routing, actions, and synthesis across all systems.

Core value proposition: "I shouldn't have to open ServiceNow to approve a ticket, Teams to respond to a message, and Outlook to send a follow-up. I tell Nexus what I need, and it handles the plumbing."

Target: **Broader enterprise teams** (not just power users).

Capabilities:
- **Aggregation/summarization** - "What needs my attention today?"
- **Cross-system actions** - "Approve this ticket and message the requester"
- **Workflow automation** - "When X happens in ServiceNow, do Y in Teams"

## Why This Approach

### Options Considered

**Option A: Everything is a Channel**
Add Teams, Email, ServiceNow as channel adapters using existing patterns.
- Pro: Leverages existing architecture
- Con: ServiceNow isn't really "chat" - forces square peg into round hole

**Option B: New Connector Abstraction**
Build separate abstraction for structured systems (tickets, issues).
- Pro: Better semantic fit
- Con: More work, parallel system to channels

**Option C: Attention Service First**
Build the unified inbox layer before adding sources.
- Pro: Killer feature
- Con: Needs sources to pull from first

### Chosen Approach

**Start with Option A, design for Option B.**

- Teams and Email ARE conversational - they fit the channel model cleanly
- ServiceNow becomes: channel for notifications + **tools** for actions
- Gets value fast while learning what Connector abstraction should look like
- Attention Service becomes Phase 2 once we have sources to aggregate

## Key Decisions

### 1. Microsoft Teams as first enterprise channel
- Large enterprise footprint
- OAuth provisioning fits existing framework
- Both chat and channel messages
- Graph API is well-documented

### 2. ServiceNow as tools, not (just) a channel
- `servicenow_list_tickets` - Query tickets assigned to me
- `servicenow_approve` - Approve a ticket by ID
- `servicenow_comment` - Add comment to ticket
- `servicenow_transition` - Change ticket state
- Webhook channel for real-time notifications (optional)

### 3. Email via Microsoft Graph (initially)
- Unified with Teams auth (same OAuth)
- Read inbox, send emails, search
- IMAP/SMTP as fallback for non-O365

### 4. Enterprise foundations deferred but designed for
- Multi-tenancy: Session store supports org isolation (add later)
- SSO: Provisioning service supports OAuth (extend to SAML/OIDC)
- Audit: Hooks system can capture all actions (add compliance hook)

## Architecture Mapping

```
Existing Nexus                    Enterprise Extensions
─────────────────                 ─────────────────────
channels.Registry          →      + Teams, Outlook channels
tools.Tool interface       →      + ServiceNow tools
tasks.Scheduler            →      + "Check ServiceNow every 5 min" tasks
identity.Store             →      + Azure AD identity linking
provisioning.Service       →      + OAuth flows for M365
hooks.Registry             →      + Audit logging hook
```

## Open Questions

1. **ServiceNow auth model** - API key per user vs service account?
2. **Teams bot registration** - Azure Bot Service or direct Graph API?
3. **Email threading** - How to maintain conversation context across email chains?
4. **Rate limits** - Graph API throttling strategy?
5. **On-prem ServiceNow** - Edge daemon pattern for behind-firewall instances?

## Milestone Breakdown

### Milestone 4: Microsoft Teams Channel
- OAuth provisioning flow (Azure AD)
- Inbound messages (1:1 chat + channel mentions)
- Outbound messages (replies + proactive)
- Typing indicators + streaming

### Milestone 5: ServiceNow Integration
- ServiceNow tools (list, approve, comment, transition)
- Webhook receiver for real-time updates (optional)
- Tool policies for approval workflows

### Milestone 6: Email Channel
- Microsoft Graph email (read, send, search)
- Thread/conversation tracking
- Attachment handling

### Milestone 7: Unified Attention Layer
- Aggregate items from all sources
- Priority scoring
- "What needs my attention?" query
- Notification routing preferences

## Next Steps

→ `/workflows:plan` for Milestone 4 (Microsoft Teams Channel) implementation details
