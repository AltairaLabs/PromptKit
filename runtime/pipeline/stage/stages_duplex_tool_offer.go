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
	// unsynced names the tools the conversation offers that the session does
	// not hold, withdrawn the tools the session holds that the conversation no
	// longer offers: a call to either is refused.
	unsynced  map[string]bool
	withdrawn map[string]bool
	// updateFailed is set once a tool update of a session that can change its
	// tools failed: only then can such a session lack an offered tool.
	updateFailed bool
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

// unsyncedRefusal reports whether a call to name is refused because the
// session's tools differ from the conversation's offer, and the refusal: the
// conversation offers name but the session does not hold it, or the session
// holds it but the conversation has withdrawn it.
func (o *sessionOffer) unsyncedRefusal(name string) (string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	var what, cannot string
	switch {
	case o.unsynced[name]:
		what, cannot = "became available", "the provider cannot add tools to a live session"
	case o.withdrawn[name]:
		what, cannot = "was withdrawn", "the provider cannot remove tools from a live session"
	default:
		return "", false
	}
	if o.updateFailed {
		cannot = "the session's tools could not be updated"
	}
	return fmt.Sprintf("Tool %s was not run: it %s after this realtime session started, and %s",
		name, what, cannot), true
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
	if s.sessionToolChoice() == toolChoiceNone || r.toolsDisabled() {
		return nil
	}
	var host []providers.StreamingToolDefinition
	if s.baseConfig != nil {
		host = s.baseConfig.Tools
	}
	return mergeSessionTools(r.offer(s.allowedTools(), excluded), host, excluded)
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
	o := &dt.offer
	o.mu.Lock()
	defer o.mu.Unlock()
	if !dt.isCurrent(turn) {
		return
	}
	want := s.wantedSessionTools(excluded)
	if key := sessionToolsKey(want); key != o.heldKey {
		s.updateSessionToolsLocked(ctx, want, key)
	}
	o.unsynced = missingTools(want, o.held)
	// A tool excluded for this turn only (repeated rejections) is not
	// withdrawn: a call to it runs, as on a text turn. Only what the
	// conversation no longer offers at all (a revoked grant) is.
	conversation := want
	if len(excluded) > 0 {
		conversation = s.wantedSessionTools(nil)
	}
	o.withdrawn = missingTools(o.held, conversation)
	if record {
		dt.recordOffered(turn, o.heldNames())
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
		o.updateFailed = true
		return
	}
	o.setHeldLocked(want, key)
}

// missingTools names the tools in want that held lacks, or nil.
func missingTools(want, held []providers.StreamingToolDefinition) map[string]bool {
	have := make(map[string]bool, len(held))
	for i := range held {
		have[held[i].Name] = true
	}
	var missing map[string]bool
	for i := range want {
		if have[want[i].Name] {
			continue
		}
		if missing == nil {
			missing = map[string]bool{}
		}
		missing[want[i].Name] = true
	}
	return missing
}

// isCurrent reports whether turn is the current tool turn.
func (dt *duplexTools) isCurrent(turn *duplexToolTurn) bool {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return dt.turn == turn
}

// recordOffered adds names to what turn offered the model.
func (dt *duplexTools) recordOffered(turn *duplexToolTurn, names []string) {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	recordOfferedLocked(turn, names)
}

// recordOfferedLocked is recordOffered with dt.mu held.
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
func (dt *duplexTools) stampToolsOffered(turn *duplexToolTurn, msg *types.Message) {
	dt.mu.Lock()
	names := make([]string, 0, len(turn.offered))
	for name := range turn.offered {
		names = append(names, name)
	}
	dt.mu.Unlock()
	if len(names) == 0 {
		return
	}
	sort.Strings(names)
	if msg.Meta == nil {
		msg.Meta = map[string]interface{}{}
	}
	msg.Meta[types.MetaToolsOffered] = names
}

// splitUnsynced separates the calls to tools the session does not hold but
// the conversation offers, or holds but the conversation has withdrawn,
// answering each with a refusal, from the calls to run. A round whose calls
// are all refused skips the tool runner, so the identical-call breaker and
// the call budget do not count it; the round limit still does (each round's
// number comes from the provider call it answers), and refuseUnsyncedRound
// caps how many such batches a turn gets.
func (dt *duplexTools) splitUnsynced(
	calls []types.MessageToolCall,
) (run []types.MessageToolCall, refused []types.Message) {
	for _, tc := range calls {
		errMsg, unsynced := dt.offer.unsyncedRefusal(tc.Name)
		if !unsynced {
			run = append(run, tc)
			continue
		}
		result := types.NewTextToolResult(tc.ID, tc.Name, errMsg)
		result.Error = errMsg
		result.ErrorType = types.ToolErrorApproval
		refused = append(refused, types.NewToolResultMessage(result))
	}
	return run, refused
}
