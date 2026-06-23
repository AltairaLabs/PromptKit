package app

import (
	"context"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui"
)

// runResultGetter is the minimal slice of the arena state store RunPage needs
// to load a completed run's result for conversation drill-down.
type runResultGetter interface {
	GetResult(ctx context.Context, runID string) (*statestore.RunResult, error)
}

// RunPage drives the live run view inside the hub shell. It wraps the
// tui.Model (the 3-pane runs/logs/result view) and, on Activate, wires an event
// bus + adapter to stream runtime events into the model and kicks off
// engine.ExecuteRuns in a background goroutine. Drill-down into a run's
// conversation is handled at the App-stack level: when the model reports a
// selected run, RunPage pushes a ConversationViewPage loaded from the state
// store.
//
// Implements Page and Activatable.
type RunPage struct {
	model       *tui.Model
	engine      *engine.Engine
	plan        *engine.RunPlan
	concurrency int
	store       runResultGetter

	bus      events.Bus
	cancel   context.CancelFunc
	started  bool
	mu       sync.Mutex
	runErr   error
	finished bool
	runIDs   []string
}

// NewRunPage builds a RunPage for the given engine and run plan. The run
// command performs setupEngine / plan generation and hands the results in;
// RunParameters lives in package main and cannot be imported here, so callers
// pass the already-built engine, plan, concurrency, configFile and totalRuns.
func NewRunPage(
	ctx *AppContext,
	eng *engine.Engine,
	plan *engine.RunPlan,
	concurrency int,
	configFile string,
	totalRuns int,
) *RunPage {
	model := tui.NewModel(configFile, totalRuns)
	// The hub shell owns terminal detection and the bubbletea program, so the
	// wrapped model should always render its interactive view.
	model.EnableTUIMode()
	if arenaStore, ok := eng.GetStateStore().(*statestore.ArenaStateStore); ok {
		model.SetStateStore(arenaStore)
	}
	// Prefer ctx.StateStore for drill-down loading, falling back to the
	// engine's own store.
	var getter runResultGetter
	if ctx != nil && ctx.StateStore != nil {
		if s, ok := ctx.StateStore.(runResultGetter); ok {
			getter = s
		}
	}
	if getter == nil {
		if s, ok := eng.GetStateStore().(runResultGetter); ok {
			getter = s
		}
	}
	return &RunPage{
		model:       model,
		engine:      eng,
		plan:        plan,
		concurrency: concurrency,
		store:       getter,
	}
}

// Title implements Page.
func (p *RunPage) Title() string { return "Run" }

// Init implements Page. Delegates to the wrapped model.
func (p *RunPage) Init() tea.Cmd { return p.model.Init() }

// Activate implements Activatable. It wires the event bus + adapter to deliver
// runtime events via send and starts ExecuteRuns in the background. The run
// results persist to the engine's state store as today. Returns the model's
// Init command.
func (p *RunPage) Activate(send func(tea.Msg)) tea.Cmd {
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return p.model.Init()
	}
	p.started = true
	p.mu.Unlock()

	bus := events.NewEventBus()
	p.bus = bus
	p.engine.SetEventBus(bus)

	adapter := tui.NewEventAdapter(send)
	adapter.Subscribe(bus)

	runCtx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	go func() {
		runIDs, err := p.engine.ExecuteRuns(runCtx, p.plan, p.concurrency)
		p.mu.Lock()
		p.runIDs = runIDs
		p.runErr = err
		p.finished = true
		p.mu.Unlock()
		bus.Close()
		if err != nil {
			logger.Warn("arena run finished with error", "error", err)
		}
	}()

	return p.model.Init()
}

// Cancel stops the background run. Safe to call after the hub exits so an
// early quit ('q' / Ctrl+C) does not leak the ExecuteRuns goroutine.
func (p *RunPage) Cancel() {
	if p.cancel != nil {
		p.cancel()
	}
}

// Results returns the run IDs produced by ExecuteRuns, whether the run
// finished, and any execution error. Safe for concurrent use; intended to be
// read by the run command after the hub exits.
func (p *RunPage) Results() (runIDs []string, finished bool, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ids := make([]string, len(p.runIDs))
	copy(ids, p.runIDs)
	return ids, p.finished, p.runErr
}

// Update implements Page. It delegates to the wrapped model, then checks
// whether the user selected a run for drill-down; if so it pushes a
// ConversationViewPage loaded from the state store.
func (p *RunPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	_, cmd := p.model.Update(msg)

	if run, ok := p.model.TakeSelectedRun(); ok {
		if push := p.drillDownCmd(&run); push != nil {
			return p, tea.Batch(cmd, push)
		}
	}

	return p, cmd
}

// drillDownCmd builds a command that pushes a ConversationViewPage for the
// selected run, loading its result from the state store. Returns nil when no
// store is attached or the result cannot be loaded.
func (p *RunPage) drillDownCmd(run *tui.RunInfo) tea.Cmd {
	if p.store == nil {
		return nil
	}
	result, err := p.store.GetResult(context.Background(), run.RunID)
	if err != nil || result == nil {
		logger.Debug("run drill-down: result not available", "run_id", run.RunID, "error", err)
		return nil
	}
	scenarioID := result.ScenarioID
	if scenarioID == "" {
		scenarioID = run.Scenario
	}
	providerID := result.ProviderID
	if providerID == "" {
		providerID = run.Provider
	}
	cvp := NewConversationViewPage(run.RunID, scenarioID, providerID, result)
	return func() tea.Msg { return PushPageMsg{Page: cvp} }
}

// View implements Page. Delegates to the wrapped model.
func (p *RunPage) View() string { return p.model.View() }

// SetSize implements Page. Forwards the terminal size to the model as a
// WindowSizeMsg (the model tracks its own width/height).
func (p *RunPage) SetSize(w, h int) {
	p.model.Update(tea.WindowSizeMsg{Width: w, Height: h})
}

// Model exposes the wrapped tui.Model for testing.
func (p *RunPage) Model() *tui.Model { return p.model }

// NewRunPageFromContext is a convenience constructor for the Home menu factory.
// It calls EnsureEngine, generates a full (unfiltered) run plan, and returns a
// RunPage ready to be pushed onto the navigation stack. The default concurrency
// is 1 so the menu can always succeed without extra flags.
func NewRunPageFromContext(ctx *AppContext) (*RunPage, error) {
	eng, err := ctx.EnsureEngine()
	if err != nil {
		return nil, err
	}
	plan, err := eng.GenerateRunPlan(nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	totalRuns := len(plan.Combinations)
	return NewRunPage(ctx, eng, plan, 1, ctx.ConfigPath, totalRuns), nil
}
