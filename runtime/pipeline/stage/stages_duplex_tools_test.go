package stage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realtimeToolProvider is the mock streaming provider with its sessions
// wrapped so the assistant turn that follows each batch of tool responses is
// scripted, and so a tool response sent after Close is counted.
type realtimeToolProvider struct {
	*providersmock.StreamingProvider
	script  [][]providers.StreamChunk
	onClose func()
	// replyDelay is how long the session takes to start each scripted reply.
	replyDelay time.Duration
	// updatable makes the sessions providers.StreamToolUpdaters.
	updatable bool
	// failUpdates makes them StreamToolUpdaters whose updates fail.
	failUpdates bool
	// partsSupport makes them providers.ToolResponsePartsSupport sessions.
	partsSupport bool

	mu      sync.Mutex
	sess    *realtimeToolSession
	created []providers.StreamingInputConfig
}

func (p *realtimeToolProvider) CreateStreamSession(
	ctx context.Context, req *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	inner, err := p.StreamingProvider.CreateStreamSession(ctx, req)
	if err != nil {
		return nil, err
	}
	s := &realtimeToolSession{
		MockStreamSession: inner.(*providersmock.MockStreamSession),
		script:            p.script,
		onClose:           p.onClose,
		replyDelay:        p.replyDelay,
	}
	p.mu.Lock()
	p.sess = s
	p.created = append(p.created, *req)
	p.mu.Unlock()
	if p.failUpdates {
		return &failingUpdateSession{realtimeToolSession: s}, nil
	}
	if p.partsSupport {
		return &partsToolSession{realtimeToolSession: s}, nil
	}
	if p.updatable {
		return &updatingToolSession{realtimeToolSession: s}, nil
	}
	return s, nil
}

// createdWith returns the configs the provider's sessions were created with.
func (p *realtimeToolProvider) createdWith() []providers.StreamingInputConfig {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]providers.StreamingInputConfig(nil), p.created...)
}

func (p *realtimeToolProvider) session() *realtimeToolSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sess
}

// realtimeToolSession records the tool responses it is sent, and answers each
// batch with the next scripted assistant turn.
type realtimeToolSession struct {
	*providersmock.MockStreamSession
	onClose    func()
	closeOnce  sync.Once
	replyDelay time.Duration

	mu             sync.Mutex
	closed         bool
	sent           [][]providers.ToolResponse
	sentAfterClose int
	script         [][]providers.StreamChunk
	// updates are the tool lists UpdateTools was called with (an updating
	// session only); log is "update" and "responses" in the order they came.
	updates [][]providers.StreamingToolDefinition
	log     []string
}

// updatingToolSession is a realtimeToolSession whose tools can be changed.
type updatingToolSession struct {
	*realtimeToolSession
}

func (s *updatingToolSession) UpdateTools(_ context.Context, defs []providers.StreamingToolDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates = append(s.updates, defs)
	s.log = append(s.log, "update")
	return nil
}

// partsToolSession is a realtimeToolSession that can send media parts.
type partsToolSession struct {
	*realtimeToolSession
}

func (s *partsToolSession) SupportsToolResponseParts() bool { return true }

// toolUpdates returns the tool lists the session was updated with, and the
// order updates and tool responses reached it in.
func (s *realtimeToolSession) toolUpdates() (updates [][]providers.StreamingToolDefinition, log []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]providers.StreamingToolDefinition(nil), s.updates...), append([]string(nil), s.log...)
}

func (s *realtimeToolSession) SendToolResponse(ctx context.Context, id, result string) error {
	return s.SendToolResponses(ctx, []providers.ToolResponse{{ToolCallID: id, Result: result}})
}

func (s *realtimeToolSession) SendToolResponses(_ context.Context, resp []providers.ToolResponse) error {
	s.mu.Lock()
	if s.closed {
		s.sentAfterClose++
		s.mu.Unlock()
		return errors.New("session closed")
	}
	var turn []providers.StreamChunk
	if len(s.script) > 0 {
		turn, s.script = s.script[0], s.script[1:]
	}
	s.mu.Unlock()
	// The scripted reply is on the response channel before the batch shows
	// as sent, so a chunk a test emits after awaitSent follows it.
	if len(turn) > 0 {
		time.Sleep(s.replyDelay)
	}
	for i := range turn {
		s.EmitChunk(&turn[i])
	}
	s.mu.Lock()
	s.sent = append(s.sent, resp)
	s.log = append(s.log, "responses")
	s.mu.Unlock()
	return nil
}

func (s *realtimeToolSession) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.closeOnce.Do(func() {
		if s.onClose != nil {
			s.onClose()
		}
	})
	return s.MockStreamSession.Close()
}

func (s *realtimeToolSession) sentResponses() [][]providers.ToolResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]providers.ToolResponse(nil), s.sent...)
}

func (s *realtimeToolSession) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *realtimeToolSession) sendsAfterClose() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sentAfterClose
}

// realtimeToolExec is the registry's "local" executor: it records each call,
// tracks how many run at once, and can hold calls on a gate.
type realtimeToolExec struct {
	result json.RawMessage
	gate   chan struct{} // when set, each call waits for it to close
	// overlap, when set, holds each call until another is running too (or it
	// expires), so calls allowed to run in parallel are seen to.
	overlap time.Duration
	// delay, when set, is how long each call takes.
	delay time.Duration
	// outputLen, when set, is read at the start of each call.
	outputLen func() int

	mu          sync.Mutex
	names       []string
	args        []string
	seenOutput  []int
	inflight    int
	maxInflight int
	finished    int
}

func (e *realtimeToolExec) Name() string { return "local" }

func (e *realtimeToolExec) Execute(
	ctx context.Context, d *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	e.mu.Lock()
	e.names = append(e.names, d.Name)
	e.args = append(e.args, string(args))
	if e.outputLen != nil {
		e.seenOutput = append(e.seenOutput, e.outputLen())
	}
	e.inflight++
	e.maxInflight = max(e.maxInflight, e.inflight)
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.inflight--
		e.finished++
		e.mu.Unlock()
	}()

	if e.overlap > 0 {
		deadline := time.Now().Add(e.overlap)
		for time.Now().Before(deadline) {
			e.mu.Lock()
			both := e.inflight >= 2
			e.mu.Unlock()
			if both {
				break
			}
			time.Sleep(time.Millisecond)
		}
	}
	if e.delay > 0 {
		time.Sleep(e.delay)
	}
	if e.gate != nil {
		select {
		case <-e.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return e.result, nil
}

func (e *realtimeToolExec) calls() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.names...)
}

// snapshot returns the recorded arguments, the output lengths seen, and the
// most calls that ran at once.
func (e *realtimeToolExec) snapshot() (args []string, seenOutput []int, maxInflight int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.args...), append([]int(nil), e.seenOutput...), e.maxInflight
}

func (e *realtimeToolExec) done() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.finished
}

// realtimeHarness runs a DuplexProviderStage against a realtimeToolProvider.
// Output is buffered and read only at the end, so its order is the order the
// stage emitted it in.
type realtimeHarness struct {
	t        *testing.T
	provider *realtimeToolProvider
	stage    *DuplexProviderStage
	input    chan StreamElement
	output   chan StreamElement
	cancel   context.CancelFunc
	done     chan error
}

type realtimeOpts struct {
	registry   *tools.Registry
	policy     *pipeline.ToolPolicy
	cfg        DuplexToolConfig
	emitter    *events.Emitter
	script     [][]providers.StreamChunk
	onClose    func()
	replyDelay time.Duration
	noToolSet  bool // leave SetToolExecution uncalled
	resolver   WorkflowStateResolver
	// sessionTools are the tools the session is offered.
	sessionTools []providers.StreamingToolDefinition
	// history is a resumed conversation's prior messages.
	history []types.Message
	// turnState is the stage's turn state (allowed tools, prompt); nil for none.
	turnState *TurnState
	// updatable makes the session a providers.StreamToolUpdater.
	updatable bool
	// failUpdates makes the session a StreamToolUpdater whose updates fail.
	failUpdates bool
	// hostToolChoice is the host's StreamingInputConfig.ToolChoice.
	hostToolChoice string
	// partsSupport makes the session a providers.ToolResponsePartsSupport.
	partsSupport bool
	// hookReg is the stage's hook registry; nil for none.
	hookReg *hooks.Registry
	// finalWait and toolWaitLimit replace the stage's waits once input
	// closes; zero keeps the defaults.
	finalWait, toolWaitLimit time.Duration
}

func startRealtime(t *testing.T, o realtimeOpts) *realtimeHarness {
	t.Helper()
	p := &realtimeToolProvider{
		StreamingProvider: providersmock.NewStreamingProvider("rt", "rt-model", false),
		script:            o.script,
		onClose:           o.onClose,
		replyDelay:        o.replyDelay,
		updatable:         o.updatable,
		failUpdates:       o.failUpdates,
		partsSupport:      o.partsSupport,
	}
	cfg := baseConfig()
	cfg.Tools = o.sessionTools
	cfg.ToolChoice = o.hostToolChoice
	st := NewDuplexProviderStageWithTurnState(p, cfg, o.emitter, o.turnState)
	if o.hookReg != nil {
		st.SetHookRegistry(o.hookReg)
	}
	if o.finalWait > 0 {
		st.finalWait = o.finalWait
	}
	if o.toolWaitLimit > 0 {
		st.toolWaitLimit = o.toolWaitLimit
	}
	if !o.noToolSet {
		st.SetToolExecution(o.registry, o.policy, o.cfg, o.resolver)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	h := &realtimeHarness{
		t: t, provider: p, stage: st, cancel: cancel,
		input:  make(chan StreamElement, 16),
		output: make(chan StreamElement, 256),
		done:   make(chan error, 1),
	}
	t.Cleanup(cancel)
	for i := range o.history {
		h.input <- StreamElement{Message: &o.history[i], Meta: ElementMetadata{FromHistory: true}}
	}
	h.input <- StreamElement{EndOfStream: true}
	go func() { h.done <- st.Process(ctx, h.input, h.output) }()
	require.Eventually(t, func() bool { return p.session() != nil },
		2*time.Second, time.Millisecond, "session was never created")
	return h
}

func (h *realtimeHarness) sess() *realtimeToolSession { return h.provider.session() }

// emit plays one assistant turn on the session.
func (h *realtimeHarness) emit(turn []providers.StreamChunk) {
	for i := range turn {
		h.sess().EmitChunk(&turn[i])
	}
}

// awaitSent waits until the session has been sent n batches of tool
// responses and returns them.
func (h *realtimeHarness) awaitSent(n int) [][]providers.ToolResponse {
	h.t.Helper()
	require.Eventually(h.t, func() bool { return len(h.sess().sentResponses()) >= n },
		2*time.Second, time.Millisecond, "expected %d tool response batches", n)
	return h.sess().sentResponses()
}

// finish ends the session from the provider side and returns everything the
// stage emitted.
func (h *realtimeHarness) finish() []StreamElement {
	h.t.Helper()
	_ = h.sess().MockStreamSession.Close()
	elems := drainUntilClosed(h.t, h.output)
	<-h.done
	return elems
}

func toolCallTurn(calls ...types.MessageToolCall) []providers.StreamChunk {
	fr := "tool_calls"
	return []providers.StreamChunk{{ToolCalls: calls, FinishReason: &fr}}
}

func textTurn(text string, cost *types.CostInfo) []providers.StreamChunk {
	fr := "stop"
	return []providers.StreamChunk{{Content: text, Delta: text, FinishReason: &fr, CostInfo: cost}}
}

func weatherCall(id string) types.MessageToolCall {
	return types.MessageToolCall{ID: id, Name: "weather", Args: json.RawMessage(`{"city":"Paris"}`)}
}

func newWeatherRegistry(t *testing.T, exec *realtimeToolExec) *tools.Registry {
	t.Helper()
	reg := registryWithTools(t, "weather")
	reg.RegisterExecutor(exec)
	return reg
}

// toolResultMessages returns the tool result messages in elems, in order.
func toolResultMessages(elems []StreamElement) []types.MessageToolResult {
	var out []types.MessageToolResult
	for _, e := range elems {
		if e.Message != nil && e.Message.ToolResult != nil {
			out = append(out, *e.Message.ToolResult)
		}
	}
	return out
}

// streamErrors returns the errors carried by error elements in elems.
func streamErrors(elems []StreamElement) []error {
	var out []error
	for _, e := range elems {
		if e.Error != nil {
			out = append(out, e.Error)
		}
	}
	return out
}

const weatherResult = "{\n  \"temp\": 21\n}"

func TestDuplexTools_ExecutesCallAndSendsRegistryResult(t *testing.T) {
	bus := events.NewEventBus()
	var evMu sync.Mutex
	var evs []*events.Event
	bus.SubscribeAll(func(e *events.Event) {
		switch e.Type {
		case events.EventProviderCallStarted, events.EventProviderCallCompleted,
			events.EventToolCallStarted, events.EventToolCallCompleted:
			evMu.Lock()
			evs = append(evs, e)
			evMu.Unlock()
		}
	})
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	var h *realtimeHarness
	exec.outputLen = func() int { return len(h.output) }
	h = startRealtime(t, realtimeOpts{
		registry: newWeatherRegistry(t, exec),
		emitter:  events.NewEmitter(bus, "run", "sess", "conv"),
		script:   [][]providers.StreamChunk{textTurn("It is 21 degrees", nil)},
	})

	h.emit(toolCallTurn(weatherCall("c1")))

	sent := h.awaitSent(1)
	assert.Equal(t, [][]providers.ToolResponse{{{ToolCallID: "c1", Result: weatherResult}}}, sent)
	assert.Equal(t, []string{"weather"}, exec.calls())
	args, seenOutput, _ := exec.snapshot()
	assert.Equal(t, []string{`{"city":"Paris"}`}, args)

	require.Eventually(t, func() bool {
		evMu.Lock()
		defer evMu.Unlock()
		return len(evs) >= 6
	}, 2*time.Second, time.Millisecond)
	elems := h.finish()

	// Output: the assistant message with the call (out before the tool ran),
	// the call's result, then the reply the result produced.
	require.Len(t, elems, 3)
	require.NotNil(t, elems[0].Message)
	assert.Equal(t, roleAssistant, elems[0].Message.Role)
	assert.Equal(t, []types.MessageToolCall{weatherCall("c1")}, elems[0].Message.ToolCalls)
	assert.Equal(t, []int{1}, seenOutput, "the assistant element was emitted before the call ran")
	require.NotNil(t, elems[1].Message)
	assert.Equal(t, "tool", elems[1].Message.Role)
	require.NotNil(t, elems[2].Message)
	assert.Equal(t, roleAssistant, elems[2].Message.Role)
	assert.Equal(t, "It is 21 degrees", elems[2].Message.Content)

	results := toolResultMessages(elems)
	require.Len(t, results, 1)
	assert.Equal(t, "c1", results[0].ID)
	assert.Equal(t, "weather", results[0].Name)
	assert.Equal(t, weatherResult, results[0].GetTextContent())

	// Events: round 1's provider call, its tool call, then round 2's provider
	// call, which only follows the tool responses reaching the session.
	evMu.Lock()
	defer evMu.Unlock()
	type step struct {
		Type  events.EventType
		Round int
	}
	var got []step
	var callID string
	startedIDs := map[int]string{}
	for _, e := range evs {
		switch d := e.Data.(type) {
		case *events.ProviderCallStartedData:
			got = append(got, step{e.Type, d.Round})
			startedIDs[d.Round] = d.CallID
		case *events.ProviderCallCompletedData:
			assert.Equal(t, startedIDs[d.Round], d.CallID, "a round's started and completed name one call")
			got = append(got, step{e.Type, d.Round})
			if d.Round == 1 {
				callID = d.CallID
			}
		case *events.ToolCallEventData:
			got = append(got, step{e.Type, d.Round})
			assert.Equal(t, "c1", d.CallID)
			assert.Equal(t, callID, d.ProviderCallID, "the tool call names the provider call that made it")
		}
	}
	assert.Equal(t, []step{
		{events.EventProviderCallStarted, 1},
		{events.EventProviderCallCompleted, 1},
		{events.EventToolCallStarted, 1},
		{events.EventToolCallCompleted, 1},
		{events.EventProviderCallStarted, 2},
		{events.EventProviderCallCompleted, 2},
	}, got)
	assert.NotEmpty(t, callID)
}

func TestDuplexTools_BlocklistedToolIsRefused(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	h := startRealtime(t, realtimeOpts{
		registry: newWeatherRegistry(t, exec),
		policy:   &pipeline.ToolPolicy{Blocklist: []string{"weather"}},
	})

	h.emit(toolCallTurn(weatherCall("c1")))

	sent := h.awaitSent(1)
	assert.Equal(t, [][]providers.ToolResponse{{{
		ToolCallID: "c1", Result: "Tool weather is blocked by policy", IsError: true,
	}}}, sent)
	elems := h.finish()
	assert.Empty(t, exec.calls(), "a blocklisted tool never runs")
	results := toolResultMessages(elems)
	require.Len(t, results, 1)
	assert.Equal(t, "Tool weather is blocked by policy", results[0].Error)
}

func TestDuplexTools_ParallelLimit(t *testing.T) {
	cases := []struct {
		name   string
		policy *pipeline.ToolPolicy
		want   int
	}{
		{name: "default runs calls in parallel", want: 2},
		{name: "MaxParallelToolCalls 1 serializes", policy: &pipeline.ToolPolicy{MaxParallelToolCalls: 1}, want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec := &realtimeToolExec{result: json.RawMessage(`"ok"`), overlap: 300 * time.Millisecond}
			h := startRealtime(t, realtimeOpts{registry: newWeatherRegistry(t, exec), policy: tc.policy})

			h.emit(toolCallTurn(weatherCall("c1"), weatherCall("c2")))

			sent := h.awaitSent(1)
			assert.Equal(t, [][]providers.ToolResponse{{
				{ToolCallID: "c1", Result: "ok"}, {ToolCallID: "c2", Result: "ok"},
			}}, sent)
			h.finish()
			_, _, maxInflight := exec.snapshot()
			assert.Equal(t, tc.want, maxInflight)
		})
	}
}

func TestDuplexTools_MaxRoundsRefusesUntilTheNextTurn(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	h := startRealtime(t, realtimeOpts{
		registry: newWeatherRegistry(t, exec),
		policy:   &pipeline.ToolPolicy{MaxRounds: 2},
		script: [][]providers.StreamChunk{
			toolCallTurn(weatherCall("c2")), // round 2: runs, and reaches the limit
			toolCallTurn(weatherCall("c3")), // round 3: refused
			textTurn("giving up", nil),
		},
	})

	h.emit(toolCallTurn(weatherCall("c1")))
	sent := h.awaitSent(3)
	refusal := "Tool weather not executed: the turn's tool calls were stopped: provider stage: max rounds (2) exceeded"
	assert.Equal(t, [][]providers.ToolResponse{
		{{ToolCallID: "c1", Result: weatherResult}},
		{{ToolCallID: "c2", Result: weatherResult}},
		{{ToolCallID: "c3", Result: refusal, IsError: true}},
	}, sent)
	assert.Equal(t, []string{"weather", "weather"}, exec.calls(), "the refused call never ran")

	// The next user turn starts a new tool turn: its call runs.
	user := types.Message{Role: roleUser, Content: "and tomorrow?"}
	h.input <- StreamElement{Message: &user}
	h.emit(toolCallTurn(weatherCall("c4")))
	sent = h.awaitSent(4)
	assert.Equal(t, []providers.ToolResponse{{ToolCallID: "c4", Result: weatherResult}}, sent[3])
	assert.Equal(t, []string{"weather", "weather", "weather"}, exec.calls())

	elems := h.finish()
	errs := streamErrors(elems)
	require.Len(t, errs, 1, "the limit is reported once")
	var maxRounds *MaxRoundsError
	require.ErrorAs(t, errs[0], &maxRounds)
	assert.Equal(t, 2, maxRounds.Rounds)
	assert.Equal(t, "provider stage: max rounds (2) exceeded", errs[0].Error())
}

func TestDuplexTools_CostBudgetRefusesTheRound(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	h := startRealtime(t, realtimeOpts{
		registry: newWeatherRegistry(t, exec),
		policy:   &pipeline.ToolPolicy{MaxCostUSD: 0.01},
	})

	turn := toolCallTurn(weatherCall("c1"))
	turn[0].CostInfo = &types.CostInfo{InputTokens: 10, OutputTokens: 5, TotalCost: 0.02}
	h.emit(turn)

	budget := "provider stage: cost budget exceeded (0.0200 > 0.0100 USD)"
	// The budget is terminal: the stage records a refusal for the call,
	// reports the budget once and closes the session itself. The refusal is
	// not sent to the session: a tool response starts another paid reply
	// (OpenAI follows it with response.create).
	elems := drainUntilClosed(t, h.output)
	require.NoError(t, <-h.done)
	assert.True(t, h.sess().isClosed(), "the session is closed")
	assert.Empty(t, h.sess().sentResponses(), "no tool response starts another reply")
	assert.Equal(t, 0, h.sess().sendsAfterClose())
	assert.Empty(t, exec.calls())
	results := toolResultMessages(elems)
	require.Len(t, results, 1, "the refusal is recorded downstream")
	assert.Equal(t, "c1", results[0].ID)
	assert.Equal(t, "Tool weather not executed: the turn's tool calls were stopped: "+budget, results[0].Error)
	errs := streamErrors(elems)
	require.Len(t, errs, 1)
	assert.Equal(t, budget, errs[0].Error())
}

// A model that answers every refusal of a stopped tool turn with another tool
// call gets two refusals; the next call closes the session.
func TestDuplexTools_RefusalLoopClosesTheSession(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	h := startRealtime(t, realtimeOpts{
		registry: newWeatherRegistry(t, exec),
		policy:   &pipeline.ToolPolicy{MaxRounds: 1},
		script: [][]providers.StreamChunk{
			toolCallTurn(weatherCall("c2")),
			toolCallTurn(weatherCall("c3")),
			toolCallTurn(weatherCall("c4")),
			toolCallTurn(weatherCall("c5")),
		},
	})

	h.emit(toolCallTurn(weatherCall("c1")))
	elems := drainUntilClosed(t, h.output)
	require.NoError(t, <-h.done)

	refusal := func(id string) []providers.ToolResponse {
		return []providers.ToolResponse{{
			ToolCallID: id,
			Result:     "Tool weather not executed: the turn's tool calls were stopped: provider stage: max rounds (1) exceeded",
			IsError:    true,
		}}
	}
	assert.Equal(t, [][]providers.ToolResponse{
		{{ToolCallID: "c1", Result: weatherResult}},
		refusal("c2"),
		refusal("c3"),
	}, h.sess().sentResponses(), "two refusal batches, then nothing")
	assert.True(t, h.sess().isClosed())
	assert.Equal(t, []string{"weather"}, exec.calls())
	errs := streamErrors(elems)
	require.Len(t, errs, 2)
	assert.Equal(t, "provider stage: max rounds (1) exceeded", errs[0].Error())
	assert.Equal(t, "duplex provider stage: closing the session: the model kept calling tools "+
		"after its tool turn was stopped: provider stage: max rounds (1) exceeded", errs[1].Error())
	var maxRounds *MaxRoundsError
	assert.ErrorAs(t, errs[1], &maxRounds)
}

// A round queued while an earlier round of the same tool turn runs is
// decided when it runs: once the earlier round stops the turn, it is refused.
func TestDuplexTools_RoundQueuedBehindAStoppingRoundIsRefused(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`), gate: make(chan struct{})}
	h := startRealtime(t, realtimeOpts{
		registry: newWeatherRegistry(t, exec),
		policy:   &pipeline.ToolPolicy{MaxRounds: 1},
	})

	h.emit(toolCallTurn(weatherCall("c1")))
	require.Eventually(t, func() bool { return len(exec.calls()) == 1 }, 2*time.Second, time.Millisecond)
	h.emit(toolCallTurn(weatherCall("c2")))
	require.Eventually(t, func() bool { return len(h.stage.tools.jobs) == 1 }, 2*time.Second, time.Millisecond,
		"the second round is queued behind the first")
	close(exec.gate)

	sent := h.awaitSent(2)
	assert.Equal(t, [][]providers.ToolResponse{
		{{ToolCallID: "c1", Result: weatherResult}},
		{{
			ToolCallID: "c2",
			Result:     "Tool weather not executed: the turn's tool calls were stopped: provider stage: max rounds (1) exceeded",
			IsError:    true,
		}},
	}, sent)
	assert.Equal(t, []string{"weather"}, exec.calls(), "the queued round never ran")
	h.finish()
}

// A repeated-call nudge on the limit round does not stop the turn; the round
// after it is past MaxRounds and is refused rather than run.
func TestDuplexTools_RoundPastTheLimitIsRefused(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	rome := types.MessageToolCall{ID: "c3", Name: "weather", Args: json.RawMessage(`{"city":"Rome"}`)}
	h := startRealtime(t, realtimeOpts{
		registry: newWeatherRegistry(t, exec),
		policy:   &pipeline.ToolPolicy{MaxRounds: 1, MaxIdenticalToolCalls: 2},
		script:   [][]providers.StreamChunk{toolCallTurn(rome)},
	})

	// Round 1 repeats a call: the repeat is fed back, not run, and the round
	// is a nudge, so nothing stops.
	h.emit(toolCallTurn(weatherCall("c1"), weatherCall("c2")))
	sent := h.awaitSent(2)
	assert.Equal(t, []providers.ToolResponse{{
		ToolCallID: "c3",
		Result:     "Tool weather not executed: the turn's tool calls were stopped: provider stage: max rounds (1) exceeded",
		IsError:    true,
	}}, sent[1])
	elems := h.finish()
	assert.Equal(t, []string{"weather"}, exec.calls(),
		"round 1 ran only its first call (the repeat was fed back); round 2, past the limit, ran nothing")
	errs := streamErrors(elems)
	require.Len(t, errs, 1)
	var maxRounds *MaxRoundsError
	require.ErrorAs(t, errs[0], &maxRounds)
	assert.Equal(t, 1, maxRounds.Rounds)
}

// Each realtime round is a provider call: provider.call.started when it
// begins (the user's turn, or the tool responses that continue it) and
// provider.call.completed when its reply completes, timed between the two,
// with the configured source and labels.
func TestDuplexTools_ProviderCallTiming(t *testing.T) {
	bus := events.NewEventBus()
	var evMu sync.Mutex
	var started []*events.ProviderCallStartedData
	var completed []*events.ProviderCallCompletedData
	bus.SubscribeAll(func(e *events.Event) {
		evMu.Lock()
		defer evMu.Unlock()
		switch d := e.Data.(type) {
		case *events.ProviderCallStartedData:
			started = append(started, d)
		case *events.ProviderCallCompletedData:
			completed = append(completed, d)
		}
	})
	const toolTime = 150 * time.Millisecond
	const thinkTime = 40 * time.Millisecond
	const replyTime = 30 * time.Millisecond
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`), delay: toolTime}
	labels := map[string]string{"team": "voice"}
	h := startRealtime(t, realtimeOpts{
		registry:   newWeatherRegistry(t, exec),
		emitter:    events.NewEmitter(bus, "run", "sess", "conv"),
		cfg:        DuplexToolConfig{Source: "selfplay", Labels: labels},
		script:     [][]providers.StreamChunk{textTurn("It is 21 degrees", nil)},
		replyDelay: replyTime,
	})

	user := types.Message{Role: roleUser, Content: "weather?"}
	h.input <- StreamElement{Message: &user}
	require.Eventually(t, func() bool {
		evMu.Lock()
		defer evMu.Unlock()
		return len(started) == 1
	}, 2*time.Second, time.Millisecond, "the user's turn starts round 1")
	time.Sleep(thinkTime)
	h.emit(toolCallTurn(weatherCall("c1")))
	h.awaitSent(1)
	require.Eventually(t, func() bool {
		evMu.Lock()
		defer evMu.Unlock()
		return len(completed) == 2
	}, 2*time.Second, time.Millisecond)
	h.finish()

	evMu.Lock()
	defer evMu.Unlock()
	require.Len(t, started, 2)
	for i, d := range started {
		assert.Equal(t, i+1, d.Round)
		assert.Equal(t, labels, d.Labels)
		assert.Equal(t, "rt", d.Provider)
		assert.Equal(t, "rt-model", d.Model)
		assert.Equal(t, d.CallID, completed[i].CallID)
	}
	for i, d := range completed {
		assert.Equal(t, i+1, d.Round)
		assert.Equal(t, "selfplay", d.Source)
		assert.Equal(t, labels, d.Labels)
	}
	assert.GreaterOrEqual(t, completed[0].Duration, thinkTime, "round 1 runs from the user's turn")
	assert.Less(t, completed[0].Duration, time.Second)
	assert.GreaterOrEqual(t, completed[1].Duration, replyTime, "round 2 starts when the tool responses are sent")
	assert.Less(t, completed[1].Duration, toolTime, "round 2 runs from the tool responses, not the tool run")
}

// A reply to input the stage cannot see the end of (audio the provider does
// not transcribe) is a provider call from its first output.
func TestDuplexTools_ProviderCallTimingWithoutUserTurn(t *testing.T) {
	bus := events.NewEventBus()
	var evMu sync.Mutex
	var completed []*events.ProviderCallCompletedData
	bus.SubscribeAll(func(e *events.Event) {
		if d, ok := e.Data.(*events.ProviderCallCompletedData); ok {
			evMu.Lock()
			completed = append(completed, d)
			evMu.Unlock()
		}
	})
	const streamTime = 40 * time.Millisecond
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	h := startRealtime(t, realtimeOpts{
		registry: newWeatherRegistry(t, exec),
		emitter:  events.NewEmitter(bus, "run", "sess", "conv"),
	})

	h.emit([]providers.StreamChunk{{Content: "Let me check", Delta: "Let me check"}})
	time.Sleep(streamTime)
	h.emit(toolCallTurn(weatherCall("c1")))
	h.awaitSent(1)
	require.Eventually(t, func() bool {
		evMu.Lock()
		defer evMu.Unlock()
		return len(completed) == 1
	}, 2*time.Second, time.Millisecond)
	h.finish()

	evMu.Lock()
	defer evMu.Unlock()
	assert.Equal(t, 1, completed[0].Round)
	assert.GreaterOrEqual(t, completed[0].Duration, streamTime, "the call runs from the reply's first output")
	assert.Less(t, completed[0].Duration, time.Second)
}

// A barge-in cuts the turn short; what it had cost still counts toward the
// budget, and it is reported as a provider call like any other turn.
func TestDuplexTools_InterruptedTurnIsChargedAndReported(t *testing.T) {
	bus := events.NewEventBus()
	var evMu sync.Mutex
	var completed []*events.ProviderCallCompletedData
	bus.SubscribeAll(func(e *events.Event) {
		if d, ok := e.Data.(*events.ProviderCallCompletedData); ok {
			evMu.Lock()
			completed = append(completed, d)
			evMu.Unlock()
		}
	})
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	h := startRealtime(t, realtimeOpts{
		registry: newWeatherRegistry(t, exec),
		policy:   &pipeline.ToolPolicy{MaxCostUSD: 0.01},
		emitter:  events.NewEmitter(bus, "run", "sess", "conv"),
	})

	h.emit([]providers.StreamChunk{
		{Content: "Let me ch", Delta: "Let me ch"},
		{Interrupted: true, CostInfo: &types.CostInfo{InputTokens: 7, OutputTokens: 3, TotalCost: 0.02}},
	})
	elems := drainUntilClosed(t, h.output)
	require.NoError(t, <-h.done)

	assert.True(t, h.sess().isClosed(), "the budget is spent: the session is closed")
	errs := streamErrors(elems)
	require.Len(t, errs, 1)
	assert.Equal(t, "provider stage: cost budget exceeded (0.0200 > 0.0100 USD)", errs[0].Error())
	require.Eventually(t, func() bool {
		evMu.Lock()
		defer evMu.Unlock()
		return len(completed) == 1
	}, 2*time.Second, time.Millisecond)
	evMu.Lock()
	defer evMu.Unlock()
	assert.Equal(t, 1, completed[0].Round)
	assert.Equal(t, types.FinishReasonInterrupted, completed[0].FinishReason)
	assert.InDelta(t, 0.02, completed[0].Cost, 1e-9)
	assert.Equal(t, 7, completed[0].InputTokens)
}

// MaxCostUSD bounds the whole session, the resumed conversation's spend
// included, not each tool turn.
func TestDuplexTools_CostBudgetSpansTheSession(t *testing.T) {
	budget := "provider stage: cost budget exceeded (0.0600 > 0.0500 USD)"
	// The budget closes the session; the refusal is recorded, not sent.
	assertRefused := func(t *testing.T, h *realtimeHarness) {
		t.Helper()
		elems := drainUntilClosed(t, h.output)
		require.NoError(t, <-h.done)
		assert.True(t, h.sess().isClosed())
		assert.Empty(t, h.sess().sentResponses())
		results := toolResultMessages(elems)
		require.Len(t, results, 1)
		assert.Equal(t, "Tool weather not executed: the turn's tool calls were stopped: "+budget, results[0].Error)
	}
	toolTurn := func() []providers.StreamChunk {
		turn := toolCallTurn(weatherCall("c1"))
		turn[0].CostInfo = &types.CostInfo{TotalCost: 0.02}
		return turn
	}

	t.Run("earlier turns", func(t *testing.T) {
		exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
		h := startRealtime(t, realtimeOpts{
			registry: newWeatherRegistry(t, exec),
			policy:   &pipeline.ToolPolicy{MaxCostUSD: 0.05},
		})
		h.emit(textTurn("hello", &types.CostInfo{TotalCost: 0.02}))
		h.emit(textTurn("hello again", &types.CostInfo{TotalCost: 0.02}))
		h.emit(toolTurn())
		assertRefused(t, h)
		assert.Empty(t, exec.calls())
	})

	t.Run("resumed history", func(t *testing.T) {
		exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
		h := startRealtime(t, realtimeOpts{
			registry: newWeatherRegistry(t, exec),
			policy:   &pipeline.ToolPolicy{MaxCostUSD: 0.05},
			history: []types.Message{
				{Role: roleUser, Content: "hi"},
				{Role: roleAssistant, Content: "hello", CostInfo: &types.CostInfo{TotalCost: 0.04}},
			},
		})
		h.emit(toolTurn())
		assertRefused(t, h)
		assert.Empty(t, exec.calls())
	})
}

func TestDuplexTools_PendingCallIsSurfaced(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	gate := func(_ context.Context, _, name string, _ map[string]any) (*tools.PendingToolInfo, error) {
		return &tools.PendingToolInfo{Reason: "approval", Message: "approve " + name}, nil
	}
	h := startRealtime(t, realtimeOpts{
		registry: newWeatherRegistry(t, exec),
		cfg:      DuplexToolConfig{ApprovalGate: gate},
	})

	h.emit(toolCallTurn(weatherCall("c1")))
	var pending []tools.PendingToolExecution
	deadline := time.After(2 * time.Second)
	var elems []StreamElement
	for pending == nil {
		select {
		case e := <-h.output:
			elems = append(elems, e)
			pending = e.Meta.PendingTools
		case <-deadline:
			t.Fatal("no pending element")
		}
	}
	require.Len(t, pending, 1)
	assert.Equal(t, "c1", pending[0].CallID)
	assert.Equal(t, "weather", pending[0].ToolName)
	assert.Equal(t, map[string]any{"city": "Paris"}, pending[0].Args)
	assert.Equal(t, "approve weather", pending[0].PendingInfo.Message)
	elems = append(elems, h.finish()...)
	assert.Empty(t, h.sess().sentResponses(), "a held call is not answered")
	assert.Empty(t, exec.calls())
	assert.Empty(t, toolResultMessages(elems))
}

// A user turn that arrives while a round runs starts a new tool turn: the
// round's results still answer its calls, and are counted against the turn
// that made them, not the new one.
func TestDuplexTools_NewUserTurnDuringRound(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`), gate: make(chan struct{})}
	h := startRealtime(t, realtimeOpts{
		registry: newWeatherRegistry(t, exec),
		policy:   &pipeline.ToolPolicy{MaxRounds: 1},
	})

	h.emit(toolCallTurn(weatherCall("c1")))
	require.Eventually(t, func() bool { return len(exec.calls()) == 1 }, 2*time.Second, time.Millisecond)
	user := types.Message{Role: roleUser, Content: "actually, Rome"}
	h.input <- StreamElement{Message: &user}
	require.Eventually(t, func() bool { return len(h.output) >= 2 }, 2*time.Second, time.Millisecond,
		"the user message is forwarded")
	close(exec.gate)

	sent := h.awaitSent(1)
	assert.Equal(t, [][]providers.ToolResponse{{{ToolCallID: "c1", Result: weatherResult}}}, sent)

	// Round 1 of the first turn hit MaxRounds; the new turn's first round runs.
	h.emit(toolCallTurn(weatherCall("c2")))
	sent = h.awaitSent(2)
	assert.Equal(t, []providers.ToolResponse{{ToolCallID: "c2", Result: weatherResult}}, sent[1])
	assert.Equal(t, []string{"weather", "weather"}, exec.calls())
	h.finish()
}

// Closing the session while a round runs: the round's results are never sent
// to the closed session, but are still recorded downstream.
func TestDuplexTools_CloseDuringRoundSendsNothingAfterClose(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`), gate: make(chan struct{})}
	var h *realtimeHarness
	onClose := func() {
		// Let the round finish while the session is closing, and give its
		// results the chance to be sent: wait until they are, or until its
		// result message follows the assistant message downstream.
		close(exec.gate)
		require.Eventually(t, func() bool { return exec.done() == 1 }, 2*time.Second, time.Millisecond)
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) && h.sess().sendsAfterClose() == 0 && len(h.output) < 2 {
			time.Sleep(time.Millisecond)
		}
	}
	h = startRealtime(t, realtimeOpts{registry: newWeatherRegistry(t, exec), onClose: onClose})

	h.emit(toolCallTurn(weatherCall("c1")))
	require.Eventually(t, func() bool { return len(exec.calls()) == 1 }, 2*time.Second, time.Millisecond)

	// The host is done: all responses are in, and input ends. Output is read
	// only once the stage returns, so onClose can watch it fill.
	h.input <- StreamElement{Meta: ElementMetadata{AllResponsesReceived: true}}
	close(h.input)
	require.NoError(t, <-h.done)
	elems := drainUntilClosed(t, h.output)

	assert.Equal(t, 0, h.sess().sendsAfterClose(), "no tool response is sent after Close")
	assert.Empty(t, h.sess().sentResponses())
	results := toolResultMessages(elems)
	require.Len(t, results, 1, "the round's tool ran: its result is recorded with the call")
	assert.Equal(t, "c1", results[0].ID)
	assert.Equal(t, weatherResult, results[0].GetTextContent())
}

// An assistant turn without tool calls ends the tool turn, as a round without
// calls ends a text turn, even when no user turn is seen (audio the provider
// does not transcribe): the next call starts a fresh tool turn.
func TestDuplexTools_ReplyWithoutCallsEndsTheToolTurn(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	h := startRealtime(t, realtimeOpts{
		registry: newWeatherRegistry(t, exec),
		policy:   &pipeline.ToolPolicy{MaxRounds: 1},
		script:   [][]providers.StreamChunk{textTurn("It is 21 degrees", nil)},
	})

	h.emit(toolCallTurn(weatherCall("c1")))
	h.awaitSent(1)
	h.emit(toolCallTurn(weatherCall("c2")))
	sent := h.awaitSent(2)
	assert.Equal(t, [][]providers.ToolResponse{
		{{ToolCallID: "c1", Result: weatherResult}},
		{{ToolCallID: "c2", Result: weatherResult}},
	}, sent)
	assert.Equal(t, []string{"weather", "weather"}, exec.calls())
	h.finish()
}

// After a barge-in the provider closes the cut-short reply with an empty turn
// completion, which is skipped. A reply that then makes only tool calls (no
// text, no audio) is a real turn: its calls run and their results are sent,
// whether the calls arrive with the finish reason or ahead of it.
func TestDuplexTools_ToolCallOnlyReplyAfterBargeInIsAnswered(t *testing.T) {
	fr := "tool_calls"
	stop := "stop"
	for _, tc := range []struct {
		name  string
		reply []providers.StreamChunk
	}{
		{name: "calls on the finish chunk", reply: toolCallTurn(weatherCall("c1"))},
		{name: "calls ahead of the finish chunk", reply: []providers.StreamChunk{
			{ToolCalls: []types.MessageToolCall{weatherCall("c1")}},
			{FinishReason: &fr},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
			h := startRealtime(t, realtimeOpts{
				registry: newWeatherRegistry(t, exec),
				script:   [][]providers.StreamChunk{textTurn("It is 21 degrees", nil)},
			})

			h.emit([]providers.StreamChunk{
				{Content: "Let me ch", Delta: "Let me ch"},
				{Interrupted: true},
				{FinishReason: &stop},
			})
			h.emit(tc.reply)

			sent := h.awaitSent(1)
			assert.Equal(t, [][]providers.ToolResponse{{{ToolCallID: "c1", Result: weatherResult}}}, sent)
			assert.Equal(t, []string{"weather"}, exec.calls())
			elems := h.finish()

			var calls [][]types.MessageToolCall
			var replies []string
			for _, e := range elems {
				if e.Message == nil || e.Message.Role != roleAssistant {
					continue
				}
				if len(e.Message.ToolCalls) > 0 {
					calls = append(calls, e.Message.ToolCalls)
				} else if e.Message.FinishReason == stop {
					replies = append(replies, e.Message.Content)
				}
			}
			assert.Equal(t, [][]types.MessageToolCall{{weatherCall("c1")}}, calls)
			assert.Equal(t, []string{"It is 21 degrees"}, replies)
		})
	}
}

// recordingStateResolver reports a workflow state and records the executed
// tool-call counts it is given.
type recordingStateResolver struct {
	fakeResolver
	mu       sync.Mutex
	recorded []int
}

func (r *recordingStateResolver) RecordToolCalls(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recorded = append(r.recorded, n)
}

func (r *recordingStateResolver) records() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int(nil), r.recorded...)
}

// The assistant message with the calls records what the session was offered
// and the workflow state that produced it; executed calls count toward the
// workflow's tool-call budget.
func TestDuplexTools_StampsTheTurnAndRecordsCalls(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	resolver := &recordingStateResolver{fakeResolver: fakeResolver{meta: map[string]any{"state": "triage"}}}
	h := startRealtime(t, realtimeOpts{
		registry:     newWeatherRegistry(t, exec),
		resolver:     resolver,
		sessionTools: []providers.StreamingToolDefinition{{Name: "weather"}, {Name: "forecast"}},
	})

	h.emit(toolCallTurn(weatherCall("c1"), weatherCall("c2")))
	h.awaitSent(1)
	elems := h.finish()

	require.NotEmpty(t, elems)
	require.NotNil(t, elems[0].Message)
	assert.Equal(t, map[string]interface{}{
		finishReasonMetaKey:    "tool_calls",
		types.MetaToolsOffered: []string{"forecast", "weather"},
		workflowStateMetaKey:   map[string]any{"state": "triage"},
	}, elems[0].Message.Meta)
	assert.Equal(t, []int{2}, resolver.records())
}

// Input ending does not end the session: a round still running when it does
// is answered.
func TestDuplexTools_RoundAfterInputEndsIsStillAnswered(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`), gate: make(chan struct{})}
	h := startRealtime(t, realtimeOpts{registry: newWeatherRegistry(t, exec)})

	h.emit(toolCallTurn(weatherCall("c1")))
	require.Eventually(t, func() bool { return len(exec.calls()) == 1 }, 2*time.Second, time.Millisecond)
	close(h.input)
	close(exec.gate)

	sent := h.awaitSent(1)
	assert.Equal(t, [][]providers.ToolResponse{{{ToolCallID: "c1", Result: weatherResult}}}, sent)
	elems := h.finish()
	results := toolResultMessages(elems)
	require.Len(t, results, 1)
	assert.Equal(t, weatherResult, results[0].GetTextContent())
}

// Without a registry the stage forwards tool calls downstream untouched, as
// it always has: the host executes them.
func TestDuplexTools_NilRegistryForwardsToolCalls(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts realtimeOpts
	}{
		{name: "never set", opts: realtimeOpts{noToolSet: true}},
		{name: "nil registry", opts: realtimeOpts{policy: &pipeline.ToolPolicy{MaxRounds: 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bus := events.NewEventBus()
			var evMu sync.Mutex
			var rounds []int
			bus.SubscribeAll(func(e *events.Event) {
				evMu.Lock()
				defer evMu.Unlock()
				switch d := e.Data.(type) {
				case *events.ProviderCallStartedData:
					rounds = append(rounds, d.Round)
				case *events.ProviderCallCompletedData:
					rounds = append(rounds, d.Round)
				}
			})
			emitter := events.NewEmitter(bus, "run", "sess", "conv")
			tc.opts.emitter = emitter
			h := startRealtime(t, tc.opts)
			h.emit(toolCallTurn(weatherCall("c1")))
			h.emit(toolCallTurn(weatherCall("c2")))
			require.Eventually(t, func() bool { return len(h.output) == 2 }, 2*time.Second, time.Millisecond)
			elems := h.finish()

			require.Len(t, elems, 2)
			for i, id := range []string{"c1", "c2"} {
				msg := elems[i].Message
				require.NotNil(t, msg)
				assert.Equal(t, []types.MessageToolCall{weatherCall(id)}, msg.ToolCalls)
				assert.Equal(t, map[string]interface{}{finishReasonMetaKey: "tool_calls"}, msg.Meta)
				assert.True(t, elems[i].EndOfStream)
			}
			assert.Empty(t, h.sess().sentResponses())
			assert.Nil(t, h.stage.tools)

			// The bus delivers in publish order: once a sentinel published now
			// arrives, every provider event the stage published has too.
			emitter.ProviderCallStartedCtx(context.Background(), &events.ProviderCallStartedData{Round: 99})
			require.Eventually(t, func() bool {
				evMu.Lock()
				defer evMu.Unlock()
				return len(rounds) > 0 && rounds[len(rounds)-1] == 99
			}, 2*time.Second, time.Millisecond)
			evMu.Lock()
			defer evMu.Unlock()
			assert.Equal(t, []int{99}, rounds, "no provider.call.* events without a registry")
		})
	}
}

const partsDroppedLog = "realtime session cannot send non-text tool result parts"

// chartParts is a multimodal tool result: text around two images.
func chartParts() []types.ContentPart {
	return []types.ContentPart{
		types.NewTextPart("Chart: "),
		{Type: types.ContentTypeImage, Media: &types.MediaContent{MIMEType: "image/png"}},
		types.NewTextPart("rainfall by month"),
		{Type: types.ContentTypeImage, Media: &types.MediaContent{MIMEType: "image/jpeg"}},
	}
}

func chartRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	reg := registryWithTools(t, "weather")
	reg.RegisterExecutor(&mockAsyncExecutor{name: "local", status: tools.ToolStatusComplete, parts: chartParts()})
	return reg
}

// A multimodal tool result reaches the session as its text in Result and its
// parts in Parts. A session that cannot send parts gets the same responses,
// and the stage warns once per session, naming the provider and the dropped
// part types, however many rounds drop parts.
func TestDuplexTools_MultimodalResultWithoutPartsSupport(t *testing.T) {
	logs := captureLogSync(t)
	h := startRealtime(t, realtimeOpts{
		registry: chartRegistry(t),
		script:   [][]providers.StreamChunk{toolCallTurn(weatherCall("c2")), textTurn("Done", nil)},
	})

	h.emit(toolCallTurn(weatherCall("c1")))

	sent := h.awaitSent(2)
	want := func(id string) []providers.ToolResponse {
		return []providers.ToolResponse{{ToolCallID: id, Result: "Chart: rainfall by month", Parts: chartParts()}}
	}
	assert.Equal(t, [][]providers.ToolResponse{want("c1"), want("c2")}, sent)
	h.finish()

	out := logs.String()
	assert.Equal(t, 1, strings.Count(out, partsDroppedLog), out)
	assert.Contains(t, out, `provider=rt`)
	assert.Contains(t, out, `part_types=[image]`)
}

// A media-only tool result has no text. A session that cannot send its parts
// sends a placeholder naming the part types in Result, so the model is not
// answered with an empty string; a session that can send parts gets them with
// an empty Result.
func TestDuplexTools_MediaOnlyResult(t *testing.T) {
	parts := []types.ContentPart{
		{Type: types.ContentTypeImage, Media: &types.MediaContent{MIMEType: "image/png"}},
		{Type: types.ContentTypeAudio, Media: &types.MediaContent{MIMEType: "audio/wav"}},
		{Type: types.ContentTypeImage, Media: &types.MediaContent{MIMEType: "image/jpeg"}},
	}
	for _, tc := range []struct {
		name         string
		partsSupport bool
		result       string
	}{
		{name: "without parts support", result: "[tool returned image, audio content this session cannot receive]"},
		{name: "with parts support", partsSupport: true, result: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := registryWithTools(t, "weather")
			reg.RegisterExecutor(&mockAsyncExecutor{name: "local", status: tools.ToolStatusComplete, parts: parts})
			h := startRealtime(t, realtimeOpts{registry: reg, partsSupport: tc.partsSupport})

			h.emit(toolCallTurn(weatherCall("c1")))

			sent := h.awaitSent(1)
			assert.Equal(t, [][]providers.ToolResponse{{{
				ToolCallID: "c1", Result: tc.result, Parts: parts,
			}}}, sent)
			elems := h.finish()
			results := toolResultMessages(elems)
			require.Len(t, results, 1)
			assert.Equal(t, parts, results[0].Parts, "the recorded result keeps its parts")
		})
	}
}

// An empty text result has nothing dropped: it is sent empty, with no
// placeholder, to a session that cannot send parts.
func TestDuplexTools_EmptyTextResultGetsNoPlaceholder(t *testing.T) {
	reg := registryWithTools(t, "weather")
	reg.RegisterExecutor(&mockAsyncExecutor{name: "local", status: tools.ToolStatusComplete,
		parts: []types.ContentPart{types.NewTextPart("")}})
	h := startRealtime(t, realtimeOpts{registry: reg})

	h.emit(toolCallTurn(weatherCall("c1")))

	sent := h.awaitSent(1)
	require.Len(t, sent, 1)
	require.Len(t, sent[0], 1)
	assert.Equal(t, "c1", sent[0][0].ToolCallID)
	assert.Equal(t, "", sent[0][0].Result)
	h.finish()
}

// A session that can send parts gets the same responses and no warning.
func TestDuplexTools_MultimodalResultWithPartsSupport(t *testing.T) {
	logs := captureLogSync(t)
	h := startRealtime(t, realtimeOpts{registry: chartRegistry(t), partsSupport: true})

	h.emit(toolCallTurn(weatherCall("c1")))

	sent := h.awaitSent(1)
	assert.Equal(t, [][]providers.ToolResponse{{{
		ToolCallID: "c1", Result: "Chart: rainfall by month", Parts: chartParts(),
	}}}, sent)
	h.finish()
	assert.NotContains(t, logs.String(), partsDroppedLog)
}

// A text-only result is sent as text with no parts, and nothing is dropped
// to warn about.
func TestDuplexTools_TextResultSendsNoPartsAndNoWarning(t *testing.T) {
	logs := captureLogSync(t)
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	h := startRealtime(t, realtimeOpts{registry: newWeatherRegistry(t, exec)})

	h.emit(toolCallTurn(weatherCall("c1")))

	sent := h.awaitSent(1)
	assert.Equal(t, [][]providers.ToolResponse{{{ToolCallID: "c1", Result: weatherResult}}}, sent)
	assert.Nil(t, sent[0][0].Parts)
	h.finish()
	assert.NotContains(t, logs.String(), partsDroppedLog)
}

// A typed turn an input guardrail blocks never reaches the provider, so it
// opens no provider call: no provider.call.started is emitted for it. That
// holds for bare text and for text carried with its user message.
func TestDuplexTools_BlockedTypedTurnOpensNoProviderCall(t *testing.T) {
	for name, withMessage := range map[string]bool{"bare text": false, "user message": true} {
		t.Run(name, func(t *testing.T) {
			bus := events.NewEventBus()
			var evMu sync.Mutex
			started := 0
			bus.SubscribeAll(func(e *events.Event) {
				if e.Type == events.EventProviderCallStarted {
					evMu.Lock()
					started++
					evMu.Unlock()
				}
			})
			h := startRealtime(t, realtimeOpts{
				registry:  newWeatherRegistry(t, &realtimeToolExec{result: json.RawMessage(`"ok"`)}),
				turnState: &TurnState{AllowedTools: []string{"weather"}},
				emitter:   events.NewEmitter(bus, "run", "sess", "conv"),
				updatable: true,
				hookReg: hooks.NewRegistry(hooks.WithProviderHook(&enforceBeforeCallHook{
					reason: "off topic", replacement: "Not here.",
				})),
			})

			text := "something off topic"
			elem := StreamElement{Text: &text}
			if withMessage {
				elem.Message = &types.Message{Role: roleUser, Content: text}
			}
			h.input <- elem
			require.Eventually(t, func() bool {
				for _, e := range h.stage.history.snapshot() {
					if e.Role == roleAssistant && e.Content == "Not here." {
						return true
					}
				}
				return false
			}, 2*time.Second, time.Millisecond, "the blocked turn was never emitted")
			bus.Close()
			h.finish()

			evMu.Lock()
			defer evMu.Unlock()
			assert.Zero(t, started, "a blocked turn starts no provider call")
			updates, _ := h.sess().toolUpdates()
			assert.Empty(t, updates)
		})
	}
}

// A round that finishes once the session is closing still has its results
// recorded downstream: its tool ran, and its assistant tool-call message was
// already sent. Nothing is sent to the closing session.
func TestDuplexTools_RoundFinishingAfterCloseIsStillRecorded(t *testing.T) {
	st := NewDuplexProviderStageWithTurnState(
		providersmock.NewStreamingProvider("rt", "rt-model", false), baseConfig(), nil, nil)
	st.SetToolExecution(registryWithTools(t, "weather"), nil, DuplexToolConfig{}, nil)
	sess := newRecordingSession()
	st.session = sess
	st.tools.markClosed()

	result := types.NewToolResultMessage(types.NewTextToolResult("c1", "weather", weatherResult))
	out := make(chan StreamElement, 4)
	require.NoError(t, st.deliverToolRound(context.Background(),
		toolRoundResult([]types.Message{result}, nil, nil), out))
	close(out)

	var elems []StreamElement
	for e := range out {
		elems = append(elems, e)
	}
	results := toolResultMessages(elems)
	require.Len(t, results, 1, "the finished round's result is recorded")
	assert.Equal(t, "c1", results[0].ID)
	assert.Equal(t, weatherResult, results[0].GetTextContent())
	sess.mu.Lock()
	defer sess.mu.Unlock()
	assert.Empty(t, sess.toolResponses, "nothing is sent to a closing session")
}

// A round with a call held for approval still applies the turn's other
// checks to the calls that ran: a realtime turn goes on past a held call, so
// a termination tool that ran beside one still stops the turn, and the
// model's next calls are refused.
func TestDuplexTools_HeldCallDoesNotSkipTermination(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`"ok"`)}
	reg := registryWithTools(t, "finish", "risky", "weather")
	reg.RegisterExecutor(exec)
	gate := func(_ context.Context, _, name string, _ map[string]any) (*tools.PendingToolInfo, error) {
		if name != "risky" {
			return nil, nil
		}
		return &tools.PendingToolInfo{Reason: "approval", Message: "approve " + name}, nil
	}
	h := startRealtime(t, realtimeOpts{
		registry: reg,
		policy:   &pipeline.ToolPolicy{StopOnTool: "finish"},
		cfg:      DuplexToolConfig{ApprovalGate: gate},
		script:   [][]providers.StreamChunk{toolCallTurn(weatherCall("c3"))},
	})

	h.emit(toolCallTurn(namedCall("c1", "finish"), namedCall("c2", "risky")))
	sent := h.awaitSent(2)
	h.finish()

	assert.Equal(t, [][]providers.ToolResponse{
		{{ToolCallID: "c1", Result: "ok"}},
		{{ToolCallID: "c3", IsError: true, Result: "Tool weather not executed: the turn's tool calls were stopped: " +
			"the turn's termination tool was called"}},
	}, sent)
	assert.Equal(t, []string{"finish"}, exec.calls(), "the held call waits; the call after the stop is refused")
}

// A termination tool held for approval has not run, so it does not stop the
// turn: the model's next calls run.
func TestDuplexTools_HeldTerminationToolDoesNotStopTheTurn(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`"ok"`)}
	reg := registryWithTools(t, "finish", "weather")
	reg.RegisterExecutor(exec)
	gate := func(_ context.Context, _, name string, _ map[string]any) (*tools.PendingToolInfo, error) {
		if name != "finish" {
			return nil, nil
		}
		return &tools.PendingToolInfo{Reason: "approval", Message: "approve " + name}, nil
	}
	h := startRealtime(t, realtimeOpts{
		registry: reg,
		policy:   &pipeline.ToolPolicy{StopOnTool: "finish"},
		cfg:      DuplexToolConfig{ApprovalGate: gate},
		script:   [][]providers.StreamChunk{toolCallTurn(weatherCall("c3"))},
	})

	h.emit(toolCallTurn(namedCall("c1", "finish"), weatherCall("c2")))
	sent := h.awaitSent(2)
	h.finish()

	assert.Equal(t, [][]providers.ToolResponse{
		{{ToolCallID: "c2", Result: "ok"}},
		{{ToolCallID: "c3", Result: "ok"}},
	}, sent)
	assert.Equal(t, []string{"weather", "weather"}, exec.calls())
}

// Once input closes, the final-response wait is extended while a tool round
// runs and while the reply its results start streams: the round's results
// reach the session and its reply reaches the caller, and the session then
// closes.
func TestDuplexTools_FinalWaitCoversARunningRoundAndItsReply(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`), delay: 300 * time.Millisecond}
	h := startRealtime(t, realtimeOpts{
		registry:  newWeatherRegistry(t, exec),
		finalWait: 50 * time.Millisecond,
	})

	h.emit(toolCallTurn(weatherCall("c1")))
	require.Eventually(t, func() bool { return len(exec.calls()) == 1 }, 2*time.Second, time.Millisecond)
	close(h.input)
	h.awaitSent(1)
	// The reply starts well after the round was delivered: several waits pass
	// while it is awaited.
	time.Sleep(200 * time.Millisecond)
	h.emit(textTurn("It is 21 degrees", nil))
	elems := drainUntilClosed(t, h.output)
	require.NoError(t, <-h.done)

	assert.Equal(t, [][]providers.ToolResponse{{{ToolCallID: "c1", Result: weatherResult}}},
		h.sess().sentResponses())
	msgs := realtimeAssistantMessages(elems)
	require.Len(t, msgs, 2)
	assert.Equal(t, "It is 21 degrees", msgs[1].Content)
	assert.True(t, h.sess().isClosed())
}

// A tool that never finishes cannot hold a session whose input closed open
// past the wait limit.
func TestDuplexTools_FinalWaitIsBoundedForAHungTool(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`"ok"`), gate: make(chan struct{})}
	h := startRealtime(t, realtimeOpts{
		registry:      newWeatherRegistry(t, exec),
		finalWait:     20 * time.Millisecond,
		toolWaitLimit: 200 * time.Millisecond,
	})

	h.emit(toolCallTurn(weatherCall("c1")))
	require.Eventually(t, func() bool { return len(exec.calls()) == 1 }, 2*time.Second, time.Millisecond)
	start := time.Now()
	close(h.input)
	drainUntilClosed(t, h.output)
	require.NoError(t, <-h.done)

	assert.True(t, h.sess().isClosed())
	assert.GreaterOrEqual(t, time.Since(start), 200*time.Millisecond, "the wait was extended for the running round")
	assert.Less(t, time.Since(start), 2*time.Second, "but not past the limit")
	assert.Empty(t, h.sess().sentResponses())
}
