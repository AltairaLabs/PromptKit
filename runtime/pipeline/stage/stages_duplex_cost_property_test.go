package stage

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A property test of realtime cost accounting: seeded random sessions of
// replies, tool-call turns, replies to tool results, barge-ins, late usage,
// usage-only chunks, user turns and blocked turns, shaped as Gemini Live and
// as OpenAI Realtime send them. Whatever the sequence:
//
//   - the provider call events' total (completed + usage) equals the spend,
//     which equals the messages' CostInfo plus what is carried at close with
//     no message after it;
//   - every call that started completed exactly once;
//   - no call is left open once the session has ended;
//   - a user turn's first call is round 1.

type costShape int

const (
	geminiShape costShape = iota
	openaiShape
)

func (c costShape) String() string { return [...]string{"gemini", "openai"}[c] }

// costScenario drives one random session.
type costScenario struct {
	t     *testing.T
	h     *realtimeHarness
	rng   *rand.Rand
	shape costShape
	gen   int64
	sent  int
	log   []string
	// userRounds are the indexes, in started order, of the first call after
	// each user turn.
	firstAfterUser []int
	// over is set once the session has ended (a guardrail denied a turn).
	over bool
	// expected is each completed generation's full text; audio its audio.
	expected map[int64]string
	audio    map[int64]string
	// ownMessage are generations whose call must report NoMessage=false;
	// noMessage those that must report true.
	ownMessage, noMessage []int64
	// sessionLevelOK counts the operations that may report usage with no
	// call (a forgotten generation; unnumbered usage before any call).
	sessionLevelOK int
	started        func() []*events.ProviderCallStartedData
}

func (sc *costScenario) cost() *types.CostInfo {
	return usd(1+sc.rng.Intn(500), 1+sc.rng.Intn(100))
}

func (sc *costScenario) next() int64 {
	sc.gen++
	return sc.gen
}

func (sc *costScenario) emit(op string, chunks []providers.StreamChunk) {
	sc.log = append(sc.log, op)
	sc.h.emit(chunks)
}

func (sc *costScenario) reply() {
	g := sc.next()
	sc.expect(g, tok(g))
	stop := "stop"
	sc.emit("reply", gen(g, []providers.StreamChunk{
		{Content: tok(g), Delta: tok(g)},
		{FinishReason: &stop, CostInfo: sc.cost()},
	}))
}

func (sc *costScenario) toolTurn() {
	g := sc.next()
	turn := toolCallTurn(weatherCall(fmt.Sprintf("c%d", g)))
	if sc.shape == openaiShape {
		turn[0].CostInfo = sc.cost() // OpenAI: response.done carries the cost
	}
	sc.emitTool(g, turn)
	if sc.shape == geminiShape {
		// Gemini 3: the tool-call turn's usage, after its tool calls (here
		// after the tool ran and the reply's call started).
		sc.emit("late-usage", gen(g, []providers.StreamChunk{{CostInfo: sc.cost()}}))
	}
	sc.reply()
	sc.turnEnded()
}

func (sc *costScenario) bargeIn() {
	g := sc.next()
	stop := "stop"
	if sc.shape == openaiShape {
		interrupted := types.FinishReasonInterrupted
		sc.emit("barge-mid", gen(g, []providers.StreamChunk{
			{Content: tok(g), Delta: tok(g)},
			{FinishReason: &interrupted, CostInfo: sc.cost()},
		}))
		return
	}
	chunks := []providers.StreamChunk{{Content: tok(g), Delta: tok(g)}, {Interrupted: true}}
	if sc.rng.Intn(2) == 0 {
		chunks[1].CostInfo = sc.cost()
	}
	chunks = append(chunks, providers.StreamChunk{FinishReason: &stop, CostInfo: sc.cost()})
	sc.emit("barge", gen(g, chunks))
}

func (sc *costScenario) playoutBargeIn() {
	if sc.gen == 0 {
		return // no reply yet to barge into
	}
	sc.emit("barge-playout", gen(sc.gen, []providers.StreamChunk{{Interrupted: true}}))
}

func (sc *costScenario) usageOnly() {
	g := sc.gen
	if g > 1 && sc.rng.Intn(2) == 0 {
		g -= int64(1 + sc.rng.Intn(int(g-1)))
	}
	if g == 0 {
		sc.sessionLevelOK++ // unnumbered, maybe before any call
	}
	sc.emit(fmt.Sprintf("usage-only(g%d)", g), []providers.StreamChunk{{CostInfo: sc.cost(), Generation: g}})
}

func (sc *costScenario) userTurn() {
	earlier := sc.h.toolTurn()
	if earlier != nil {
		return // a turn is under way: the user's turn would keep its call
	}
	sc.log = append(sc.log, "user")
	before := len(sc.started())
	user := types.Message{Role: core.RoleUser, Content: "next"}
	sc.h.input <- StreamElement{Message: &user}
	sc.awaitUserTurn()
	sc.firstAfterUser = append(sc.firstAfterUser, before)
}

func (sc *costScenario) blockedTurn() {
	sc.log = append(sc.log, "blocked")
	text := "blocked input"
	sc.h.input <- StreamElement{Text: &text}
}

// expect records a generation's full text, which exactly one emitted
// message must carry.
func (sc *costScenario) expect(g int64, text string) {
	if sc.expected == nil {
		sc.expected = map[int64]string{}
		sc.audio = map[int64]string{}
	}
	sc.expected[g] = text
}

// emitTool emits a tool-call turn and waits for its results to reach the
// session (unless a limit closed it, or it refused the calls).
func (sc *costScenario) emitTool(g int64, turn []providers.StreamChunk) {
	before := len(sc.h.sess().sentResponses())
	sc.emit("tool", gen(g, turn))
	assert.Eventually(sc.t, func() bool {
		return len(sc.h.sess().sentResponses()) > before || sc.h.sess().isClosed() || sc.h.stage.calls.isTerminal()
	}, 2*time.Second, time.Millisecond)
}

// userDuringReply sends a typed user turn while the reply to a tool result
// (round 2) streams. The reply goes on to completion: user input never ends
// a call. Then the reply to the user's turn.
func (sc *costScenario) userDuringReply() {
	g := sc.next()
	sc.emitTool(g, toolCallTurn(weatherCall(fmt.Sprintf("c%d", g))))
	r := sc.next()
	sc.emit("reply-streaming", gen(r, []providers.StreamChunk{{Content: tok(r), Delta: tok(r)}}))
	sc.settle()
	sc.log = append(sc.log, "user-mid-reply")
	user := types.Message{Role: core.RoleUser, Content: "actually, Rome"}
	sc.h.input <- StreamElement{Message: &user}
	sc.awaitUserTurnDue()
	stop := "stop"
	sc.emit("reply-rest", gen(r, []providers.StreamChunk{
		{Content: tok(r), Delta: tok(r)},
		{MediaData: &providers.StreamMediaData{Data: []byte(fmt.Sprintf("a%d", r)), MIMEType: "audio/pcm"}},
		{FinishReason: &stop, CostInfo: sc.cost()},
	}))
	sc.expect(r, tok(r)+tok(r))
	sc.audio[r] = fmt.Sprintf("a%d", r)
	sc.ownMessage = append(sc.ownMessage, r)
	sc.reply()
	sc.turnEnded()
}

// endOnly is a generation that only ends: no content, no cost.
func (sc *costScenario) endOnly() {
	g := sc.next()
	stop := "stop"
	sc.emit("end-only", []providers.StreamChunk{{FinishReason: &stop, Generation: g}})
	sc.noMessage = append(sc.noMessage, g)
}

// costFirst is a generation whose first chunk is its cost, then its reply.
func (sc *costScenario) costFirst() {
	g := sc.next()
	stop := "stop"
	sc.emit("cost-first", gen(g, []providers.StreamChunk{
		{CostInfo: sc.cost()},
		{Content: tok(g), Delta: tok(g)},
		{FinishReason: &stop, CostInfo: sc.cost()},
	}))
	sc.expect(g, tok(g))
	sc.ownMessage = append(sc.ownMessage, g)
	sc.turnEnded()
}

// bargeBeforeOutput: a user turn opens a call, and the provider reports an
// interruption tagged with the generation completed before it (it saw no new
// output), then replies.
func (sc *costScenario) bargeBeforeOutput() {
	if sc.h.toolTurn() != nil {
		return
	}
	user := types.Message{Role: core.RoleUser, Content: "hello?"}
	sc.h.input <- StreamElement{Message: &user}
	sc.awaitUserTurn()
	if sc.gen > 0 {
		chunk := providers.StreamChunk{Interrupted: true, Generation: sc.gen}
		if sc.shape == geminiShape && sc.rng.Intn(2) == 0 {
			chunk.CostInfo = sc.cost()
		}
		sc.emit("barge-before-output", []providers.StreamChunk{chunk})
	}
	sc.reply()
	sc.turnEnded()
}

// zeroTokenEnd ends a turn with a cost but no tokens and no content.
func (sc *costScenario) zeroTokenEnd() {
	g := sc.next()
	stop := "stop"
	sc.emit("zero-token-end", gen(g, []providers.StreamChunk{
		{Delta: "x", Metadata: map[string]interface{}{"type": "input_transcription", "transcription": "x"}},
		{FinishReason: &stop, CostInfo: &types.CostInfo{TotalCost: float64(1+sc.rng.Intn(9)) / 1000}},
	}))
	sc.turnEnded()
}

// deniedReply is a reply the output guardrail denies.
func (sc *costScenario) deniedReply() {
	g := sc.next()
	stop := "stop"
	sc.noMessage = append(sc.noMessage, g)
	sc.emit("denied-reply", gen(g, []providers.StreamChunk{
		{Content: "deny-me", Delta: "deny-me"},
		{FinishReason: &stop, CostInfo: sc.cost()},
	}))
	sc.over = true // a denied turn ends the session
}

// manyGenerations runs more replies than the stage remembers generations
// for, then reports usage for an early one.
func (sc *costScenario) manyGenerations() {
	early := sc.gen + 1
	for i := 0; i < maxTrackedGenerations+4; i++ {
		sc.reply()
	}
	sc.settle()
	sc.sessionLevelOK++
	sc.emit(fmt.Sprintf("usage-evicted(g%d)", early), []providers.StreamChunk{{CostInfo: sc.cost(), Generation: early}})
}

// trailingCancelled: OpenAI cancels a response, starts the next, and the
// cancelled one's deltas and a tool call trail in after the next began.
func (sc *costScenario) trailingCancelled() {
	g := sc.next()
	interrupted := types.FinishReasonInterrupted
	sc.emit("cancelled", gen(g, []providers.StreamChunk{
		{Content: tok(g), Delta: tok(g)},
		{FinishReason: &interrupted, CostInfo: sc.cost()},
	}))
	n := sc.next()
	sc.emit("next-starts", gen(n, []providers.StreamChunk{{Content: tok(n), Delta: tok(n)}}))
	sc.emit("trailing", gen(g, []providers.StreamChunk{
		{Content: tok(g), Delta: tok(g)},
		{ToolCalls: []types.MessageToolCall{weatherCall(fmt.Sprintf("late%d", g))}},
	}))
	stop := "stop"
	sc.emit("next-ends", gen(n, []providers.StreamChunk{{FinishReason: &stop, CostInfo: sc.cost()}}))
	sc.turnEnded()
}

// awaitUserTurn waits for the user's turn to begin its tool turn.
func (sc *costScenario) awaitUserTurn() {
	ct := sc.h.stage.calls
	ok := assert.Eventuallyf(sc.t, func() bool { return sc.h.toolTurn() != nil || ct.isTerminal() },
		2*time.Second, time.Millisecond, "the user turn began no tool turn: %v", sc.log)
	if !ok {
		ct.mu.Lock()
		sc.t.Logf("open=%+v turn=%v round=%d terminal=%v", ct.open, ct.turn, ct.round, ct.terminal)
		ct.mu.Unlock()
	}
}

// awaitUserTurnDue waits for a user turn sent while a call is under way to
// be taken in: the call goes on, and the next generation's call starts the
// user's tool turn.
func (sc *costScenario) awaitUserTurnDue() {
	ct := sc.h.stage.calls
	assert.Eventuallyf(sc.t, func() bool {
		ct.mu.Lock()
		defer ct.mu.Unlock()
		return ct.newTurnDue || ct.terminal || sc.h.sess().isClosed()
	}, 2*time.Second, time.Millisecond, "the user turn was never taken in: %v", sc.log)
}

// turnEnded checks that once a turn has ended no call and no tool turn is
// left open.
func (sc *costScenario) turnEnded() {
	ct := sc.h.stage.calls
	if sc.h.sess().isClosed() || ct.isTerminal() {
		return // a spent budget is closing the session
	}
	if !assert.Eventuallyf(sc.t, func() bool {
		ct.mu.Lock()
		defer ct.mu.Unlock()
		return (ct.open == nil && ct.turn == nil) || ct.terminal
	}, 2*time.Second, time.Millisecond, "a call or tool turn left open after a turn end: %v", sc.log) {
		ct.mu.Lock()
		sc.t.Logf("open=%+v turn=%v round=%d", ct.open, ct.turn != nil, ct.round)
		ct.mu.Unlock()
	}
}

// settle waits for the stage to handle everything emitted so far.
func (sc *costScenario) settle() {
	sc.h.settle()
}

// costTotals are a session's three accounts of what it cost.
type costTotals struct {
	events, spend, history float64
	log                    []string
}

func runCostScenario(t *testing.T, shape costShape, seed int64) costTotals {
	emitter, ev := newCallEvents()
	startedSnapshot := func() []*events.ProviderCallStartedData {
		ev.mu.Lock()
		defer ev.mu.Unlock()
		return append([]*events.ProviderCallStartedData(nil), ev.started...)
	}
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	opts := realtimeOpts{registry: newWeatherRegistry(t, exec), emitter: emitter, hookReg: propertyHooks(),
		timeout: time.Minute, outputBuffer: 1 << 14}
	if seed%4 == 0 {
		// A budget spent mid-session: the session goes terminal, and what it
		// still reports is accounted.
		opts.policy = &pipeline.ToolPolicy{MaxCostUSD: 0.8}
	}
	h := startRealtime(t, opts)
	sc := &costScenario{t: t, h: h, rng: rand.New(rand.NewSource(seed)), shape: shape, started: startedSnapshot}

	ops := []func(){sc.reply, sc.toolTurn, sc.bargeIn, sc.playoutBargeIn, sc.usageOnly, sc.userTurn, sc.blockedTurn,
		sc.userDuringReply, sc.bargeBeforeOutput, sc.zeroTokenEnd, sc.deniedReply, sc.manyGenerations,
		sc.endOnly, sc.costFirst}
	if shape == openaiShape {
		ops = append(ops, sc.trailingCancelled)
	}
	for i, n := 0, 4+sc.rng.Intn(8); i < n && !sc.over; i++ {
		ops[sc.rng.Intn(len(ops))]()
		sc.settle()
		if os.Getenv("COSTDEBUG") != "" {
			ct := h.stage.calls
			ct.mu.Lock()
			t.Logf("%v: open=%v turn=%v round=%d terminal=%v spent=%.3f", sc.log, ct.open != nil, ct.turn != nil,
				ct.round, ct.terminal, ct.spent)
			ct.mu.Unlock()
		}
	}
	elems := h.finish()
	label := fmt.Sprintf("%s seed %d %v", shape, seed, sc.log)
	ev.mu.Lock()
	toolStarts := append([]*events.ToolCallEventData(nil), ev.toolStarts...)
	ev.mu.Unlock()
	assertOwnContent(t, elems, toolStarts, label)
	sc.assertDelivered(elems, ev, label)

	var messageCost float64
	for _, e := range elems {
		if e.Message != nil && e.Message.CostInfo != nil {
			messageCost += e.Message.CostInfo.TotalCost
		}
	}
	remainder := 0.0
	if h.stage.carriedCost != nil {
		remainder = h.stage.carriedCost.TotalCost
	}
	spend := h.spent()

	var done []*events.ProviderCallCompletedData
	var usage []*events.ProviderCallUsageData
	var eventCost float64
	snapshot := func() (int, float64) {
		nStarted := len(startedSnapshot())
		ev.mu.Lock()
		defer ev.mu.Unlock()
		done = append([]*events.ProviderCallCompletedData(nil), ev.done...)
		usage = append([]*events.ProviderCallUsageData(nil), ev.usage...)
		eventCost = 0
		for _, d := range done {
			eventCost += d.Cost
		}
		for _, d := range usage {
			eventCost += d.Cost
		}
		return nStarted, eventCost
	}
	assert.Eventually(t, func() bool {
		n, c := snapshot()
		return len(done) >= n && abs(c-spend) < 1e-9
	}, 2*time.Second, time.Millisecond)
	snapshot()
	assert.InDeltaf(t, spend, eventCost, 1e-9, "%s seed %d %v: events (%d completed, %d usage) == spend",
		shape, seed, sc.log, len(done), len(usage))
	assert.InDeltaf(t, spend, messageCost+remainder, 1e-9,
		"%s seed %d %v: spend %.6f == messages %.6f + remainder %.6f", shape, seed, sc.log, spend, messageCost, remainder)

	startedIDs := map[string]int{}
	for _, d := range startedSnapshot() {
		startedIDs[d.CallID]++
	}
	completedIDs := map[string]int{}
	for _, d := range done {
		completedIDs[d.CallID]++
	}
	assert.Equalf(t, startedIDs, completedIDs, "%s seed %d %v: every started call completes once", shape, seed, sc.log)
	for id, n := range startedIDs {
		assert.Equalf(t, 1, n, "%s seed %d: call %s started %d times", shape, seed, id, n)
	}
	h.stage.calls.mu.Lock()
	assert.Nilf(t, h.stage.calls.open, "%s seed %d %v: a call left open", shape, seed, sc.log)
	h.stage.calls.mu.Unlock()
	all := startedSnapshot()
	for _, i := range sc.firstAfterUser {
		if i < len(all) { // the user's turn starts with the next call
			assert.Equalf(t, 1, all[i].Round, "%s seed %d %v: a user turn's first call is round 1", shape, seed, sc.log)
		}
	}
	return costTotals{events: eventCost, spend: spend, history: messageCost + remainder, log: sc.log}
}

var propertyHooksOn = true

func propertyHooks() *hooks.Registry {
	if !propertyHooksOn {
		return nil
	}
	return hooks.NewRegistry(
		hooks.WithProviderHook(&enforceBeforeCallHook{replacement: "Not here."}),
		hooks.WithProviderHook(denyReplyHook{}),
	)
}

// denyReplyHook is an output guardrail that denies a completed reply saying
// "deny-me".
type denyReplyHook struct{}

func (denyReplyHook) Name() string { return "deny_reply" }

func (denyReplyHook) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}

func (denyReplyHook) AfterCall(_ context.Context, _ *hooks.ProviderRequest, resp *hooks.ProviderResponse) hooks.Decision {
	if resp != nil && resp.Message.Content == "deny-me" {
		return hooks.Deny("no")
	}
	return hooks.Allow
}

// assertDelivered asserts each completed generation's full text is in
// exactly one emitted message and its audio was delivered; that calls whose
// message was emitted report NoMessage=false and those with none true; and
// that usage names no call only where the scenario allows it.
func (sc *costScenario) assertDelivered(elems []StreamElement, ev *callEvents, label string) {
	t := sc.t
	ct := sc.h.stage.calls
	if ct.isTerminal() || sc.over {
		return // the session closed early: later output was never read
	}
	for g, want := range sc.expected {
		n := 0
		for _, e := range elems {
			if e.Message != nil && e.Message.Role == core.RoleAssistant && e.Message.Content == want {
				n++
			}
		}
		assert.Equalf(t, 1, n, "%s: generation %d's text %q is in %d messages", label, g, want, n)
	}
	for g, want := range sc.audio {
		found := false
		for _, e := range elems {
			if e.Audio != nil && string(e.Audio.Samples) == want {
				found = true
			}
		}
		assert.Truef(t, found, "%s: generation %d's audio was not delivered", label, g)
	}
	ev.mu.Lock()
	done := append([]*events.ProviderCallCompletedData(nil), ev.done...)
	usage := append([]*events.ProviderCallUsageData(nil), ev.usage...)
	ev.mu.Unlock()
	noMessageOf := func(g int64) (bool, bool) {
		ct.mu.Lock()
		r := ct.gens[g]
		ct.mu.Unlock()
		if r == nil {
			return false, false
		}
		for _, d := range done {
			if d.CallID == r.rr.providerCallID {
				return d.NoMessage, true
			}
		}
		return false, false
	}
	for _, g := range sc.ownMessage {
		if nm, ok := noMessageOf(g); ok {
			assert.Falsef(t, nm, "%s: generation %d's message was emitted but its call reports NoMessage", label, g)
		}
	}
	for _, g := range sc.noMessage {
		if nm, ok := noMessageOf(g); ok {
			assert.Truef(t, nm, "%s: generation %d emitted no message but its call reports one", label, g)
		}
	}
	sessionLevel := 0
	for _, u := range usage {
		if u.CallID == "" {
			sessionLevel++
		}
	}
	assert.LessOrEqualf(t, sessionLevel, sc.sessionLevelOK, "%s: usage with no call", label)
}

// tok is a generation's content token: a message whose text mixes tokens
// carries another generation's content.
func tok(g int64) string { return fmt.Sprintf("g%d|", g) }

// callGen is the generation a tool call ID names: c<g> or late<g>.
func callGen(id string) string {
	return strings.TrimLeft(id, "cdelat")
}

// assertOwnContent asserts every emitted assistant message carries only its
// own generation's text and tool calls, and that each provider call ran only
// its own generation's tool calls, and never a canceled response's.
func assertOwnContent(t *testing.T, elems []StreamElement, tools []*events.ToolCallEventData, label string) {
	t.Helper()
	for _, e := range elems {
		m := e.Message
		if m == nil || m.Role != core.RoleAssistant {
			continue
		}
		gens := map[string]bool{}
		for _, part := range strings.Split(m.Content, "|") {
			if strings.HasPrefix(part, "g") {
				gens[strings.TrimPrefix(part, "g")] = true
			}
		}
		for _, tc := range m.ToolCalls {
			gens[callGen(tc.ID)] = true
		}
		assert.LessOrEqualf(t, len(gens), 1, "%s: a message mixes generations: %q %v", label, m.Content, m.ToolCalls)
	}
	perCall := map[string]map[string]bool{}
	for _, d := range tools {
		assert.Falsef(t, strings.HasPrefix(d.CallID, "late"), "%s: a canceled response's tool call ran", label)
		if perCall[d.ProviderCallID] == nil {
			perCall[d.ProviderCallID] = map[string]bool{}
		}
		perCall[d.ProviderCallID][callGen(d.CallID)] = true
	}
	for call, gens := range perCall {
		assert.LessOrEqualf(t, len(gens), 1, "%s: call %s ran tool calls of several generations", label, call)
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func TestDuplexCost_Property(t *testing.T) {
	propertyHooksOn = os.Getenv("NOHOOKS") == ""
	for _, shape := range []costShape{geminiShape, openaiShape} {
		for seed := int64(1); seed <= 25; seed++ {
			t.Run(fmt.Sprintf("%s/%d", shape, seed), func(t *testing.T) {
				got := runCostScenario(t, shape, seed)
				assert.Positive(t, got.spend, "the session cost something: %v", got.log)
				assert.InDelta(t, got.spend, got.events, 1e-9, "events == spend: %v", got.log)
				assert.InDelta(t, got.spend, got.history, 1e-9, "messages + remainder == spend: %v", got.log)
			})
		}
	}
}
