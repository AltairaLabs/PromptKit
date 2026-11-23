# Runtime Module — Claude Code Instructions

## Role

The runtime is PromptKit's core library. It defines all interfaces, executes LLM calls, manages tools, runs pipelines, and handles state. **Runtime never imports SDK or Arena** — it is the foundation that both depend on.

## Key Invariant

**Runtime has zero dependencies on `sdk/` or downstream consumers (PromptArena)**. All extensibility is via interfaces that higher-level modules implement. If you need to add functionality that both SDK and Arena use, it belongs here.

**Runtime is also a leaf module — it must not depend on `pkg/` either.** `runtime/go.mod` carries no `require` or `replace` on a sibling PromptKit module, and `scripts/check-runtime-is-leaf.sh` enforces it in CI and again before release tagging. Runtime used to import `pkg/config` from `hooks/exec_build.go`, which made the two modules require each other; that cycle is why every published library tag shipped a stale internal `require` for five releases (issue #1713).

If runtime and `pkg/config` both need a declarative type, put it in `runtime/hooks/execconfig` (a stdlib-only leaf package) and re-export it from `pkg/config` as an **alias** — `type ExecHook = execconfig.ExecHook`. An alias keeps call sites, YAML tags and the generated JSON schema identical; a defined type does not.

**Runtime must run on a `FROM scratch` image — always.** No cgo, no shelling out to external binaries (ffmpeg and friends), pure-Go decoders only. Check a new dependency's build constraints before reaching for it: anything needing cgo or a runtime executable belongs behind an interface a downstream consumer implements, not in runtime.

## Architecture Overview

```
runtime/
├── tools/         # Tool registry, executors, descriptors
├── pipeline/stage/ # Pipeline DAG, stages, ProviderStage (LLM+tool loop)
├── workflow/      # State machine, transitions, artifacts, budgets
├── providers/     # LLM provider interfaces + implementations
├── events/        # EventBus, Emitter, 40+ event types
├── hooks/         # ProviderHook, ToolHook, SessionHook
├── statestore/    # Store interface, MessageLog, optional interfaces
├── prompt/        # Registry, pack loading, template engine
├── types/         # Shared types (Message, ContentPart, CostInfo)
├── tokenizer/     # Heuristic token counting
└── ...            # audio, stt, tts, mcp, variables, etc.
```

## Tool Execution Model

Everything flows through the `tools.Executor` interface:

```go
type Executor interface {
    Execute(ctx context.Context, descriptor *ToolDescriptor, args json.RawMessage) (json.RawMessage, error)
    Name() string
}
```

**Mode-based dispatch**: Each tool has a `Mode` field. The Registry looks up `executors[tool.Mode]` to find the right executor. Built-in modes: `mock`, `live` (HTTP), `mcp`, `client`, `local`. Custom modes work automatically — register an executor with `Name()` matching the mode string.

**SDK's `OnTool()` handlers are wrapped in a `localExecutor`** that implements `Executor`. Arena registers executors directly. Both paths converge at `Registry.Execute()`.

## Pipeline & ProviderStage

The ProviderStage runs the LLM-tool loop:

1. Call provider (Predict/PredictWithTools)
2. If response has tool calls → execute via Registry → append results → loop
3. Between rounds: compaction, cost budget check, idle timeout reset
4. Max 50 rounds (configurable via ToolPolicy.MaxRounds)

**Key config on ProviderConfig**: `Compactor CompactionStrategy`, `MessageLog`, `MessageLogConvID`

## Workflow State Machine

`runtime/workflow/` owns the state machine, transitions, visit counting, budgets, and artifacts. The `workflow__transition` tool descriptor is built here (`transition_tool.go`), but **handlers are wired by consumers** (SDK, Arena) because they have different execution semantics.

**ProcessEvent returns `(*TransitionResult, error)`** — redirects (max_visits) are successful transitions, not errors.

## PromptKit reads the PromptPack spec. It does not extend it.

The spec lives in another repo. Everything a pack author types — keys, roles,
param names, reserved values — is owned there, and this runtime is a consumer
of it. Two rules follow, and both have been broken here:

1. **Never author a name a pack must use.** `JudgeProviderKey = "judge"` had the
   runtime decide what a pack calls its grading provider, resolved it by
   convention, and shipped in v2.4.0 before it was caught (#1996 → corrected in
   #2027). If a feature seems to need new pack vocabulary, it is a request
   against the spec repo, not a constant here.

2. **Never let a pack name something host-side.** `classifier_id` took a
   provider id that meant something only in one deployment, so a host could not
   swap providers without editing someone else's pack (removed in #2027).

The shape that satisfies both: a pack declares a LOGICAL name in `requires`, a
check points at that name, and the host binds it to whatever it likes and stays
free to rebind it. One param — `provider` — carries it for judges, classifiers
and anything later. See `runtime/evals/binding.go`.

`TestPackFacingParams_AreDeclared` (runtime/evals/handlers) makes new pack
vocabulary visible in the diff: a param a handler reads must be declared with a
reason. It cannot see a name invented elsewhere in the codebase — which is
exactly what `JudgeProviderKey` was — so it is a backstop for this rule, not a
substitute for it.

## Adding New Functionality

- **New tool executor**: Implement `Executor`, register via `Registry.RegisterExecutor()`
- **New event type**: Add to `events/types.go`, add emitter method, subscribe in consumers
- **New hook type**: Define interface in `hooks/`, add to Registry, call from appropriate stage
- **New pipeline stage**: Implement `Stage` interface, wire in `sdk/internal/pipeline/builder.go`. A stage of the realtime (duplex) chain from the provider stage on gets a slot in `stage.NewRealtimeChain` (`pipeline/stage/realtime_chain.go`) instead: the SDK builds its realtime chain from it and hosts (PromptArena) are expected to, so its order is defined once. Where its code goes: see below
- **New provider capability**: Define optional interface (like `ContextWindowProvider`), type-assert in consumers
- **New statestore capability**: Define optional interface (like `MessageLog`), type-assert in pipeline stages

## Where a new stage goes

`pipeline/stage` is a facade. Its code lives in `pipeline/stage/internal/<pkg>`
packages (`core`, `history`, `compaction`, `heard`, `segment`, `media`, `voice`,
`cue`, `persist`, `provider`, `duplex`, plus `stagetest` for shared test
helpers). stage re-exports the API of `core`, `history`, `compaction`,
`heard`, `media`, `voice`, `cue`, `persist`, `provider` and `duplex` through a
generated `<pkg>_aliases.go` for each. `segment` and `stagetest` have no alias
file: `segment` holds the segmenters that `provider` and `voice` use, none
of which stage ever exported, and `stagetest` is for tests only. The split keeps each Mutation Gate mutant's rebuild small: a mutant
recompiles only its own package and that package's tests. The table of what
each package holds is in `pipeline/stage/README.md` ("Package layout").

- **Put the stage in the internal package of its subsystem**: speech/TTS/VAD in
  `voice`, media in `media`, saving and recording in `persist`, the LLM
  call and tool loop in `provider`, the realtime session in `duplex`. A type or
  helper several packages share goes in `core`. Only the composition roots stay
  in `stage` itself: `pipeline.go`, `builder.go`, the context/core stages,
  composition and `realtime_chain.go`. A test that chains stages from several
  internal packages stays in `stage` too.
- **Imports point down; nothing under `internal/` imports `stage`**, not even a
  `_test.go` file (`TestInternalPackagesDoNotImportStage`). Tests in an internal
  package cannot use stage's test helpers: put a helper that tests in several
  packages need in `internal/stagetest`, which imports only `core`.
- **Export it from the internal package, then regenerate the facade**:
  `make stage-aliases PKG=<pkg>`, then `make docs-reference` (the alias files
  carry the internal doc comments onto stage's reference page). Never edit a
  `*_aliases.go` by hand; `make stage-aliases-check` fails on it. The generator
  re-exports only the names stage already declares at `BASE` (default
  `origin/main`), which is what keeps an internal package's helpers out of
  stage's API. A name stage never exported needs one first step by hand: add a
  bare re-export (`type FooStage = voice.FooStage`) to `<pkg>_aliases.go`,
  commit, and run `make stage-aliases PKG=<pkg> BASE=HEAD`, which rewrites it in
  the generator's form with its doc comment. `make stage-api-check` runs apidiff
  on stage against the merge base, as the "Stage API Compatibility" CI job does.
- **Never add methods to an aliased type from stage**: Go forbids it, and an
  exported method added to the internal type just so stage can reach unexported
  state widens stage's API. Add a package-level function in the internal
  package instead (`duplex.DetachCues(provider)`, `cue.BankOf(s)`). stage does
  not re-export it.
- **When a parameter or variable shadows an internal package's name**, import
  the package under another name rather than renaming the parameter, which
  would change stage's documented signature: hand-written stage code uses
  `providerstage "…/pipeline/stage/internal/provider"` (`composition_executor.go`);
  the generator does the same on its own (`providerpkg`, `duplexpkg`).
- Exported names in an internal package keep their stage names (`voice.TTSStage`,
  not `voice.Stage`), so `%T` output and the aliases stay identical; revive's
  stutter check is turned off under `pipeline/stage/internal/` for that.

## Testing

```bash
go -C runtime test ./... -race -count=1     # from the repo root
go test ./... -race -count=1                # from inside runtime/
```

`go test ./runtime/...` from the repo root does **not** work — there is no root `go.mod`. `make test` / `make test-race` cover every module; see the root `CLAUDE.md`.

Runtime tests are self-contained — no external services needed. Mock providers and in-memory stores are used throughout.
