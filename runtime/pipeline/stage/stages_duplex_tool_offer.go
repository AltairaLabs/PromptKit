package stage

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/provider"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// This file holds the realtime session's tool offer: the tools a text turn
// would offer the model (the prompt's allowed tools, the conversation's
// grants and the capability tools, as DescribeTool describes them), merged
// with the host's StreamingInputConfig.Tools.
//
// The session is created with the offer. After each tool round, and when a
// new tool turn starts, the offer is built again; when it changed (a grant,
// an exclusion, DescribeTool) a session that implements
// providers.StreamToolUpdater is given the new tools before the round's
// responses. A session without it, or whose update failed, keeps the tools it
// holds: a call to a tool the conversation offers but the session does not
// hold is refused, and so is a call to a tool the session holds but the
// conversation has withdrawn (a revoked grant). A tool excluded for one tool
// turn after repeated rejections is not withdrawn: a call to it still runs.
// A call to a registered tool the conversation does not offer at all (never
// granted, or a grant revoked since the session was last updated) is refused
// too. Each round decides these from the offer and the session's tools as
// they are when it begins (DuplexProviderStage.roundRefusals). Every such
// refusal is the tool runner's, after the blocklist and the tool hooks, as on
// a text turn.
//
// A spent max_tool_calls_per_turn budget does not change the offer: calls
// over it are refused (overBudgetResults), and a session stripped of its
// tools could not make the call that would restore them.
//
// The policy's tool_choice is not sent to the session. On a text turn it
// governs the first round only; a realtime session has no rounds to scope it
// to, and "required" or a tool name kept for the session would force a call
// after every tool result. Only the host's StreamingInputConfig.ToolChoice is
// sent. A policy tool_choice of "none" offers no tools.

// sessionOffer is what the realtime session holds of the offer.
type sessionOffer struct {
	// mu serializes building the offer with sending it to the session.
	mu sync.Mutex
	// held are the tools the session holds, heldKey their fingerprint.
	held    []providers.StreamingToolDefinition
	heldKey string
	// updateFailed is set while the last tool update of a session that can
	// change its tools failed: it keeps the tools it held. A later update
	// that succeeds clears it. Readable without mu.
	updateFailed atomic.Bool
	// names are held's names, sorted, readable without mu.
	names atomic.Pointer[[]string]
}

// setHeldLocked records tools as what the session holds. Called with mu held,
// or before the session runs.
func (o *sessionOffer) setHeldLocked(defs []providers.StreamingToolDefinition, key string) {
	o.held, o.heldKey = defs, key
	names := toolDefNames(defs)
	sort.Strings(names)
	o.names.Store(&names)
}

// heldNames returns the names of the tools the session holds, sorted.
func (o *sessionOffer) heldNames() []string {
	if p := o.names.Load(); p != nil {
		return *p
	}
	return nil
}

// heldSet returns the names of the tools the session holds, as a set.
func (o *sessionOffer) heldSet() map[string]bool {
	names := o.heldNames()
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

// sessionSyncRefusal is the refusal of a call to name on a session that
// cannot change its tools, or whose update failed, where they differ from
// the conversation's offer: with wanted, the conversation offers it but the
// session does not hold it; without, the session holds it but the
// conversation has withdrawn it.
func (o *sessionOffer) sessionSyncRefusal(name string, wanted bool) string {
	what, cannot := "was withdrawn", "the provider cannot remove tools from a live session"
	if wanted {
		what, cannot = "became available", "the provider cannot add tools to a live session"
	}
	if o.updateFailed.Load() {
		cannot = "the session's tools could not be updated"
	}
	return fmt.Sprintf("Tool %s was not run: it %s after this realtime session started, and %s",
		name, what, cannot)
}

// sessionToolChoice is the session's tool_choice: the host's
// StreamingInputConfig.ToolChoice. The policy's is never sent (see above).
func (s *DuplexProviderStage) sessionToolChoice() string {
	if s.baseConfig == nil {
		return ""
	}
	return s.baseConfig.ToolChoice
}

// allowedTools is the prompt's allowed tools, or nil without turn state.
func (s *DuplexProviderStage) allowedTools() []string {
	if s.turnState == nil {
		return nil
	}
	return s.turnState.AllowedTools
}

// wantedSessionTools is what the session should hold now: the conversation's
// offer less excluded, merged with the host's tools. None when the host's or
// the policy's tool choice is "none".
func (s *DuplexProviderStage) wantedSessionTools(excluded map[string]bool) []providers.StreamingToolDefinition {
	r := s.toolRunner()
	return s.sessionTools(r, r.Offer(s.allowedTools(), excluded), excluded)
}

// sessionTools is what the session is given of offer, the conversation's
// offer less excluded: offer merged with the host's tools
// (mergeSessionTools), or nothing when the host's or the policy's tool
// choice is "none". The session's sync (wantedSessionTools) and each
// round's check (sessionOffers) both apply these rules through it.
func (s *DuplexProviderStage) sessionTools(
	r *provider.ToolRunner, offer []*tools.ToolDescriptor, excluded map[string]bool,
) []providers.StreamingToolDefinition {
	if s.sessionToolChoice() == provider.ToolChoiceNone || r.ToolsDisabled() {
		return nil
	}
	var host []providers.StreamingToolDefinition
	if s.baseConfig != nil {
		host = s.baseConfig.Tools
	}
	return mergeSessionTools(offer, host, excluded)
}

// mergeSessionTools merges the conversation's offer with the host's tools:
// the offer's order, with the host's definition in place of an offered tool
// of the same name, then the host's other tools in their order, less
// excluded.
func mergeSessionTools(
	offer []*tools.ToolDescriptor, host []providers.StreamingToolDefinition, excluded map[string]bool,
) []providers.StreamingToolDefinition {
	hostByName := make(map[string]int, len(host))
	for i := range host {
		hostByName[host[i].Name] = i
	}
	out := make([]providers.StreamingToolDefinition, 0, len(offer)+len(host))
	used := make(map[string]bool, len(host))
	for _, d := range offer {
		if i, ok := hostByName[d.Name]; ok {
			out = append(out, host[i])
			used[d.Name] = true
			continue
		}
		out = append(out, streamingToolDefinition(d))
	}
	for i := range host {
		if !used[host[i].Name] && !excluded[host[i].Name] {
			out = append(out, host[i])
		}
	}
	return out
}

// streamingToolDefinition converts an offered tool to a session tool.
func streamingToolDefinition(d *tools.ToolDescriptor) providers.StreamingToolDefinition {
	def := providers.StreamingToolDefinition{Name: d.Name, Description: d.Description}
	if len(d.InputSchema) > 0 {
		if err := json.Unmarshal(d.InputSchema, &def.Parameters); err != nil {
			logger.Warn("duplex provider stage: tool input schema is not a JSON object; offering it without one",
				"tool", d.Name, "error", err)
		}
	}
	return def
}

// toolDefNames returns the names of defs, in order.
func toolDefNames(defs []providers.StreamingToolDefinition) []string {
	names := make([]string, 0, len(defs))
	for i := range defs {
		names = append(names, defs[i].Name)
	}
	return names
}

// sessionToolsKey fingerprints a tools list: each tool's name, description
// and schema, in order.
func sessionToolsKey(defs []providers.StreamingToolDefinition) string {
	var sb strings.Builder
	for i := range defs {
		params, _ := json.Marshal(defs[i].Parameters)
		sb.WriteString(defs[i].Name)
		sb.WriteByte(0)
		sb.WriteString(defs[i].Description)
		sb.WriteByte(0)
		sb.Write(params)
		sb.WriteByte(0)
	}
	return sb.String()
}

// initialSessionTools sets cfg's tools and tool choice for a new session and
// records them as what the session holds. Without tool execution cfg keeps
// the host's.
func (s *DuplexProviderStage) initialSessionTools(cfg *providers.StreamingInputConfig) {
	if s.tools == nil {
		return
	}
	cfg.Tools = s.wantedSessionTools(nil)
	cfg.ToolChoice = s.sessionToolChoice()
	s.tools.offer.setHeldLocked(cfg.Tools, sessionToolsKey(cfg.Tools))
}

// syncSessionTools builds the offer for turn, less excluded, and gives a
// changed one to a session that can change its tools. With record, what the
// session holds afterwards is recorded as offered on turn: false when the
// reply the turn reports on began before the update. A turn that is no longer
// current changes nothing: a newer turn owns the offer, and syncs it when it
// starts, after this (both hold the offer's mu).
func (s *DuplexProviderStage) syncSessionTools(
	ctx context.Context, turn *duplexToolTurn, excluded map[string]bool, record bool,
) {
	dt := s.tools
	if dt == nil {
		return // without tool execution the session keeps the host's tools
	}
	o := &dt.offer
	o.mu.Lock()
	defer o.mu.Unlock()
	if !s.calls.isCurrent(turn) {
		return
	}
	want := s.wantedSessionTools(excluded)
	if key := sessionToolsKey(want); key != o.heldKey {
		s.updateSessionToolsLocked(ctx, want, key)
	}
	if record {
		s.calls.recordOffered(turn, o.heldNames())
	}
}

// updateSessionToolsLocked gives want to a session that can change its tools.
// A session that cannot, or whose update fails, keeps what it holds. Called
// with the offer's mu held.
func (s *DuplexProviderStage) updateSessionToolsLocked(
	ctx context.Context, want []providers.StreamingToolDefinition, key string,
) {
	updater, ok := s.session.(providers.StreamToolUpdater)
	if !ok {
		logger.Debug("duplex provider stage: the session cannot change its tools; keeping its offer",
			"offered", toolDefNames(want))
		return
	}
	o := &s.tools.offer
	if err := updater.UpdateTools(ctx, want); err != nil {
		logger.Warn("duplex provider stage: updating the session's tools failed; keeping its offer",
			"error", err)
		o.updateFailed.Store(true)
		return
	}
	o.updateFailed.Store(false) // the session holds the offer again
	o.setHeldLocked(want, key)
}

// isCurrent reports whether turn is the current tool turn.
func (ct *callTracker) isCurrent(turn *duplexToolTurn) bool {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.turn == turn
}

// recordOffered adds names to what turn offered the model.
func (ct *callTracker) recordOffered(turn *duplexToolTurn, names []string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	recordOfferedLocked(turn, names)
}

// recordOfferedLocked is recordOffered with ct.mu held.
func recordOfferedLocked(turn *duplexToolTurn, names []string) {
	if turn.offered == nil {
		turn.offered = make(map[string]bool, len(names))
	}
	for _, name := range names {
		turn.offered[name] = true
	}
}

// stampToolsOffered records on msg what turn offered the model: every tool
// the session held during the turn, sorted. Nothing when it offered none.
func (ct *callTracker) stampToolsOffered(turn *duplexToolTurn, msg *types.Message) {
	ct.mu.Lock()
	names := make([]string, 0, len(turn.offered))
	for name := range turn.offered {
		names = append(names, name)
	}
	ct.mu.Unlock()
	if len(names) == 0 {
		return
	}
	sort.Strings(names)
	if msg.Meta == nil {
		msg.Meta = map[string]interface{}{}
	}
	msg.Meta[types.MetaToolsOffered] = names
}
