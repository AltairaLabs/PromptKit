package stage

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
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
	stop := "stop"
	sc.emit("reply", gen(g, []providers.StreamChunk{
		{Content: "hi", Delta: "hi"},
		{FinishReason: &stop, CostInfo: sc.cost()},
	}))
}

func (sc *costScenario) toolTurn() {
	g := sc.next()
	turn := toolCallTurn(weatherCall(fmt.Sprintf("c%d", g)))
	if sc.shape == openaiShape {
		turn[0].CostInfo = sc.cost() // OpenAI: response.done carries the cost
	}
	sc.emit("tool", gen(g, turn))
	sc.sent++
	sc.h.awaitSent(sc.sent)
	if sc.shape == geminiShape {
		// Gemini 3: the tool-call turn's usage, after its tool calls (here
		// after the tool ran and the reply's call started).
		sc.emit("late-usage", gen(g, []providers.StreamChunk{{CostInfo: sc.cost()}}))
	}
	sc.reply()
}

func (sc *costScenario) bargeIn() {
	g := sc.next()
	stop := "stop"
	if sc.shape == openaiShape {
		interrupted := types.FinishReasonInterrupted
		sc.emit("barge-mid", gen(g, []providers.StreamChunk{
			{Content: "once", Delta: "once"},
			{FinishReason: &interrupted, CostInfo: sc.cost()},
		}))
		return
	}
	chunks := []providers.StreamChunk{{Content: "once", Delta: "once"}, {Interrupted: true}}
	if sc.rng.Intn(2) == 0 {
		chunks[1].CostInfo = sc.cost()
	}
	chunks = append(chunks, providers.StreamChunk{FinishReason: &stop, CostInfo: sc.cost()})
	sc.emit("barge", gen(g, chunks))
}

func (sc *costScenario) playoutBargeIn() {
	sc.emit("barge-playout", gen(sc.gen, []providers.StreamChunk{{Interrupted: true}}))
}

func (sc *costScenario) usageOnly() {
	g := sc.gen
	if g > 1 && sc.rng.Intn(2) == 0 {
		g -= int64(1 + sc.rng.Intn(int(g-1)))
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
	user := types.Message{Role: roleUser, Content: "next"}
	sc.h.input <- StreamElement{Message: &user}
	sc.h.awaitNewToolTurn(nil)
	assert.Eventually(sc.t, func() bool { return len(sc.started()) > before }, 2*time.Second, time.Millisecond)
	sc.firstAfterUser = append(sc.firstAfterUser, before)
}

func (sc *costScenario) blockedTurn() {
	sc.log = append(sc.log, "blocked")
	text := "blocked input"
	sc.h.input <- StreamElement{Text: &text}
}

// settle waits for the stage to handle everything emitted so far.
func (sc *costScenario) settle() {
	time.Sleep(5 * time.Millisecond)
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
	h := startRealtime(t, realtimeOpts{
		registry: newWeatherRegistry(t, exec),
		emitter:  emitter,
		hookReg:  propertyHooks(),
	})
	sc := &costScenario{t: t, h: h, rng: rand.New(rand.NewSource(seed)), shape: shape, started: startedSnapshot}

	ops := []func(){sc.reply, sc.toolTurn, sc.bargeIn, sc.playoutBargeIn, sc.usageOnly, sc.userTurn, sc.blockedTurn}
	for i, n := 0, 4+sc.rng.Intn(8); i < n; i++ {
		ops[sc.rng.Intn(len(ops))]()
		sc.settle()
	}
	elems := h.finish()

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
		"%s seed %d %v: spend == messages + remainder", shape, seed, sc.log)

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
	h.stage.tools.mu.Lock()
	assert.Nilf(t, h.stage.tools.open, "%s seed %d %v: a call left open", shape, seed, sc.log)
	h.stage.tools.mu.Unlock()
	all := startedSnapshot()
	for _, i := range sc.firstAfterUser {
		assert.Equalf(t, 1, all[i].Round, "%s seed %d %v: a user turn's first call is round 1", shape, seed, sc.log)
	}
	return costTotals{events: eventCost, spend: spend, history: messageCost + remainder, log: sc.log}
}

var propertyHooksOn = true

func propertyHooks() *hooks.Registry {
	if !propertyHooksOn {
		return nil
	}
	return hooks.NewRegistry(hooks.WithProviderHook(&enforceBeforeCallHook{replacement: "Not here."}))
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
