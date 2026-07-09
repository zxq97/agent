# AGENTS.md — 租车智能体 v2

## Project Overview

Conversational AI assistant for end-users of a car rental service. Built with Go 1.24+, using a responsibility-chain Pipeline architecture.

- **Branch**: `feat/agent-v2-pipeline`
- **LLM**: DeepSeek-chat (default), pure Go OpenAI-compatible client (no eino dependency)
- **Backend data**: tyche MCP (POI resolution) + rental-guide cluster (quotes + menu)
- **Service**: HTTP + SSE (`cmd/http/main.go`), with frontend page (`web/index.html`)

## Build & Run

```bash
# Build (do not commit binaries)
go build ./...

# Run locally
DEEPSEEK_API_KEY=sk-xxx go run ./cmd/http -env dev

# Test
go test ./...

# Lint/vet
go vet ./...
```

## Coding Rules

### Compiled Artifacts

**Never commit compiled binaries.** Use `go run` to run the project. Do not leave executables from `go build`. `.gitignore` already excludes `/http`, `/cli`, `/rental-agent`.

### Import Aliases

**Do not use import aliases** unless two packages with the same name are imported in the same file.

```go
// ✅ Correct
import (
    "github.com/zxq97/rental-agent/internal/types"
)

// ❌ Wrong (unnecessary alias)
import (
    mytypes "github.com/zxq97/rental-agent/internal/types"
)

// ✅ Exception: disambiguate two same-named packages
import (
    agenthttp "github.com/zxq97/rental-agent/internal/httphandler"
    "net/http"
)
```

### Documentation Changes

**All design/spec doc edits must be logged.** Append a line to Appendix A (evolution log) in `docs/technical-plan.md` with: date, what changed, why, and scope of impact. When updating a Phase spec, annotate the changed section with `[演进 YYYY-MM-DD]` and a reason. Silent edits to design docs are forbidden.

### Other Rules

- **ConversationState is defined once** in `internal/orchestration/state.go`; all sub-packages import it from there
- **Stage / Capability unified signature**: `Stage.Handle(ctx, *AgentContext) (Signal, error)`; `Capability.Run(ctx, CapabilityInput) (*CapabilityResult, error)`
- **Tool descriptions are for the LLM**: Chinese is fine; decide tool schema **must not include ID fields** (IDs are injected by Go from state)
- **Error handling**: errors inside tools are converted to `{is_error, user_msg, debug}`; `user_msg` must be human-readable
- **Tool results stored in history must be summarized**: store a refined summary, not raw tyche JSON
- **Logs must include structured fields**: LLM / tool / stage logs must carry `trace_id` / `session_id`

## Directory Structure

```
cmd/
  http/main.go          HTTP + SSE server entry point
  cli/main.go           Local debug CLI (kept but not primary)
internal/
  agent/                Pipeline + Stage + Decider + Capability + NeedState + FilterCode
  orchestration/        Single source of truth for ConversationState
  tools/                tyche MCP tool wrappers (Go-managed IDs) + allowlist
  tyche/                tyche MCP JSON-RPC client + guide storelist HTTP client
  llm/                  Provider factory + pure Go OpenAI-compatible client
  httphandler/          HTTP handler + SSE emitter + middleware
  session/              Session Store interface + MemoryStore
  config/               yaml + env config loader
  types/                Cross-module shared types (UserNeed / NeedDelta / SearchConstraints, etc.)
  prompt/               decide_system prompt + per-Capability prompts
conf/                   dev / pre / prod multi-env yaml configs
web/                    Single-page frontend application
docs/                   Technical plan + per-Phase specs
```

## Key Architecture

### Pipeline

```
DecideStage (LLM #1 streaming function-calling)
  → CapabilityStage (dispatch by Decision.Tool)
  → FinalizeStage (persist state, write history)
```

### UserNeed Lifecycle

The LLM outputs `need_delta` operations (`ADD` / `UPDATE` / `DELETE` / `NEGATE` / `DECAY`). Go manages the lifecycle:

- `TickNeeds` — natural decay each round
- `ApplyDelta` — apply delta operations
- `ApplyConflictDecay` — conflict decay (e.g. switching brand → decay old seat-count need)
- `FilterActiveNeeds` — filter out Dormant needs
- `StaticRecall` — map needs → filter_codes statically

### Data Sources

- **Pickup/return POI**: tyche MCP (`rental_search_locations` + `rental_resolve_poi`)
- **Quotes + menu**: rental-guide `/car/rental/guide/store/list/agent` (returns `menu_group` + `veh_rates`)
- Fallback to MCP `rental_search_quotes` when rental-guide is unavailable

### ID Security Rule

`context_id` / `reference_id` / `supplier` are injected by Go from state. The LLM never handles them. These fields must **not** appear in any tool schema exposed to the LLM.
