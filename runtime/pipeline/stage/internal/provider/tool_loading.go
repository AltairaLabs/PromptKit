package provider

import (
	"fmt"
	"slices"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// ToolLoadingMode is how a conversation's tools array follows a change to
// what the model may call (ProviderConfig.ToolLoading).
type ToolLoadingMode string

const (
	// ToolLoadingRebuild sends each call the tools the model may call on it,
	// so the array changes whenever they do. It is the default.
	ToolLoadingRebuild ToolLoadingMode = "rebuild"
	// ToolLoadingStable sends every call the same tools array, and gives
	// what the model may call beside it (PredictionRequest.ActiveTools).
	ToolLoadingStable ToolLoadingMode = "stable"
)

// ToolLoading is how the provider stage builds the tools array it sends.
//
// In rebuild mode, the default, each call is sent the tools the model may
// call on it, and the array is rebuilt when they change: a ToolSelector's
// choice, a skill grant, a tool excluded after repeated rejection, a
// workflow state's tools, or max_tool_calls_per_turn spent. A provider that
// caches the prompt then processes the prefix again (tool.offer.rebuilt).
//
// In stable mode every call is sent the conversation's catalog: the
// prompt's allowed tools, Catalog's tools, the current grants and the
// capability tools. What the model may call on a call is given beside it in
// PredictionRequest.ActiveTools, which a provider that can restrict calls
// sends (OpenAI's tool_choice allowed_tools), and the stage refuses a call
// outside it with NotAvailableRefusal. With max_tool_calls_per_turn spent,
// the last round keeps the array and is sent tool choice none. The array is
// rebuilt only when the catalog itself changes: a grant or a workflow state
// naming a tool outside it, an MCP server's tool list, a DescribeTool
// change, or another provider.
type ToolLoading struct {
	// Mode is the mode; empty is ToolLoadingRebuild.
	Mode ToolLoadingMode
	// Catalog, in stable mode, returns tool names the conversation may be
	// offered later on, beyond the prompt's allowed tools: every tool a
	// skill can grant, or another workflow state's tools, say. Each is sent
	// from the first call, and called only once something lets the model
	// call it. A name ending in "*" is a pattern, expanded against the
	// registry as an allowed_tools entry is; any other is matched exactly,
	// and skipped when no tool is registered under it. Nil adds none.
	Catalog func() []string
}

// Stable reports whether l is stable mode.
func (l ToolLoading) Stable() bool { return l.Mode == ToolLoadingStable }

// stableTools reports whether the stage runs in tool_loading mode stable.
func (s *ProviderStage) stableTools() bool {
	return s.config != nil && s.config.ToolLoading.Stable()
}

// catalogExtras is ToolLoading.Catalog's tools, or none.
func (s *ProviderStage) catalogExtras() []string {
	if s.config == nil || s.config.ToolLoading.Catalog == nil {
		return nil
	}
	return s.config.ToolLoading.Catalog()
}

// catalogOffer returns the stable-mode tools array for acc, and the provider
// to build it for: the prompt's (or workflow state's) allowed tools before a
// ToolSelector narrowed them, the grants, ToolLoading.Catalog's tools and the
// capability tools, none excluded, each as DescribeTool describes it, sorted
// by name. Nil offer and provider when this call offers no tools at all (see
// offerableDescriptors).
func (s *ProviderStage) catalogOffer(acc *providerInput) ([]*tools.ToolDescriptor, providers.ToolSupport) {
	toolProvider, ok := s.toolCallingProvider()
	if !ok {
		return nil, nil
	}
	r := s.runner()
	if r.ToolsDisabled() {
		return nil, nil
	}
	// A catalog entry ending in "*" is a pattern, expanded as an
	// allowed_tools entry is; any other names exactly one tool, and one no
	// tool is registered under is skipped.
	allowed, exact := slices.Clone(acc.promptTools()), r.grantedTools()
	for _, name := range s.catalogExtras() {
		if strings.HasSuffix(name, "*") {
			allowed = append(allowed, name)
		} else {
			exact = append(exact, name)
		}
	}
	return r.collectOffer(allowed, exact, nil), toolProvider
}

// buildStableTools builds the stable-mode tools array from offer, the catalog
// catalogOffer returned for toolProvider. It returns the array, the policy's
// tool choice, and the snapshot of what it sent (none when it built no
// array).
func (s *ProviderStage) buildStableTools(
	offer []*tools.ToolDescriptor, toolProvider providers.ToolSupport,
) (providerTools interface{}, toolChoice string, catalog offerSnapshot, err error) {
	providerTools, toolChoice, err = s.buildProviderToolsFrom(offer, toolProvider)
	if err != nil {
		return nil, "", s.snapshotOf(nil), err
	}
	return providerTools, toolChoice, s.snapshotOf(offer), nil
}

// syncStable keeps the loop's tools array the catalog for its input: it is
// rebuilt only when the catalog changed (a grant or a workflow state naming
// a tool outside it, an MCP server's tool list, DescribeTool, another
// provider). Then it prepares what the next round may call (prepareActive).
// Reports whether it rebuilt the array.
func (tl *toolLoop) syncStable() (bool, error) {
	s := tl.stage
	offer, toolProvider := s.catalogOffer(tl.acc)
	rebuilt := false
	if !s.snapshotOf(offer).equal(tl.catalog) {
		providerTools, _, catalog, err := s.buildStableTools(offer, toolProvider)
		if err != nil {
			return false, err
		}
		rebuilt = !catalog.equal(tl.catalog)
		tl.providerTools, tl.catalog = providerTools, catalog
		tl.invalidateReserve()
	}
	tl.prepareActive()
	return rebuilt, nil
}

// prepareActive records what the model may call in the next round, in stable
// mode: what rebuild mode would send it (ToolRunner.Offer, for the turn's
// allowed tools as a ToolSelector narrowed them, less the turn's exclusions).
// It is what the turn reports offered (types.MetaToolsOffered), what a forced
// tool choice must fit (choiceFits), and what a round refuses calls outside
// of (roundRefusals). A call sent no tools array may call none.
func (tl *toolLoop) prepareActive() {
	s := tl.stage
	r := s.runner()
	var offer []*tools.ToolDescriptor
	if tl.providerTools != nil {
		offer = r.Offer(tl.acc.allowedTools, tl.Excluded)
	}
	active := make([]string, 0, len(offer))
	for _, d := range offer {
		active = append(active, d.Name)
	}
	slices.Sort(active)
	r.offers.setStable(active, tl.catalog.names)
	r.markSent(offer)
	s.recordOffered(providerDescriptors(offer))
}

// requestActiveTools is a provider call's PredictionRequest.ActiveTools: in
// stable mode, the round's active tools when the tools array holds more;
// otherwise nil, and always nil for a call sent no tools array.
func (s *ProviderStage) requestActiveTools(providerTools interface{}) []string {
	if providerTools == nil {
		return nil
	}
	return s.offerState().narrowedActive()
}

// stableChoiceNone reports whether a stable-mode round must be sent tool
// choice none: max_tool_calls_per_turn is spent, or the array holds tools
// and the model may call none of them.
func (tl *toolLoop) stableChoiceNone() bool {
	if tl.providerTools == nil {
		return false // no tools array, so no tool choice to send
	}
	if tl.exhausted {
		return true
	}
	active, ok := tl.stage.offerState().stableActive()
	return ok && len(active) == 0
}

// noteStableRoundOffer records why a tool round rebuilt the stable-mode
// array: the grants moved past the catalog, or else DescribeTool now
// describes a tool differently. Anything else is reported as a registry
// change.
func (tl *toolLoop) noteStableRoundOffer() {
	s := tl.stage
	o := s.offerLog()
	in := s.offerInputsFor(tl.acc)
	switch {
	case !slices.Equal(in.grants, o.lastInputs().grants):
		o.noteCause(events.ToolOfferReasonSkillGrant)
	case s.config.DescribeTool != nil:
		o.noteCause(events.ToolOfferReasonDescribe)
	}
	o.setInputs(in)
}

// applyStableHandoff is applyStateHandoff's stable-mode half, once the
// handoff switched the call (providerChanged): the turn takes the state's
// prompt, and its tools when they select others; the tools array is
// rebuilt only when the catalog changed, and what the model may call follows
// the state either way.
func (s *ProviderStage) applyStableHandoff(
	acc *providerInput, loop *toolLoop, handoff Handoff, providerChanged bool,
) error {
	sameTools := s.runner().selectsSameTools(handoff.AllowedTools, acc.promptTools())
	if !sameTools {
		acc.allowedTools, acc.eligibleTools = handoff.AllowedTools, nil
	}
	acc.systemPrompt = handoff.SystemPrompt
	rebuilt, err := loop.syncStable()
	if err != nil {
		return fmt.Errorf("provider stage: workflow handoff: rebuild tools: %w", err)
	}
	if rebuilt {
		s.noteHandoffOffer(acc, providerChanged, sameTools && !providerChanged)
	}
	loop.chooseTool(false, false)
	if providerChanged {
		loop.cachingSupported = supportsPromptCaching(s.callProvider())
	}
	if s.turnState != nil {
		s.turnState.SystemPrompt = handoff.SystemPrompt
		s.turnState.AllowedTools = handoff.AllowedTools
	}
	return nil
}
