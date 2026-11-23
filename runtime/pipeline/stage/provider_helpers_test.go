package stage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/compaction"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// windowedProvider is a callRecordingProvider with a context window, that
// records whether each round's transcript carried compacted tool results.
type windowedProvider struct {
	*callRecordingProvider
	window    int
	compacted []bool
}

// toolHistory is three old, large tool round-trips and a recent user turn.
func toolHistory() []types.Message {
	big := strings.Repeat("result data ", 200) // ~600 tokens each
	var history []types.Message
	for _, id := range []string{"h1", "h2", "h3"} {
		history = append(history,
			types.Message{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: id, Name: "lookup", Args: []byte(`{}`)}}},
			types.Message{Role: "tool", ToolResult: &types.MessageToolResult{
				ID: id, Name: "lookup", Parts: []types.ContentPart{types.NewTextPart(big)},
			}},
		)
	}
	return append(history, types.Message{Role: "user", Content: "next"})
}

// enforcingChunkInterceptor fires once from OnChunk with a caller-owned
// metadata map, so a test can mutate that map after the run and observe
// whether the recorded validation aliased it.
type enforcingChunkInterceptor struct {
	meta  map[string]any
	fired bool
}

// thoughtful is an assistant reply that carries the model's reasoning.
func thoughtful() *types.Message {
	return &types.Message{
		Role: core.RoleAssistant, Content: "the banned answer",
		Reasoning: &types.ReasoningTrace{
			Text:   "reasoning toward the banned answer",
			Opaque: []types.OpaqueReasoning{{Provider: "claude", Kind: "thinking_signature", Data: "sig"}},
		},
	}
}

// maxCharsChunkInterceptor is a test ChunkInterceptor that aborts when
// accumulated content exceeds maxChars.
type maxCharsChunkInterceptor struct {
	maxChars    int
	accumulated string
}

// denyAllProviderHook is a test hook that denies all provider calls.
type denyAllProviderHook struct {
	reason   string
	metadata map[string]any
}

// allowBeforeDenyAfterHook allows BeforeCall but denies AfterCall.
type allowBeforeDenyAfterHook struct {
	reason   string
	metadata map[string]any
}

// denyToolHook blocks specific tools by name.
type denyToolHook struct {
	blockedTool string
	reason      string
}

func runProviderStage(
	t *testing.T,
	stage *ProviderStage,
	userContent string,
) ([]StreamElement, error) {
	t.Helper()
	input := make(chan StreamElement, 1)
	msg := types.Message{Role: "user", Content: userContent}
	elem := NewMessageElement(&msg)
	input <- elem
	close(input)

	output := make(chan StreamElement, 50)
	err := stage.Process(context.Background(), input, output)

	var elems []StreamElement
	for e := range output {
		elems = append(elems, e)
	}
	return elems, err
}

// redactionRecordingProvider captures the PredictionRequest the provider
// actually received, on whichever path the stage takes. Named distinctly from
// the package's other recordingProvider (composition_executor_test.go), which
// records only metadata and isn't a providers.Provider wrapper.
//
// streaming selects the path: false (the zero value) routes the stage through
// executeRound, true through executeStreamingRound. Both Predict and
// PredictStream record, so the same "provider never called" / "provider saw the
// mutated request" proofs hold on either path.
type redactionRecordingProvider struct {
	providers.Provider
	streaming bool
	mu        sync.Mutex
	requests  []providers.PredictionRequest
	calls     int
}

func (p *redactionRecordingProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// enforceBeforeCallHook enforces on BeforeCall, supplying canned text.
type enforceBeforeCallHook struct {
	reason      string
	replacement string
	metadata    map[string]any
}

// assistantMessages extracts assistant messages from emitted stream elements.
func assistantMessages(elems []StreamElement) []types.Message {
	var out []types.Message
	for _, e := range elems {
		if e.Message != nil && e.Message.Role == "assistant" {
			out = append(out, *e.Message)
		}
	}
	return out
}

// enforceAfterCallHook enforces on AfterCall, replacing the response content.
type enforceAfterCallHook struct {
	replacement string
}

// toolCallingProvider always returns one tool call plus content, so we can
// prove an enforced guardrail drops the call instead of executing it.
type toolCallingProvider struct {
	providers.Provider
	mu    sync.Mutex
	calls int
}

// collectValidationFailures subscribes to validation.failed and returns a
// getter that closes the bus (flushing async delivery) and returns the data.
func collectValidationFailures(
	t *testing.T, bus *events.EventBus,
) func() []*events.ValidationEventData {
	t.Helper()
	var mu sync.Mutex
	var received []*events.Event
	bus.Subscribe(events.EventValidationFailed, func(e *events.Event) {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
	})
	return func() []*events.ValidationEventData {
		bus.Close() // flush: the bus dispatches on a worker pool
		mu.Lock()
		defer mu.Unlock()
		out := make([]*events.ValidationEventData, 0, len(received))
		for _, e := range received {
			if d, ok := e.Data.(*events.ValidationEventData); ok {
				out = append(out, d)
			}
		}
		return out
	}
}

// scriptedRoundProvider answers with a tool call on every round until the
// scripted number of tool-calling rounds is exhausted, then answers with text.
// This is the shape that makes round attribution observable: the same tool,
// called with the same arguments, once per round.
type scriptedRoundProvider struct {
	providers.Provider
	toolRounds int

	mu    sync.Mutex
	calls int
}

// staticExecutor answers any tool call with a fixed result.
type staticExecutor struct{}

func registryWithTools(t *testing.T, names ...string) *tools.Registry {
	t.Helper()
	r := tools.NewRegistry()
	for _, n := range names {
		err := r.Register(&tools.ToolDescriptor{
			Name:        n,
			Description: "desc-" + n,
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Mode:        "local",
		})
		if err != nil {
			t.Fatalf("Register %q: %v", n, err)
		}
	}
	return r
}

// multiTurnRecordingProvider records the messages of every Predict call and
// returns a numbered reply, so tests can assert one provider call per turn and
// that later turns see earlier turns as history.
type multiTurnRecordingProvider struct {
	requests [][]types.Message
}

// blockingProvider blocks in Predict until its context is cancelled, signaling
// when generation is in-flight so a test can drive a barge-in mid-generation.
type blockingProvider struct {
	multiTurnRecordingProvider
	started chan struct{}
	once    sync.Once
}

// mockAsyncExecutor implements tools.AsyncToolExecutor for testing
type mockAsyncExecutor struct {
	name       string
	status     tools.ToolExecutionStatus
	content    []byte
	parts      []types.ContentPart
	errorMsg   string
	pendingMsg string
}

func toolResultsByID(msgs []types.Message) map[string]*types.MessageToolResult {
	out := map[string]*types.MessageToolResult{}
	for i := range msgs {
		if r := msgs[i].ToolResult; r != nil {
			out[r.ID] = r
		}
	}
	return out
}

// callRound is what one round looked like to the provider that served it.
type callRound struct {
	system      string
	maxTokens   int
	temperature float32
	tools       providers.ProviderTools
}

// callRecordingProvider records each round, builds tools tagged with its own
// id, and calls the transition tool on its first round when transitions is
// set.
type callRecordingProvider struct {
	*mock.ToolProvider
	transitions bool
	streaming   bool
	rounds      []callRound
}

func newCallRecordingProvider(id string, transitions bool) *callRecordingProvider {
	return &callRecordingProvider{
		ToolProvider: mock.NewToolProvider(id, id+"-model", false, nil),
		transitions:  transitions,
	}
}

// fakeResolver returns a scripted sequence of Handoffs, repeating the last.
// The resolver contract is "what should this turn be running", so a real
// implementation returns the ORIGIN state's prompt until the transition
// commits and the DESTINATION's afterwards — the sequence models that.
type fakeResolver struct {
	sequence []Handoff
	err      error
	meta     map[string]any
	calls    int
}

// scriptedChannelProvider streams whatever the test sends on its channel.
type scriptedChannelProvider struct {
	*mock.Provider
	stream chan providers.StreamChunk
}

const noPineappleMsg = "Let's talk about something else."

// sendScriptedDeltas sends each delta as a chunk carrying the reply so far.
func sendScriptedDeltas(stream chan<- providers.StreamChunk, deltas ...string) {
	sofar := ""
	for _, d := range deltas {
		sofar += d
		stream <- providers.StreamChunk{Delta: d, Content: sofar}
	}
}

// lastAssistantOf is the last assistant message among elems.
func lastAssistantOf(t *testing.T, elems []StreamElement) *types.Message {
	t.Helper()
	for i := len(elems) - 1; i >= 0; i-- {
		if m := elems[i].Message; m != nil && m.Role == core.RoleAssistant {
			return m
		}
	}
	require.FailNow(t, "no assistant message")
	return nil
}

// holdTestDeadline bounds every wait in these tests. A held round over a few
// scripted chunks takes milliseconds; a broken one may never return (a
// segmenter loop that makes no progress spins inside Process, a judgment no
// one answers blocks it), and must fail here, not at the test binary's
// timeout.
const holdTestDeadline = 5 * time.Second

// within returns what f returns, failing the test when f has not returned by
// holdTestDeadline. f runs on its own goroutine, so a call that spins or
// blocks forever still fails the test.
func within[T any](t *testing.T, what string, f func() T) T {
	t.Helper()
	done := make(chan T, 1)
	go func() { done <- f() }()
	select {
	case v := <-done:
		return v
	case <-time.After(holdTestDeadline):
		t.Fatalf("%s did not return within %v", what, holdTestDeadline)
		var zero T
		return zero
	}
}

// liveText renders the live text elements the caller receives, each as
// "<text>" or, when it replaces the turn's text so far, "replace:<text>".
func liveText(elems []StreamElement) []string {
	var out []string
	for _, e := range elems {
		if e.Text == nil || e.Message != nil {
			continue
		}
		if e.Meta.ReplacesText {
			out = append(out, "replace:"+*e.Text)
		} else {
			out = append(out, *e.Text)
		}
	}
	return out
}

// guardedStage is a streaming provider stage reading the test's stream with
// hs as its provider hooks, in order.
func guardedStage(stream chan providers.StreamChunk, hs ...hooks.ProviderHook) *ProviderStage {
	opts := make([]hooks.Option, 0, len(hs))
	for _, h := range hs {
		opts = append(opts, hooks.WithProviderHook(h))
	}
	prov := &scriptedChannelProvider{Provider: mock.NewProvider("p", "m", false), stream: stream}
	return NewProviderStageWithHooks(prov, nil, nil, &ProviderConfig{MaxTokens: 100}, nil, hooks.NewRegistry(opts...))
}

func guardrailHook(t *testing.T, typ string, params map[string]any, opts ...guardrails.GuardrailOption) hooks.ProviderHook {
	t.Helper()
	h, err := guardrails.NewGuardrailHook(typ, params, opts...)
	require.NoError(t, err)
	return h
}

// capturingProvider records the messages the model is actually asked to answer.
//
// The stock mock provider answers regardless of its input, so asserting that a
// turn was produced proves only that the stage ran — not that the transcript
// reached the model. Capturing the request is what distinguishes the two.
type capturingProvider struct {
	*mock.Provider

	mu   sync.Mutex
	seen [][]types.Message
}

func newCapturingProvider() *capturingProvider {
	return &capturingProvider{Provider: mock.NewProvider("capturing", "test-model", false)}
}

// sawText reports whether any captured request carried the given text.
func (p *capturingProvider) sawText(want string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, req := range p.seen {
		for _, m := range req {
			if m.GetContent() == want {
				return true
			}
			for _, part := range m.Parts {
				if part.Text != nil && *part.Text == want {
					return true
				}
			}
		}
	}
	return false
}

// reaskHistorySpy records the messages of every tool-path call, so a test can
// see what the structured-output re-ask sent.
type reaskHistorySpy struct {
	*schemaSpyProvider
	seen [][]types.Message
}

// requestRecordingProvider records the messages of every request.
type requestRecordingProvider struct {
	*providersmock.Provider
	seen [][]types.Message
}

func (p *windowedProvider) MaxContextTokens() int { return p.window }

func (p *windowedProvider) PredictWithTools(
	ctx context.Context, req providers.PredictionRequest, tools providers.ProviderTools, choice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.compacted = append(p.compacted, transcriptCompacted(req.Messages))
	return p.callRecordingProvider.PredictWithTools(ctx, req, tools, choice)
}

func (p *windowedProvider) PredictStreamWithTools(
	ctx context.Context, req providers.PredictionRequest, tools providers.ProviderTools, choice string,
) (<-chan providers.StreamChunk, error) {
	p.compacted = append(p.compacted, transcriptCompacted(req.Messages))
	return p.callRecordingProvider.PredictStreamWithTools(ctx, req, tools, choice)
}

func (h *enforcingChunkInterceptor) Name() string { return "enforcing_chunk_test" }

func (h *enforcingChunkInterceptor) BeforeCall(
	_ context.Context, _ *hooks.ProviderRequest,
) hooks.Decision {
	return hooks.Allow
}

func (h *enforcingChunkInterceptor) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.Allow
}

func (h *enforcingChunkInterceptor) OnChunk(
	_ context.Context, _ *providers.StreamChunk,
) hooks.Decision {
	if h.fired {
		return hooks.Allow
	}
	h.fired = true
	// Enforced rather than Deny: the stage stops reading the stream but returns
	// the message normally, so the stamped validation is observable.
	return hooks.Enforced("streaming guardrail fired", h.meta)
}

func (h *maxCharsChunkInterceptor) Name() string { return "max_chars_test" }

func (h *maxCharsChunkInterceptor) BeforeCall(
	_ context.Context, _ *hooks.ProviderRequest,
) hooks.Decision {
	return hooks.Allow
}

func (h *maxCharsChunkInterceptor) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.Allow
}

func (h *maxCharsChunkInterceptor) OnChunk(
	_ context.Context, chunk *providers.StreamChunk,
) hooks.Decision {
	h.accumulated += chunk.Delta
	if len(h.accumulated) > h.maxChars {
		return hooks.Deny(fmt.Sprintf(
			"content length %d exceeds max %d", len(h.accumulated), h.maxChars,
		))
	}
	return hooks.Allow
}

func (h *denyAllProviderHook) Name() string { return "deny_all" }

func (h *denyAllProviderHook) BeforeCall(
	_ context.Context, _ *hooks.ProviderRequest,
) hooks.Decision {
	return hooks.DenyWithMetadata(h.reason, h.metadata)
}

func (h *denyAllProviderHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.Allow
}

func (h *allowBeforeDenyAfterHook) Name() string { return "deny_after" }

func (h *allowBeforeDenyAfterHook) BeforeCall(
	_ context.Context, _ *hooks.ProviderRequest,
) hooks.Decision {
	return hooks.Allow
}

func (h *allowBeforeDenyAfterHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.DenyWithMetadata(h.reason, h.metadata)
}

func (h *denyToolHook) Name() string { return "deny_tool" }

func (h *denyToolHook) BeforeExecution(
	_ context.Context, req hooks.ToolRequest,
) hooks.Decision {
	if req.Name == h.blockedTool {
		return hooks.Deny(h.reason)
	}
	return hooks.Allow
}

func (h *denyToolHook) AfterExecution(
	_ context.Context, _ hooks.ToolRequest, _ hooks.ToolResponse,
) hooks.Decision {
	return hooks.Allow
}

func (p *redactionRecordingProvider) SupportsStreaming() bool { return p.streaming }

func (p *redactionRecordingProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.record(req)
	return p.Provider.Predict(ctx, req)
}

func (p *redactionRecordingProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.record(req)
	return p.Provider.PredictStream(ctx, req)
}

func (p *redactionRecordingProvider) record(req providers.PredictionRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, req)
	p.calls++
}

// userInputs returns, per request the provider received, the content of the
// request's last user message.
func (p *redactionRecordingProvider) userInputs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, req := range p.requests {
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == "user" {
				out = append(out, req.Messages[i].GetContent())
				break
			}
		}
	}
	return out
}

func (p *redactionRecordingProvider) lastRequest() providers.PredictionRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requests[len(p.requests)-1]
}

func (h *enforceBeforeCallHook) Name() string { return "enforce_before" }

func (h *enforceBeforeCallHook) BeforeCall(
	_ context.Context, req *hooks.ProviderRequest,
) hooks.Decision {
	req.Replacement = h.replacement
	return hooks.Enforced(h.reason, h.metadata)
}

func (h *enforceBeforeCallHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.Allow
}

func (h *enforceAfterCallHook) Name() string { return "enforce_after" }

func (h *enforceAfterCallHook) BeforeCall(
	_ context.Context, _ *hooks.ProviderRequest,
) hooks.Decision {
	return hooks.Allow
}

func (h *enforceAfterCallHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, resp *hooks.ProviderResponse,
) hooks.Decision {
	resp.Message.Content = h.replacement
	return hooks.Enforced("output blocked", map[string]any{
		"validator_type": "test_output",
	})
}

func (p *toolCallingProvider) SupportsStreaming() bool { return false }

func (p *toolCallingProvider) Predict(
	_ context.Context, _ providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return providers.PredictionResponse{
		Content: "let me look that up",
		ToolCalls: []types.MessageToolCall{
			{ID: "call_1", Name: "search", Args: json.RawMessage(`{"q":"x"}`)},
		},
	}, nil
}

func (p *toolCallingProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (p *scriptedRoundProvider) ID() string { return "scripted" }

func (p *scriptedRoundProvider) Name() string { return "scripted" }

func (p *scriptedRoundProvider) Model() string { return "scripted-model" }

func (p *scriptedRoundProvider) Type() base.ProviderType { return base.ProviderTypeInference }

func (p *scriptedRoundProvider) Close() error { return nil }

// SupportsStreaming reports false so the stage takes the unary round path.
func (p *scriptedRoundProvider) SupportsStreaming() bool { return false }

func (p *scriptedRoundProvider) Predict(
	_ context.Context, _ providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{Content: "done"}, nil
}

// BuildTooling passes the descriptors through; the stub ignores their shape.
func (p *scriptedRoundProvider) BuildTooling(
	d []*providers.ToolDescriptor,
) (providers.ProviderTools, error) {
	return d, nil
}

func (p *scriptedRoundProvider) PredictStreamWithTools(
	_ context.Context, _ providers.PredictionRequest,
	_ providers.ProviderTools, _ string,
) (<-chan providers.StreamChunk, error) {
	ch := make(chan providers.StreamChunk)
	close(ch)
	return ch, nil
}

func (p *scriptedRoundProvider) PredictWithTools(
	_ context.Context, _ providers.PredictionRequest,
	_ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.mu.Lock()
	p.calls++
	n := p.calls
	p.mu.Unlock()

	if n > p.toolRounds {
		return providers.PredictionResponse{
			Content:   "done",
			Reasoning: &types.ReasoningTrace{Text: fmt.Sprintf("round %d: I have everything; answer.", n)},
		}, nil, nil
	}
	// The SAME tool called again with corrected arguments — the motivating
	// case from #1840. Timestamp order puts these in sequence but cannot show
	// they came from two different model decisions; the round and the
	// provider-call ID can. Arguments differ per round because identical ones
	// trip the stage's repeated-call breaker.
	return providers.PredictionResponse{
			Reasoning: &types.ReasoningTrace{Text: fmt.Sprintf("round %d: I still need data; calling probe.", n)},
		}, []types.MessageToolCall{{
			ID:   "call_" + string(rune('A'+n-1)),
			Name: "probe",
			Args: json.RawMessage(fmt.Sprintf(`{"q":"attempt-%d"}`, n)),
		}}, nil
}

func (p *multiTurnRecordingProvider) ID() string { return "rec" }

func (p *multiTurnRecordingProvider) Name() string { return "rec" }

func (p *multiTurnRecordingProvider) Type() base.ProviderType { return base.ProviderTypeInference }

func (p *multiTurnRecordingProvider) Pricing() *base.PricingDescriptor { return nil }

func (p *multiTurnRecordingProvider) Validate() error { return nil }

func (p *multiTurnRecordingProvider) Init(_ context.Context) error { return nil }

func (p *multiTurnRecordingProvider) HealthCheck(_ context.Context) error { return nil }

func (p *multiTurnRecordingProvider) Model() string { return "rec-model" }

func (p *multiTurnRecordingProvider) SupportsStreaming() bool { return false }

func (p *multiTurnRecordingProvider) ShouldIncludeRawOutput() bool { return false }

func (p *multiTurnRecordingProvider) CalculateCost(_, _, _ int) types.CostInfo {
	return types.CostInfo{}
}

func (p *multiTurnRecordingProvider) Close() error { return nil }

func (p *multiTurnRecordingProvider) Predict(
	_ context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	msgs := make([]types.Message, len(req.Messages))
	copy(msgs, req.Messages)
	p.requests = append(p.requests, msgs)
	return providers.PredictionResponse{Content: fmt.Sprintf("reply-%d", len(p.requests))}, nil
}

func (p *multiTurnRecordingProvider) PredictStream(
	_ context.Context, _ providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	ch := make(chan providers.StreamChunk)
	close(ch)
	return ch, nil
}

func (p *blockingProvider) Predict(
	ctx context.Context, _ providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.once.Do(func() { close(p.started) })
	<-ctx.Done()
	return providers.PredictionResponse{}, ctx.Err()
}

func (m *mockAsyncExecutor) Name() string {
	return m.name
}

func (m *mockAsyncExecutor) Execute(_ context.Context, descriptor *tools.ToolDescriptor, _ json.RawMessage) (json.RawMessage, error) {
	if m.status == tools.ToolStatusFailed {
		return nil, fmt.Errorf("%s", m.errorMsg)
	}
	return m.content, nil
}

func (m *mockAsyncExecutor) ExecuteAsync(_ context.Context, descriptor *tools.ToolDescriptor, _ json.RawMessage) (*tools.ToolExecutionResult, error) {
	result := &tools.ToolExecutionResult{
		Status:  m.status,
		Content: m.content,
		Parts:   m.parts,
		Error:   m.errorMsg,
	}
	if m.status == tools.ToolStatusPending {
		result.PendingInfo = &tools.PendingToolInfo{
			Reason:   "requires_approval",
			Message:  m.pendingMsg,
			ToolName: descriptor.Name,
		}
	}
	return result, nil
}

func (p *callRecordingProvider) SupportsStreaming() bool { return p.streaming }

func (p *callRecordingProvider) BuildTooling(_ []*providers.ToolDescriptor) (providers.ProviderTools, error) {
	return "tools-of-" + p.ID(), nil
}

func (p *callRecordingProvider) next(req providers.PredictionRequest, tools providers.ProviderTools) bool {
	p.rounds = append(p.rounds, callRound{req.System, req.MaxTokens, req.Temperature, tools})
	return p.transitions && len(p.rounds) == 1
}

func (p *callRecordingProvider) Predict(
	_ context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	// Recorded only: a call without tools cannot carry the scripted transition.
	p.rounds = append(p.rounds, callRound{req.System, req.MaxTokens, req.Temperature, nil})
	return providers.PredictionResponse{Content: "from " + p.ID()}, nil
}

func (p *callRecordingProvider) PredictWithTools(
	_ context.Context, req providers.PredictionRequest, tools providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	if p.next(req, tools) {
		calls := []types.MessageToolCall{{ID: "call-1", Name: "workflow__transition", Args: []byte(`{"event":"Go"}`)}}
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	}
	return providers.PredictionResponse{Content: "from " + p.ID()}, nil, nil
}

func (p *callRecordingProvider) PredictStreamWithTools(
	_ context.Context, req providers.PredictionRequest, tools providers.ProviderTools, _ string,
) (<-chan providers.StreamChunk, error) {
	if !p.streaming {
		return nil, errors.New(p.ID() + " does not stream")
	}
	transition := p.next(req, tools)
	out := make(chan providers.StreamChunk, 1)
	go func() {
		defer close(out)
		if transition {
			reason := "tool_calls"
			out <- providers.StreamChunk{FinishReason: &reason, ToolCalls: []types.MessageToolCall{{
				ID: "call-1", Name: "workflow__transition", Args: []byte(`{"event":"Go"}`),
			}}}
			return
		}
		reason := "stop"
		out <- providers.StreamChunk{Content: "from " + p.ID(), Delta: "from " + p.ID(), FinishReason: &reason}
	}()
	return out, nil
}

func (f *fakeResolver) ResolveCurrentState(_ context.Context) (Handoff, error) {
	if f.err != nil {
		return Handoff{}, f.err
	}
	f.calls++
	if len(f.sequence) == 0 {
		return Handoff{}, nil
	}
	idx := f.calls - 1
	if idx >= len(f.sequence) {
		idx = len(f.sequence) - 1
	}
	return f.sequence[idx], nil
}

func (f *fakeResolver) CurrentStateMeta() map[string]any { return f.meta }

func (p *scriptedChannelProvider) SupportsStreaming() bool { return true }

func (p *scriptedChannelProvider) PredictStream(
	context.Context, providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	return p.stream, nil
}

func (p *capturingProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.record(req.Messages)
	return p.Provider.Predict(ctx, req)
}

func (p *capturingProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.record(req.Messages)
	return p.Provider.PredictStream(ctx, req)
}

func (p *capturingProvider) record(msgs []types.Message) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cp := make([]types.Message, len(msgs))
	copy(cp, msgs)
	p.seen = append(p.seen, cp)
}

func transcriptCompacted(msgs []types.Message) bool {
	for i := range msgs {
		if strings.Contains(msgs[i].GetContent(), compaction.CompactedMarker) {
			return true
		}
	}
	return false
}

// schemaSpyProvider records the ResponseFormat and tool presence of every call
// it receives, across both the tool path and the plain Predict path.
//
// Recording per call is the point: the defect this guards against is a schema
// reaching the WRONG calls, so a spy that only reports "a schema was sent
// somewhere" would pass against the bug.
type schemaSpyProvider struct {
	providers.Provider
	toolRounds int
	failReask  bool
	reaskCost  float64

	mu           sync.Mutex
	calls        []spiedCall
	plainPredict bool
}

func (staticExecutor) Name() string { return "local" }

func (staticExecutor) Execute(
	_ context.Context, d *tools.ToolDescriptor, _ json.RawMessage,
) (json.RawMessage, error) {
	_ = d
	return json.RawMessage(`{"ok":true}`), nil
}

// sawPlainPredict reports whether any re-ask took the non-tool request path.
func (p *schemaSpyProvider) sawPlainPredict() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.plainPredict
}

func (p *schemaSpyProvider) ID() string { return "spy" }

func (p *schemaSpyProvider) Name() string { return "spy" }

func (p *schemaSpyProvider) Model() string { return "spy-model" }

func (p *schemaSpyProvider) Type() base.ProviderType { return base.ProviderTypeInference }

func (p *schemaSpyProvider) Close() error { return nil }

func (p *schemaSpyProvider) SupportsStreaming() bool { return false }

func (p *schemaSpyProvider) BuildTooling(d []*providers.ToolDescriptor) (providers.ProviderTools, error) {
	return d, nil
}

func (p *schemaSpyProvider) record(withTools bool, rf *providers.ResponseFormat) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, spiedCall{withTools: withTools, schema: rf != nil})
}

func (p *schemaSpyProvider) snapshot() []spiedCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]spiedCall, len(p.calls))
	copy(out, p.calls)
	return out
}

func (p *schemaSpyProvider) Predict(
	_ context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.record(false, req.ResponseFormat)
	p.mu.Lock()
	p.plainPredict = true
	p.mu.Unlock()
	// Reject a transcript carrying tool-result roles, exactly as a real
	// provider does. Claude answers
	//   400 messages: Unexpected role "tool"
	// because its plain-Predict serializer is tool-blind — a live-only failure
	// until this fake reproduced it. Without this the re-ask could regress to
	// plain Predict and every canned test here would still pass.
	for i := range req.Messages {
		if req.Messages[i].Role == "tool" {
			return providers.PredictionResponse{}, fmt.Errorf(
				`provider: 400 messages: Unexpected role "tool"`)
		}
	}
	return p.finalAnswer()
}

func (p *schemaSpyProvider) PredictStreamWithTools(
	_ context.Context, _ providers.PredictionRequest,
	_ providers.ProviderTools, _ string,
) (<-chan providers.StreamChunk, error) {
	ch := make(chan providers.StreamChunk)
	close(ch)
	return ch, nil
}

// PredictWithTools serves both roles the stage puts through this method: a
// tool-loop round (tools offered) and the final re-ask (nil tools, taken via
// the tool path only because the transcript carries tool-result roles a plain
// Predict serializer would reject).
//
// withTools therefore records whether tools were OFFERED, not which method was
// called — the re-ask's defining property is that the model has nothing to
// choose from, and recording the method name would report the opposite.
func (p *schemaSpyProvider) PredictWithTools(
	_ context.Context, req providers.PredictionRequest,
	providerTools providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	offered := providerTools != nil
	p.record(offered, req.ResponseFormat)
	if !offered {
		resp, err := p.finalAnswer()
		return resp, nil, err
	}

	p.mu.Lock()
	n := 0
	for _, c := range p.calls {
		if c.withTools {
			n++
		}
	}
	p.mu.Unlock()

	if n > p.toolRounds {
		return providers.PredictionResponse{Content: "unconstrained prose"}, nil, nil
	}
	return providers.PredictionResponse{}, []types.MessageToolCall{{
		ID:   "call_" + string(rune('A'+n-1)),
		Name: "probe",
		Args: json.RawMessage(`{"q":"` + string(rune('a'+n-1)) + `"}`),
	}}, nil
}

// finalAnswer is what a re-ask returns, on either request path.
func (p *schemaSpyProvider) finalAnswer() (providers.PredictionResponse, error) {
	if p.failReask {
		return providers.PredictionResponse{}, assert.AnError
	}
	resp := providers.PredictionResponse{Content: `{"answer":"constrained"}`}
	if p.reaskCost > 0 {
		resp.CostInfo = &types.CostInfo{TotalCost: p.reaskCost}
	}
	return resp, nil
}

type spiedCall struct {
	withTools bool
	schema    bool
}
