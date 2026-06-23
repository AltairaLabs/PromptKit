package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/turnexecutors"
)

// AgentInfo describes one selectable agent in an Arena config. An "agent" is a
// prompt config, keyed by its task_type, carrying the system prompt and tools.
type AgentInfo struct {
	TaskType    string
	Description string
}

// Agents lists the prompt configs (agents) declared in the loaded config,
// sorted by task_type for stable ordering in pickers.
func (e *Engine) Agents() []AgentInfo {
	taskTypes := make([]string, 0, len(e.config.LoadedPromptConfigs))
	for _, pd := range e.config.LoadedPromptConfigs {
		if pd.TaskType != "" {
			taskTypes = append(taskTypes, pd.TaskType)
		}
	}
	sort.Strings(taskTypes)
	out := make([]AgentInfo, len(taskTypes))
	for i, tt := range taskTypes {
		out[i] = AgentInfo{TaskType: tt}
	}
	return out
}

// ProviderIDs lists the inference provider IDs declared in the loaded config,
// sorted for stable ordering.
func (e *Engine) ProviderIDs() []string {
	out := make([]string, 0, len(e.config.LoadedProviders))
	for id := range e.config.LoadedProviders {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// MissingRequiredVars returns the names of template variables the prompt for
// taskType declares as required but that are absent (or blank) in provided.
// The console uses this to prompt the user before chatting.
func (e *Engine) MissingRequiredVars(taskType string, provided map[string]string) ([]string, error) {
	pc, err := e.promptConfigForTaskType(taskType)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, v := range pc.Spec.Variables {
		if !v.Required {
			continue
		}
		if val, ok := provided[v.Name]; !ok || val == "" {
			missing = append(missing, v.Name)
		}
	}
	return missing, nil
}

// promptConfigForTaskType finds the loaded prompt config whose task_type matches
// taskType. Returns an error if no match is found.
func (e *Engine) promptConfigForTaskType(taskType string) (*prompt.Config, error) {
	for _, pd := range e.config.LoadedPromptConfigs {
		if pd.TaskType != taskType {
			continue
		}
		if pd.Config == nil {
			return nil, fmt.Errorf("prompt config for task type %q has nil config", taskType)
		}
		cfg, ok := pd.Config.(*prompt.Config)
		if !ok {
			return nil, fmt.Errorf("prompt config for task type %q has invalid type", taskType)
		}
		return cfg, nil
	}
	return nil, fmt.Errorf("no prompt config found for task type %q", taskType)
}

// scriptedExecutorFrom reaches the scripted TurnExecutor through whatever
// ConversationExecutor shape BuildEngineComponents produced. Mirrors
// evalOrchestratorFrom. Returns nil if none can be reached.
func scriptedExecutorFrom(executor ConversationExecutor) turnexecutors.TurnExecutor {
	switch ce := executor.(type) {
	case *DefaultConversationExecutor:
		return ce.scriptedExecutor
	case *CompositeConversationExecutor:
		if ce.defaultExecutor != nil {
			return ce.defaultExecutor.scriptedExecutor
		}
	}
	return nil
}

// InteractiveSessionOptions configures a live chat session.
type InteractiveSessionOptions struct {
	ProviderID string            // provider to chat against
	TaskType   string            // agent (prompt config) task_type
	Variables  map[string]string // user-supplied template variables
	RunEvals   bool              // when true, run config evals per turn for scores
}

// InteractiveSession drives one user turn at a time against a chosen agent and
// provider, persisting history under a fresh conversation ID. Guardrails fire
// automatically via the provider pipeline; assertions are never populated.
type InteractiveSession struct {
	engine         *Engine
	scripted       turnexecutors.TurnExecutor
	provider       providers.Provider
	scenario       *config.Scenario
	taskType       string
	promptVars     map[string]string
	conversationID string
	runEvals       bool
}

// NewInteractiveSession builds a session. Errors if the provider or scripted
// executor cannot be resolved. Variables are merged over the config's
// prompt-config vars (user wins).
func (e *Engine) NewInteractiveSession(opts InteractiveSessionOptions) (*InteractiveSession, error) {
	provider, ok := e.providerRegistry.Get(opts.ProviderID)
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", opts.ProviderID)
	}
	scripted := scriptedExecutorFrom(e.conversationExecutor)
	if scripted == nil {
		return nil, fmt.Errorf("scripted executor unavailable")
	}

	// Merge prompt-config vars (matched by task_type, not by map key which is the
	// ref ID for file-based configs) under the user's vars. This mirrors the
	// canonical pattern in conversation_executor.go:355-363.
	promptVars := map[string]string{}
	for _, pc := range e.config.LoadedPromptConfigs {
		if pc != nil && pc.TaskType == opts.TaskType {
			for k, v := range pc.Vars {
				promptVars[k] = v
			}
			break
		}
	}
	for k, v := range opts.Variables {
		promptVars[k] = v
	}

	scenario := &config.Scenario{
		ID:        "interactive",
		TaskType:  opts.TaskType,
		Streaming: true,
	}

	return &InteractiveSession{
		engine:         e,
		scripted:       scripted,
		provider:       provider,
		scenario:       scenario,
		taskType:       opts.TaskType,
		promptVars:     promptVars,
		conversationID: fmt.Sprintf("interactive-%d", time.Now().UnixNano()),
		runEvals:       opts.RunEvals,
	}, nil
}

// ConversationID returns this session's persistence key.
func (s *InteractiveSession) ConversationID() string { return s.conversationID }

// Provider returns the resolved provider for this session. Used by the voice
// chat path to obtain the provider handle without a separate registry lookup.
func (s *InteractiveSession) Provider() providers.Provider { return s.provider }

// RunEvalsEnabled reports whether eval scoring is on for this session.
func (s *InteractiveSession) RunEvalsEnabled() bool { return s.runEvals }

// SendUserMessage drives one live user turn and returns the assistant stream.
// Assertions are intentionally left nil so the assertion stage never runs;
// guardrails still fire inside the provider pipeline.
func (s *InteractiveSession) SendUserMessage(
	ctx context.Context,
	text string,
) (<-chan turnexecutors.MessageStreamChunk, error) {
	seed := s.engine.config.Defaults.Seed
	req := turnexecutors.TurnRequest{
		Provider:       s.provider,
		Scenario:       s.scenario,
		Temperature:    float64(s.engine.config.Defaults.Temperature),
		MaxTokens:      s.engine.config.Defaults.MaxTokens,
		Seed:           &seed,
		PromptRegistry: s.engine.promptRegistry,
		TaskType:       s.taskType,
		PromptVars:     s.promptVars,
		BaseDir:        s.engine.config.ConfigDir,
		ConversationID: s.conversationID,
		RunID:          s.conversationID,
		StateStoreConfig: &turnexecutors.StateStoreConfig{
			Store: s.engine.GetStateStore(),
		},
		ScriptedContent: text,
		EventBus:        s.engine.eventBus,
	}
	return s.scripted.ExecuteTurnStream(ctx, req)
}

// Messages loads the full transcript from the state store.
func (s *InteractiveSession) Messages(ctx context.Context) ([]types.Message, error) {
	state, err := s.engine.GetStateStore().Load(ctx, s.conversationID)
	if err != nil {
		if errors.Is(err, statestore.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if state == nil {
		return nil, nil
	}
	return state.Messages, nil
}

// RunEvals runs the config's evals against the current transcript and returns
// their raw scores. Returns nil when eval scoring is disabled for the session
// or the config declares no evals. Evals are pure primitives (no pass/fail).
func (s *InteractiveSession) RunEvals(ctx context.Context) ([]evals.EvalResult, error) {
	if !s.runEvals {
		return nil, nil
	}
	orch := s.engine.evalOrchestrator
	if orch == nil || !orch.HasEvals() {
		return nil, nil
	}
	msgs, err := s.Messages(ctx)
	if err != nil {
		return nil, err
	}
	return orch.RunSessionEvals(ctx, msgs, s.conversationID), nil
}

// HasConfigEvals reports whether the config declares any evals the console can
// run for scores.
func (e *Engine) HasConfigEvals() bool {
	return e.evalOrchestrator != nil && e.evalOrchestrator.HasEvals()
}

// Cost sums per-message cost across the transcript.
func (s *InteractiveSession) Cost(ctx context.Context) (types.CostInfo, error) {
	msgs, err := s.Messages(ctx)
	if err != nil {
		return types.CostInfo{}, err
	}
	var total types.CostInfo
	for i := range msgs {
		if msgs[i].CostInfo == nil {
			continue
		}
		c := msgs[i].CostInfo
		total.InputTokens += c.InputTokens
		total.OutputTokens += c.OutputTokens
		total.CachedTokens += c.CachedTokens
		total.InputCostUSD += c.InputCostUSD
		total.OutputCostUSD += c.OutputCostUSD
		total.CachedCostUSD += c.CachedCostUSD
		total.TotalCost += c.TotalCost
	}
	return total, nil
}
