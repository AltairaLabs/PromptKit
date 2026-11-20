package stage

import "sync"

// callTracker is the stage's provider-call lifecycle: which call is open,
// which call each provider generation is bound to, the tool turn (the chain
// of calls tool results continue) and its round count, the session's spend,
// and the completions waiting for their call's usage. It is always there,
// whether or not the stage runs the session's tools (SetToolExecution), so
// a cut reply's trailing chunks are dropped, cost lands on its own call and
// every call has its provider.call.started and provider.call.completed
// either way. See stages_duplex_cost.go for the invariants it keeps, and
// stages_duplex_cost_hold.go for the completions it holds.
type callTracker struct {
	// mu guards everything below and the tool turns' own state
	// (duplexToolTurn, duplexRound), which the response, input and tool
	// worker goroutines all reach.
	mu sync.Mutex
	// completionMu serializes sending provider.call.completed and
	// provider.call.usage events, so held completions go out in order and a
	// call's usage never precedes its completion. Taken before mu.
	completionMu sync.Mutex
	turn         *duplexToolTurn
	round        int
	open         *duplexRound
	// gens maps the provider's generations to the calls opened for them, so
	// cost reported for a generation lands on its own call; last is the call
	// started last (stages_duplex_cost.go). genOrder bounds gens.
	gens     map[int64]*duplexRound
	genOrder []int64
	// newTurnDue is set by a user turn that arrived while a call was under
	// way: the next generation's call starts the user's tool turn.
	newTurnDue bool
	// forgottenUpTo is the newest generation evicted from gens.
	forgottenUpTo int64
	last          *duplexRound
	spent         float64
	// held are the completions waiting for their call's usage, in the order
	// the calls completed (stages_duplex_cost_hold.go).
	held []*heldCompletion
	// terminal is set once a limit closes the session: no call is opened and
	// nothing more is run or reported.
	terminal bool

	// labels and source are what the provider call events carry
	// (DuplexToolConfig.Labels and Source). Set before the stage runs.
	labels map[string]string
	source string
}

// heldToolNames names the tools the session holds, or none without tool
// execution (the host's tools are not the stage's to count).
func (s *DuplexProviderStage) heldToolNames() []string {
	if s.tools == nil {
		return nil
	}
	return s.tools.offer.heldNames()
}

// newCallTurn is a fresh tool turn with maxRounds. Without tool execution it
// only groups calls: it runs nothing and enforces no budget.
func (s *DuplexProviderStage) newCallTurn(maxRounds int) *duplexToolTurn {
	if s.tools == nil {
		return &duplexToolTurn{}
	}
	return &duplexToolTurn{toolTurn: s.toolRunner().newTurn(nil, maxRounds)}
}
