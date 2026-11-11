package integration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	sdk "github.com/AltairaLabs/PromptKit/sdk/v2"
)

// The helpers in this file serve the eval end-to-end tests (eval_e2e_*_test.go).
// Every test goes from a spec-valid pack JSON through sdk.Open / sdk.Resume /
// Send / Close to what a host sees: the results its eval hook receives, the
// events on its bus and the metrics in its registry. The packs are validated
// against the embedded PromptPack schema; none of these tests skips it.

// e2ePack renders a spec-valid pack with one prompt, "chat". promptExtra is
// spliced into the prompt object after system_template (start it with a
// comma), packEvals is the body of the pack-level "evals" array, and
// packExtra is spliced into the pack object (start it with a comma).
func e2ePack(promptExtra, packEvals, packExtra string) string {
	evalsField := ""
	if packEvals != "" {
		evalsField = `, "evals": [` + packEvals + `]`
	}
	return fmt.Sprintf(`{
	"id": "e2e-evals",
	"name": "E2E evals",
	"version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"version": "1.0.0",
			"system_template": "You are terse."%s
		}
	}%s%s
}`, promptExtra, evalsField, packExtra)
}

// replyProvider is the real mock provider answering every turn with reply.
func replyProvider(reply string) *mock.Provider {
	return mock.NewProviderWithRepository("mock", "mock-model", false, mock.NewInMemoryMockRepository(reply))
}

// resultLog is an evals.EvalHook (sdk.WithEvalHook) that records every result
// the conversation's runner produces, and how many messages the eval saw.
type resultLog struct {
	mu      sync.Mutex
	results []evals.EvalResult
	seen    []int // len(evalCtx.Messages) per result, in result order
}

func (l *resultLog) Name() string { return "e2e-result-log" }

func (l *resultLog) OnEvalResult(_ context.Context, _ *evals.EvalDef, ec *evals.EvalContext, r *evals.EvalResult) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.results = append(l.results, *r)
	l.seen = append(l.seen, len(ec.Messages))
}

// byID returns the results recorded for one eval id, in order.
func (l *resultLog) byID(id string) []evals.EvalResult {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []evals.EvalResult
	for i := range l.results {
		if l.results[i].EvalID == id {
			out = append(out, l.results[i])
		}
	}
	return out
}

// messagesSeen returns the transcript length each result for id was given.
func (l *resultLog) messagesSeen(id string) []int {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []int
	for i := range l.results {
		if l.results[i].EvalID == id {
			out = append(out, l.seen[i])
		}
	}
	return out
}

// ids returns the set of eval ids that produced a result.
func (l *resultLog) ids() map[string]int {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]int{}
	for i := range l.results {
		out[l.results[i].EvalID]++
	}
	return out
}

// only returns the single result for id, failing the test otherwise.
func (l *resultLog) only(t *testing.T, id string) evals.EvalResult {
	t.Helper()
	got := l.byID(id)
	require.Lenf(t, got, 1, "eval %q: want exactly one result, got %d (all results: %v)", id, len(got), l.ids())
	return got[0]
}

// openE2E opens "chat" from packJSON through sdk.Open, with the real mock
// provider answering reply, schema validation on and a result log attached.
// The returned conversation is closed by the test's cleanup if the test did
// not close it.
func openE2E(t *testing.T, packJSON, reply string, opts ...sdk.Option) (*sdk.Conversation, *resultLog) {
	t.Helper()
	log := &resultLog{}
	all := append([]sdk.Option{
		sdk.WithProvider(replyProvider(reply)),
		sdk.WithEvalHook(log),
	}, opts...)
	conv, err := sdk.Open(writePackFile(t, packJSON), "chat", all...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv, log
}

// sendAndClose sends each message on conv and closes it, so every turn eval
// has finished and the session evals have run. Close ends a session, so the
// conversation evals do not run; sendAndComplete runs them.
func sendAndClose(t *testing.T, conv *sdk.Conversation, messages ...string) []*sdk.Response {
	t.Helper()
	out := sendAll(t, conv, messages...)
	require.NoError(t, conv.Close())
	return out
}

// sendAndComplete sends each message on conv and completes it, so the turn,
// session and conversation evals have all run.
func sendAndComplete(t *testing.T, conv *sdk.Conversation, messages ...string) []*sdk.Response {
	t.Helper()
	out := sendAll(t, conv, messages...)
	require.NoError(t, conv.Complete())
	return out
}

// sendAll sends each message on conv.
func sendAll(t *testing.T, conv *sdk.Conversation, messages ...string) []*sdk.Response {
	t.Helper()
	var out []*sdk.Response
	for _, m := range messages {
		resp, err := conv.Send(context.Background(), m)
		require.NoError(t, err)
		out = append(out, resp)
	}
	return out
}

// passed reports a result's verdict as "pass", "fail" or "none".
func passed(r evals.EvalResult) string {
	switch {
	case r.Passed == nil:
		return "none"
	case *r.Passed:
		return "pass"
	default:
		return "fail"
	}
}

// score returns a result's score, failing the test when it has none.
func score(t *testing.T, r evals.EvalResult) float64 {
	t.Helper()
	require.NotNilf(t, r.Score, "eval %q has no score (error %q)", r.EvalID, r.Error)
	return *r.Score
}

// requestRecorder wraps the real mock provider and records every prediction
// request it receives, so a test can read what reached the model.
type requestRecorder struct {
	providers.Provider
	mu   sync.Mutex
	reqs []providers.PredictionRequest
}

func (r *requestRecorder) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, req)
	r.mu.Unlock()
	return r.Provider.Predict(ctx, req)
}

func (r *requestRecorder) requests() []providers.PredictionRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]providers.PredictionRequest(nil), r.reqs...)
}

// scenarioRepo serves the real FileMockRepository's turns for one scenario.
// The SDK sends no mock scenario id, so without this every turn would get the
// file's default response.
type scenarioRepo struct {
	inner    *mock.FileMockRepository
	scenario string
}

func (s scenarioRepo) GetResponse(ctx context.Context, p mock.ResponseParams) (string, error) {
	p.ScenarioID = s.scenario
	return s.inner.GetResponse(ctx, p)
}

func (s scenarioRepo) GetTurn(ctx context.Context, p mock.ResponseParams) (*mock.Turn, error) {
	p.ScenarioID = s.scenario
	return s.inner.GetTurn(ctx, p)
}

// mockYAMLRepo writes a mock provider config file and loads it through the
// real FileMockRepository, serving the scenario named "e2e".
func mockYAMLRepo(t *testing.T, yaml string) mock.ResponseRepository {
	t.Helper()
	path := t.TempDir() + "/mock.yaml"
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))
	repo, err := mock.NewFileMockRepository(path)
	require.NoError(t, err)
	return scenarioRepo{inner: repo, scenario: "e2e"}
}

// replaceOnce replaces the first old in s with new, panicking when s does not
// contain old, so a fixture edit cannot silently do nothing.
func replaceOnce(s, old, replacement string) string {
	if !strings.Contains(s, old) {
		panic("replaceOnce: fixture does not contain " + old)
	}
	return strings.Replace(s, old, replacement, 1)
}
