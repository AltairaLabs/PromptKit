package provider

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// tool.offer.rebuilt reports a provider call sent a tools array that differs
// from the one the previous provider call was sent: the stage's previous
// call, or, with a shared ToolOfferHistory, the previous call of any stage
// sharing it. Every tools build
// records the array it made (offerSnapshot) and what caused it (noteOffer*);
// before each provider call, the round compares the array it sends against
// the last one sent (reportOfferSent). A build that is never sent, such as a
// grant superseded by a workflow handoff in the same round, is reported only
// through the array that is.

// offerSnapshot is a tools array as a provider is sent it: the names of its
// tools, sorted; a fingerprint of the tools as described (descriptorsKey);
// and the provider it was built for. The zero value is no array.
type offerSnapshot struct {
	names    []string
	key      string
	provider providers.Provider
}

// equal reports whether o and other are the same array for the same
// provider. Two calls sent no tools send the same array, whichever provider
// each went to.
func (o offerSnapshot) equal(other offerSnapshot) bool {
	if len(o.names) == 0 && len(other.names) == 0 {
		return true
	}
	return o.key == other.key && sameProvider(o.provider, other.provider) && slices.Equal(o.names, other.names)
}

// offerInputs is what a turn's offer is built from: the prompt's (or workflow
// state's) allowed tools, those after a ToolSelector narrowed them, and the
// conversation's grants.
type offerInputs struct {
	prompt  []string
	allowed []string
	grants  []string
}

// ToolOfferHistory is what tool.offer.rebuilt compares a provider call's
// tools array against: the array the previous call was sent, what has
// changed the array since, and whether the cache-cost warning was logged.
// Share one across the provider stages of a conversation, as the SDK does
// across a workflow's states, so a change from one stage's calls to the
// next stage's is reported and the warning is logged once. A stage built
// without one (ProviderConfig.ToolOfferHistory) keeps its own. Safe for
// concurrent use.
type ToolOfferHistory struct {
	mu  sync.Mutex
	log offerLog
}

// NewToolOfferHistory returns a history of no calls.
func NewToolOfferHistory() *ToolOfferHistory { return &ToolOfferHistory{} }

// offerLog is the record a ToolOfferHistory keeps.
type offerLog struct {
	current   offerSnapshot // the array last built, or none once withdrawn
	sent      offerSnapshot // the array the last provider call was sent
	sentAny   bool          // a provider call has been sent an array
	inputs    offerInputs   // what the turn's offer was last built from
	causes    []string      // what changed the array since the last call, in order, each once
	withdrawn bool          // the turn withdrew tools (excluded, exhausted, structured_output_reask)
	warned    bool          // the cache-cost warning was logged (claimWarning)
}

// setCurrent records snap as the array last built.
func (o *ToolOfferHistory) setCurrent(snap offerSnapshot) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.log.current = snap
}

// noteCause records reason as having changed the array since the last call.
func (o *ToolOfferHistory) noteCause(reason string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.addCauseLocked(reason)
}

func (o *ToolOfferHistory) addCauseLocked(reason string) {
	if !slices.Contains(o.log.causes, reason) {
		o.log.causes = append(o.log.causes, reason)
	}
	switch reason {
	case events.ToolOfferReasonExcluded, events.ToolOfferReasonExhausted, events.ToolOfferReasonStructuredOutputReask:
		o.log.withdrawn = true
	}
}

// noteTurn records the turn's offer as built from in, and, when the array
// built for it differs from the one last sent, why: a turn's first build
// has no single cause, so it is told apart by what it was built from. The
// stage's first turn records a cause too, which takeSent drops with the
// first call it reports nothing for.
func (o *ToolOfferHistory) noteTurn(in offerInputs, describes bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.log.current.equal(o.log.sent) {
		o.addCauseLocked(o.turnCauseLocked(in, describes))
	}
	o.log.inputs = in
	o.log.withdrawn = false
}

// turnCauseLocked names what changed a turn's array from the one last sent.
func (o *ToolOfferHistory) turnCauseLocked(in offerInputs, describes bool) string {
	prev, cur, sent := o.log.inputs, o.log.current, o.log.sent
	added, removed := diffNames(sent.names, cur.names)
	switch {
	case !sameProvider(cur.provider, sent.provider):
		return events.ToolOfferReasonProviderSwitch
	case !slices.Equal(in.prompt, prev.prompt):
		return events.ToolOfferReasonWorkflowState
	case !slices.Equal(in.allowed, prev.allowed):
		return events.ToolOfferReasonSelector
	case !slices.Equal(in.grants, prev.grants):
		return events.ToolOfferReasonClientTools
	case onlyMCP(added, removed):
		return events.ToolOfferReasonMCPListChanged
	case o.log.withdrawn:
		return events.ToolOfferReasonTurnReset
	case describes && len(added) == 0 && len(removed) == 0:
		return events.ToolOfferReasonDescribe
	default:
		return events.ToolOfferReasonRegistry
	}
}

// setInputs records what the offer is now built from, for a change made
// during the turn (a grant, a workflow handoff).
func (o *ToolOfferHistory) setInputs(in offerInputs) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.log.inputs = in
}

// lastInputs returns what the offer was last recorded built from.
func (o *ToolOfferHistory) lastInputs() offerInputs {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.log.inputs
}

// takeSent records the array last built as sent, and returns the change from
// the array sent before it, or nil when there is none.
func (o *ToolOfferHistory) takeSent() *events.ToolOfferRebuiltData {
	o.mu.Lock()
	defer o.mu.Unlock()
	cur, prev, first := o.log.current, o.log.sent, !o.log.sentAny
	causes := o.log.causes
	o.log.sent, o.log.sentAny, o.log.causes = cur, true, nil
	if first || cur.equal(prev) {
		return nil
	}
	if len(causes) == 0 {
		causes = []string{events.ToolOfferReasonRegistry}
	}
	added, removed := diffNames(prev.names, cur.names)
	return &events.ToolOfferRebuiltData{Reasons: causes, Added: added, Removed: removed}
}

// claimWarning reports whether the cache-cost warning is still to be logged,
// and marks it logged.
func (o *ToolOfferHistory) claimWarning() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.log.warned {
		return false
	}
	o.log.warned = true
	return true
}

// diffNames returns the names in to and not in from, and those in from and
// not in to, each sorted as from and to are. Nil when there are none.
func diffNames(from, to []string) (added, removed []string) {
	for _, n := range to {
		if !slices.Contains(from, n) {
			added = append(added, n)
		}
	}
	for _, n := range from {
		if !slices.Contains(to, n) {
			removed = append(removed, n)
		}
	}
	return added, removed
}

// mcpToolPrefix starts the name of every tool an MCP server provides
// (mcp__<server>__<tool>).
const mcpToolPrefix = "mcp__"

// onlyMCP reports whether there is a change and every changed name is an MCP
// tool's.
func onlyMCP(added, removed []string) bool {
	changed := append(slices.Clone(added), removed...)
	if len(changed) == 0 {
		return false
	}
	for _, n := range changed {
		if !strings.HasPrefix(n, mcpToolPrefix) {
			return false
		}
	}
	return true
}

// offerLog returns the history the stage compares its calls' tools arrays
// against: ProviderConfig.ToolOfferHistory, or one of its own. Called on the
// turn's goroutine.
func (s *ProviderStage) offerLog() *ToolOfferHistory {
	if s.offerHistory == nil {
		s.offerHistory = NewToolOfferHistory()
	}
	return s.offerHistory
}

// snapshotOf is the snapshot of an array built from offer for the stage's
// call provider.
func (s *ProviderStage) snapshotOf(offer []*tools.ToolDescriptor) offerSnapshot {
	names := make([]string, 0, len(offer))
	for _, d := range offer {
		names = append(names, d.Name)
	}
	slices.Sort(names)
	return offerSnapshot{names: names, key: descriptorsKey(offer), provider: s.callProvider()}
}

// noteTurnOffer records the turn's first tools build, from acc.
func (s *ProviderStage) noteTurnOffer(acc *providerInput) {
	s.offerLog().noteTurn(s.offerInputsFor(acc), s.config != nil && s.config.DescribeTool != nil)
}

// offerInputsFor is what acc's offer is built from now.
func (s *ProviderStage) offerInputsFor(acc *providerInput) offerInputs {
	if acc == nil {
		return offerInputs{grants: s.runner().grantedTools()}
	}
	return offerInputs{prompt: acc.promptTools(), allowed: acc.allowedTools, grants: s.runner().grantedTools()}
}

// noteHandoffOffer records a workflow handoff's tools build, from acc as the
// handoff left it: the destination state's tools, or another provider's
// wire form of them.
func (s *ProviderStage) noteHandoffOffer(acc *providerInput, providerChanged bool) {
	o := s.offerLog()
	if providerChanged {
		o.noteCause(events.ToolOfferReasonProviderSwitch)
	} else {
		o.noteCause(events.ToolOfferReasonWorkflowState)
	}
	o.setInputs(s.offerInputsFor(acc))
}

// noteRoundOffer records a rebuild a tool round caused: the round changed
// the offer (offerChanged) by moving the grants or excluding a tool, or else
// DescribeTool now describes the tools differently.
func (tl *toolLoop) noteRoundOffer(offerChanged bool) {
	o := tl.stage.offerLog()
	in := tl.stage.offerInputsFor(tl.acc)
	if offerChanged && !slices.Equal(in.grants, o.lastInputs().grants) {
		o.noteCause(events.ToolOfferReasonSkillGrant)
	}
	if excluded := len(tl.Excluded); excluded > tl.excludedNoted {
		tl.excludedNoted = excluded
		o.noteCause(events.ToolOfferReasonExcluded)
	}
	if !offerChanged {
		o.noteCause(events.ToolOfferReasonDescribe)
	}
	o.setInputs(in)
}

// withdrawOffer records that the rest of the turn is sent no tools, because
// reason withdrew them.
func (s *ProviderStage) withdrawOffer(reason string) {
	o := s.offerLog()
	o.setCurrent(offerSnapshot{provider: s.callProvider()})
	o.noteCause(reason)
}

// reportOfferSent records that round's provider call is sent the array last
// built, and publishes tool.offer.rebuilt when it differs from the array the
// previous call was sent. On a provider that caches the prompt it also logs,
// once per history, what that costs.
func (s *ProviderStage) reportOfferSent(ctx context.Context, round int) {
	o := s.offerLog()
	change := o.takeSent()
	if change == nil {
		return
	}
	change.Round = round
	change.CachingProvider = supportsPromptCaching(s.callProvider())
	if change.CachingProvider && o.claimWarning() {
		logger.Warn("tools array changed mid-conversation: a provider that caches the prompt cannot reuse "+
			"its cache past the first changed tool (Claude writes the whole prefix again), so this call "+
			"pays for the prefix again; keep the offered tools the same across rounds and turns to keep "+
			"the cache. Every further change is reported as a tool.offer.rebuilt event",
			"reasons", change.Reasons, "added", change.Added, "removed", change.Removed, "round", round)
	}
	if s.emitter != nil {
		s.emitter.ToolOfferRebuiltCtx(ctx, change)
	}
}
