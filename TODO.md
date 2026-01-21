# Nexus TODO

This document tracks planned improvements, technical debt, and architectural enhancements for the Nexus project. Items are prioritized by severity and organized by component.

---

## Critical (Fix Immediately)

### Gateway Implementation
- [ ] **Implement message handling in gateway** - `internal/gateway/server.go:114-117`
  - Currently stubbed with TODO comments
  - Need to: Route to session → Run agent → Send response
  - Blocks: Core functionality is non-functional

- [ ] **Add authentication interceptors** - `internal/gateway/server.go:33,37`
  - gRPC endpoints have no authentication
  - Add unary and stream auth interceptors
  - Consider: API key, JWT, or mTLS options

### Session Management
- [ ] **Implement session persistence** - `internal/sessions/`
  - Sessions stored in CockroachDB but gateway doesn't use them
  - No session lifecycle management
  - Tool results not persisted
  - Impact: Stateless system, no conversation continuity

---

## High Priority

### Error Handling Improvements
- [x] ~~Add structured ProviderError with failover classification~~ ✅ Done
- [ ] **Integrate ProviderError into Anthropic provider** - `internal/agent/providers/anthropic.go`
  - Replace `fmt.Errorf` calls with `NewProviderError()`
  - Add status code extraction from API responses
  - Enable intelligent retry logic

- [ ] **Integrate ProviderError into OpenAI provider** - `internal/agent/providers/openai.go`
  - Same as Anthropic integration
  - Handle OpenAI-specific error codes

### Concurrency Issues
- [x] ~~Fix goroutine leak in AggregateMessages~~ ✅ Done
- [x] ~~Add mutex to ToolRegistry~~ ✅ Done
- [x] ~~Fix MultiRateLimiter.Reset() deadlock potential~~ ✅ Done

- [ ] **Add WaitGroup tracking in gateway** - `internal/gateway/server.go:65,102`
  - `processMessages` goroutine not tracked
  - `handleMessage` goroutines not tracked
  - Risk: Ungraceful shutdown, goroutines still running when Stop() completes

- [ ] **Buffer the Runtime.Process channel** - `internal/agent/runtime.go:405-478`
  - Currently unbuffered, risks goroutine hang if caller doesn't consume
  - Add small buffer (e.g., 10) for safety

### Code Quality
- [x] ~~Remove dead code in DuckDuckGo search~~ ✅ Done
- [x] ~~Update deprecated Playwright APIs~~ ✅ Done

- [ ] **Fix JSON marshal error handling** - `internal/tools/websearch/search.go:167`
  - `schemaBytes, _ := json.Marshal(schema)` ignores error
  - Low risk but should handle properly

- [ ] **Add nil check in convertTools** - `internal/agent/providers/anthropic.go:782-790`
  - `toolParam` could be nil, not just `OfTool`
  - Risk: Nil pointer dereference

---

## Medium Priority

### Architecture Improvements (Inspired by Clawdbot)

#### Granular Adapter Interfaces
- [ ] **Split monolithic Adapter interface into specialized adapters**
  - Current: Single `Adapter` interface with 8 methods
  - Target: Multiple focused interfaces like clawdbot:
    ```go
    type ConfigAdapter interface {
        ListAccountIDs(cfg *Config) []string
        ResolveAccount(cfg *Config, accountID string) (Account, error)
    }

    type OutboundAdapter interface {
        SendText(ctx context.Context, msg *Message) error
        SendMedia(ctx context.Context, msg *Message) error
    }

    type SecurityAdapter interface {
        CheckAllowlist(userID string) bool
        GetDMPolicy() DMPolicy
    }

    type StatusAdapter interface {
        HealthCheck(ctx context.Context) HealthStatus
        GetMetrics() MetricsSnapshot
    }
    ```
  - Benefits: Easier testing, clearer contracts, optional capabilities

#### Plugin System
- [ ] **Implement plugin registry with lazy loading**
  - Manifest-based plugin discovery
  - Instance caching to prevent duplicate loads
  - Runtime registration for dynamic plugins
  - Reference: clawdbot `/src/plugins/loader.ts`

- [ ] **Add plugin SDK package**
  - Export stable public API for plugin authors
  - Versioned interfaces
  - Documentation and examples

#### Configuration System
- [ ] **Add schema-driven config validation**
  - JSON Schema generation from Go structs
  - Separate validation from struct definition
  - UI hints for config editors
  - Reference: clawdbot config system with Zod schemas

- [ ] **Implement config defaults application**
  - `applyModelDefaults()`
  - `applySessionDefaults()`
  - `applyRateLimitDefaults()`
  - Applied post-parsing, pre-validation

### Performance Optimizations
- [ ] **Fix LatencyHistogram ring buffer** - `internal/channels/metrics.go:157-167`
  - Current: O(n) array slicing per sample
  - Target: Use circular buffer for O(1) operations
  ```go
  type LatencyHistogram struct {
      samples []time.Duration
      head    int
      count   int
      max     int
  }
  ```

### Testing Improvements
- [ ] **Add integration tests for gateway**
  - Currently no tests for message handling flow
  - Need mock adapters, mock providers

- [ ] **Add context cancellation tests**
  - Test cleanup in nested goroutines
  - Verify no leaks on context cancel

- [ ] **Improve example tests**
  - Current: Results ignored (`_ = json.Unmarshal(...)`)
  - Target: Actually verify behavior

---

## Low Priority

### Code Quality
- [ ] **Standardize error wrapping**
  - Some use `fmt.Errorf("... %w", err)`
  - Others use `fmt.Sprintf("... %v", err)`
  - Pick one pattern and apply consistently

- [ ] **Add constants for magic numbers**
  - Buffer sizes: `make(chan *models.Message, 100)`
  - Rate limits: `RateLimit: 5`
  - Timeouts: `30 * time.Second`
  - Create `internal/constants/` package

- [ ] **Remove commented-out code**
  - Scan for `// TODO` with dead code
  - Either implement or remove

### Documentation
- [ ] **Add architecture diagram**
  - Show component relationships
  - Data flow through system

- [ ] **Document plugin development**
  - How to create a new channel adapter
  - How to create a new tool
  - How to create a new provider

### Security
- [ ] **Review API key handling**
  - Keys stored in memory, exposed in dumps
  - Consider: Secure memory, key rotation

- [ ] **Add rate limiting to gateway**
  - Per-client request limits
  - Prevent abuse

---

## Completed ✅

### January 2026
- [x] Add comprehensive GoDoc documentation throughout codebase
- [x] Fix golangci-lint issues across channel adapters
- [x] Fix golangci-lint issues in observability package
- [x] Update deprecated Playwright APIs to Locator-based methods
- [x] Remove unused fields from Gateway Server struct
- [x] Fix errcheck issues in test files
- [x] Fix goroutine leak in `AggregateMessages` (added WaitGroup)
- [x] Add mutex protection to `ToolRegistry`
- [x] Fix deadlock potential in `MultiRateLimiter.Reset()`
- [x] Remove dead code in DuckDuckGo search
- [x] Add structured `ProviderError` with failover classification
- [x] Add `FailoverReason` enum for error categorization
- [x] Add `ClassifyError()` for automatic error classification
- [x] Add `IsRetryable()` and `ShouldFailover()` helpers

---

## Reference: Clawdbot Patterns to Study

These files in the clawdbot codebase demonstrate patterns worth adopting:

| File | Pattern |
|------|---------|
| `/src/channels/plugins/types.ts` | Granular adapter interfaces |
| `/src/channels/plugins/types.adapters.ts` | 14 specialized adapter types |
| `/src/plugins/loader.ts` | Plugin discovery & lazy loading |
| `/src/plugins/runtime.ts` | Global registry with Symbol.for() |
| `/src/agents/failover-error.ts` | Structured error classification |
| `/src/infra/errors.ts` | Error extraction utilities |
| `/src/config/io.ts` | Config parsing with backup rotation |
| `/src/config/defaults.ts` | Defaults application pattern |
| `extensions/discord/src/channel.ts` | Concrete adapter implementation |

---

## Contributing

When working on items in this TODO:
1. Create a branch: `fix/issue-description` or `feat/feature-name`
2. Reference the TODO item in your commit message
3. Update this file to mark items as completed
4. Add tests for any new functionality
5. Ensure `golangci-lint run ./...` passes
6. Ensure `go test ./...` passes

---

*Last updated: January 21, 2026*
