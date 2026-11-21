package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/AltairaLabs/PromptKit/runtime/v2/internal/retry"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
)

// statusSequenceServer answers with the given status codes in order, then
// repeats the last one.
func statusSequenceServer(t *testing.T, codes ...int) *httptest.Server {
	t.Helper()
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		i := int(atomic.AddInt32(&n, 1)) - 1
		if i >= len(codes) {
			i = len(codes) - 1
		}
		w.WriteHeader(codes[i])
	}))
	t.Cleanup(srv.Close)
	return srv
}

func registerTestRetryMetrics(t *testing.T) *StreamMetrics {
	t.Helper()
	ResetDefaultStreamMetrics()
	t.Cleanup(ResetDefaultStreamMetrics)
	return RegisterDefaultStreamMetrics(prometheus.NewRegistry(), "test", nil)
}

func doAgainst(t *testing.T, srv *httptest.Server, maxRetries int) {
	t.Helper()
	policy := pipeline.RetryPolicy{MaxRetries: maxRetries, Backoff: "fixed", InitialDelayMs: 1}
	resp, err := DoWithRetry(t.Context(), policy, "p1", func() (*http.Response, error) {
		return http.Get(srv.URL)
	})
	if resp != nil {
		_ = resp.Body.Close()
	}
	_ = err
}

// Each scheduled retry is counted, and a call that recovers records one
// "success" — so a backend that only works on the second try is visible.
func TestDoWithRetry_CountsRetriesAndRecovery(t *testing.T) {
	m := registerTestRetryMetrics(t)

	doAgainst(t, statusSequenceServer(t, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusOK), 3)

	if got := testutil.ToFloat64(m.providerRetriesTotal.WithLabelValues("p1", "retry")); got != 2 {
		t.Errorf("retry count = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.providerRetriesTotal.WithLabelValues("p1", "success")); got != 1 {
		t.Errorf("success-after-retry count = %v, want 1", got)
	}
}

func TestDoWithRetry_CountsExhaustion(t *testing.T) {
	m := registerTestRetryMetrics(t)

	doAgainst(t, statusSequenceServer(t, http.StatusServiceUnavailable), 2)

	if got := testutil.ToFloat64(m.providerRetriesTotal.WithLabelValues("p1", "retry")); got != 2 {
		t.Errorf("retry count = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.providerRetriesTotal.WithLabelValues("p1", "exhausted")); got != 1 {
		t.Errorf("exhausted count = %v, want 1", got)
	}
}

// A first-try success is not a retry event; the counter stays silent.
func TestDoWithRetry_FirstTrySuccessRecordsNothing(t *testing.T) {
	m := registerTestRetryMetrics(t)

	doAgainst(t, statusSequenceServer(t, http.StatusOK), 3)

	if got := testutil.CollectAndCount(m.providerRetriesTotal); got != 0 {
		t.Errorf("recorded %d series, want 0", got)
	}
}

// Every site that retries through the shared loop (A2A, TTS, STT, MCP,
// realtime connects) lands on the same metric, under its own name; after a
// reset nothing more is recorded.
func TestSharedRetryLoop_ReportsToTheProviderRetryMetric(t *testing.T) {
	m := registerTestRetryMetrics(t)
	failOnce := func() func(context.Context) (int, error) {
		n := 0
		return func(context.Context) (int, error) {
			if n++; n == 1 {
				return 0, errors.New("transient")
			}
			return 1, nil
		}
	}
	policy := retry.Policy{Name: "a2a", MaxAttempts: 2, Backoff: retry.Backoff{Initial: time.Millisecond}}

	if _, err := retry.Do(t.Context(), policy, failOnce()); err != nil {
		t.Fatal(err)
	}
	if got := testutil.ToFloat64(m.providerRetriesTotal.WithLabelValues("a2a", "retry")); got != 1 {
		t.Errorf("a2a retry count = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.providerRetriesTotal.WithLabelValues("a2a", "success")); got != 1 {
		t.Errorf("a2a success-after-retry count = %v, want 1", got)
	}

	ResetDefaultStreamMetrics()
	if _, err := retry.Do(t.Context(), policy, failOnce()); err != nil {
		t.Fatal(err)
	}
	if got := testutil.ToFloat64(m.providerRetriesTotal.WithLabelValues("a2a", "retry")); got != 1 {
		t.Errorf("after reset: a2a retry count = %v, want still 1", got)
	}
}

// A policy of no retries has nothing to count: a 503 with MaxRetries 0
// records no "exhausted".
func TestDoWithRetry_NoRetriesConfiguredRecordsNothing(t *testing.T) {
	m := registerTestRetryMetrics(t)

	doAgainst(t, statusSequenceServer(t, http.StatusServiceUnavailable), 0)

	if got := testutil.CollectAndCount(m.providerRetriesTotal); got != 0 {
		t.Errorf("recorded %d series, want 0", got)
	}
}
