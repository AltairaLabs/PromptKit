# PromptArena Web UI

**Status: Implemented** — PRs #801 (backend), #803 (frontend + docs)

## Problem

PromptArena has two output modes: a terminal TUI for live run observation and static HTML reports for post-run analysis. Neither supports collaborative viewing, remote access, or browser-based interaction. The `promptarena serve` command was a static file server — it served completed reports but had no live capabilities.

Teams using Arena in shared environments (CI dashboards, remote dev, pair review) had no way to watch runs live in a browser or interact with results beyond opening a static HTML file.

## Solution

Replaced the `promptarena serve` command with a live web UI that streams Arena run events to a React frontend via SSE. Previous runs are loaded from the output directory on startup. New runs can be started from the browser.

## Architecture

```
Browser (React SPA)  ←── SSE ──←  EventAdapter  ←── events.Bus ←── Engine
       ↑                                                              ↑
       └──── REST API (start/clear runs, config, results) ───────────┘
```

The event bus is already the backbone of the TUI. The TUI's `EventAdapter` subscribes to the bus and maps `events.Event` → bubbletea messages. The web UI's `EventAdapter` maps `events.Event` → SSE JSON frames. No changes to the engine, pipeline, or event types.

### Go Backend (679 lines)

**EventAdapter** (`web/event_adapter.go`, 185 lines)
- Subscribes to `events.Bus` via `SubscribeAll`
- Maps 12+ event data types (provider calls, messages, tool calls, validations, custom arena events)
- Handles both value and pointer receivers (e.g., `CustomEventData` and `*CustomEventData`)
- JSON serialization and fan-out to concurrent SSE client channels
- Backpressure: drops events when client buffer is full (256 events)

**HTTP Server** (`web/server.go`, 268 lines)
- `GET /api/events` — SSE endpoint, streams run events
- `POST /api/run` — start a run (accepts `providers`, `scenarios`, `regions` filters)
- `GET /api/config` — returns loaded arena config
- `GET /api/results` — lists completed run IDs
- `GET /api/results/{id}` — returns a single run result
- `DELETE /api/results` — clears all results (deletes files from output directory)
- SPA fallback — serves embedded React build via `go:embed`

**Serve Command** (`cmd/promptarena/serve_interactive.go`, 220 lines)
- Loads config from `config.arena.yaml`
- Creates engine, event bus with `WithMessageEvents()`
- Pre-loads existing JSON results from output directory into state store
- Embeds React build via `go:embed` — single binary, no external dependencies
- `--port` (default 8080), `--open` flags

### React Frontend (1,874 lines)

**Tech stack**: React 19, TypeScript, Vite, Tailwind CSS v4, shadcn/ui

**Design**: Light theme matching altairalabs-web (white cards, `#E2E8F0` borders, dark gradient header). Inter + JetBrains Mono fonts.

| Component | Lines | Description |
|-----------|------:|-------------|
| `useArenaEvents` hook | 226 | SSE connection + `useReducer` state machine |
| `useArenaAPI` hook | 42 | REST API wrapper (start, results, config, clear) |
| `types.ts` | 277 | All TypeScript types (SSE events, RunResult, Message, state) |
| `App.tsx` | 188 | Root component, error boundary, historical results table |
| `Layout.tsx` | 61 | Gradient header with logo, connection badge, Start Run button |
| `SummaryCards.tsx` | 51 | 6 live-updating stat cards (runs, cost, tokens) |
| `RunProgress.tsx` | 154 | Active/completed run list with expandable message preview |
| `ConversationThread.tsx` | 141 | Turn-by-turn messages with tool calls, validations |
| `DevToolsPanel.tsx` | 455 | Right-side inspector with 12 tabs matching HTML report |
| `AssertionsPanel.tsx` | 70 | Pass/fail assertion badges and table |
| `RunDetail.tsx` | 100 | Full run detail view fetched via REST API |
| `ScenarioMatrix.tsx` | 77 | Compact results table (scenario, provider, result, cost) |

**DevTools tabs**: Info, Workflow, Metrics, Tools, Prompt, Self-Play, Request, Response, Trace, Assertions, Evals, Validators, Raw — matching the HTML report exactly. Tabs appear conditionally based on available data. JSON viewer is collapsible with syntax highlighting.

### Tests (1,069 lines)

- 19 adapter tests: event mapping (all types, pointer and value), registration, unregistration, bus subscription, buffer overflow, `*CustomEventData` regression test
- 17 server tests: SSE streaming, all REST endpoints, mock engine, nil guards, clear results (file deletion + state store), CORS headers

### Additional Features (beyond original proposal)

- **Load existing results on startup** — previous runs from the output directory appear immediately
- **Clear all results** — `DELETE /api/results` deletes JSON files from disk and clears state store
- **Time-ago column** — previous runs show "5m ago", "2h ago", etc., sorted newest-first
- **Error boundary** — React errors show a recovery UI instead of blank screen

## What Stays the Same

- Engine, pipeline, eval orchestrator — untouched
- Event bus and all event types — already generic pub/sub
- HTML report generation — still produced for `--formats html`
- `report-data.json` — still generated, also serves as the API response format
- TUI mode — still available via `promptarena run` (not `serve`)

## Key Design Decisions

**SSE over WebSocket**: The event flow is unidirectional (server → client). SSE is simpler, has automatic reconnection, and works through proxies without upgrade negotiation.

**Light theme with shadcn/ui**: Instead of porting the 3000-line HTML report CSS, used Tailwind + shadcn/ui with altairalabs-web design tokens. Light body background with dark header accent, matching the actual altairalabs-web visual style (hybrid light/dark).

**Single binary**: The React build is embedded via `go:embed`. Users don't need Node.js installed.

**Same state model as TUI**: The React `useReducer` mirrors the TUI `Model` state shape.

**Conditional DevTools tabs**: Tabs only appear when relevant data exists for the selected message, matching the HTML report behavior.

## Deviations from Original Proposal

| Proposed | Actual | Reason |
|----------|--------|--------|
| ~450 Go lines | 679 lines | Added result loading, clear endpoint, `ArenaStateStore.Clear()` |
| ~2,650 React lines | 1,874 lines | Tailwind/shadcn replaced verbose CSS; simpler table vs matrix |
| Provider×region matrix grid | Flat results table | Compact table with scenario/provider/result/cost per row is more readable |
| Dark theme (report CSS) | Light theme (altairalabs-web) | altairalabs-web is actually light-dominant with dark accents |
| ~700 test lines | 1,069 lines | More thorough coverage including regression tests |
| No result loading | Load on startup | Essential UX — users expect to see previous results |
| No clear button | DELETE /api/results | Requested during development |

## Future Extensions

These are not in scope but the architecture supports them:

- **Multi-user**: Multiple browsers can connect to the same SSE endpoint simultaneously (already works)
- **Remote CI dashboard**: Run `promptarena serve --port 8080` in CI, tunnel or expose the port
- **Run comparison**: Side-by-side view of two completed runs
- **Filtering/search**: Filter runs by provider, scenario, status in the UI
- **Annotations**: Add notes or tags to completed runs
