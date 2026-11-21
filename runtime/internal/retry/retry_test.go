package retry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

var errTransient = errors.New("transient")

// script returns an op that fails with errs in order, then succeeds with
// "ok", and a pointer to its call count.
func script(errs ...error) (func(context.Context) (string, error), *int) {
	calls := 0
	return func(context.Context) (string, error) {
		calls++
		if calls <= len(errs) {
			return "partial", errs[calls-1]
		}
		return "ok", nil
	}, &calls
}

// recordOutcomes installs an Observer for the test and returns what it saw.
func recordOutcomes(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	SetObserver(func(name string, o Outcome) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, name+":"+string(o))
	})
	t.Cleanup(func() { SetObserver(nil) })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func fastPolicy(attempts int) Policy {
	return Policy{Name: "site", MaxAttempts: attempts, Backoff: Backoff{Initial: time.Millisecond, Max: time.Millisecond}}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDo_RetriesUntilSuccessAndReportsEachDecision(t *testing.T) {
	seen := recordOutcomes(t)
	op, calls := script(errTransient, errTransient)

	got, err := Do(context.Background(), fastPolicy(3), op)
	if err != nil || got != "ok" {
		t.Fatalf("Do = %q, %v; want ok, nil", got, err)
	}
	if *calls != 3 {
		t.Errorf("calls = %d, want 3", *calls)
	}
	if want := []string{"site:retry", "site:retry", "site:success"}; !equal(seen(), want) {
		t.Errorf("outcomes = %v, want %v", seen(), want)
	}
}

func TestDo_FirstTrySuccessReportsNothing(t *testing.T) {
	seen := recordOutcomes(t)
	op, calls := script()
	if _, err := Do(context.Background(), fastPolicy(3), op); err != nil {
		t.Fatal(err)
	}
	if *calls != 1 || len(seen()) != 0 {
		t.Errorf("calls = %d, outcomes = %v; want 1 and none", *calls, seen())
	}
}

func TestDo_ExhaustionReturnsTheLastAttemptsValueAndError(t *testing.T) {
	seen := recordOutcomes(t)
	last := errors.New("third")
	op, calls := script(errTransient, errTransient, last)

	got, err := Do(context.Background(), fastPolicy(3), op)
	if err != last || got != "partial" {
		t.Fatalf("Do = %q, %v; want partial, %v", got, err, last)
	}
	if *calls != 3 {
		t.Errorf("calls = %d, want 3", *calls)
	}
	if want := []string{"site:retry", "site:retry", "site:exhausted"}; !equal(seen(), want) {
		t.Errorf("outcomes = %v, want %v", seen(), want)
	}
}

func TestDo_UnretryableFailureIsReturnedAtOnce(t *testing.T) {
	seen := recordOutcomes(t)
	fatal := errors.New("fatal")
	op, calls := script(fatal)
	p := fastPolicy(5)
	p.Classify = func(err error) (bool, time.Duration) { return err != fatal, 0 }

	if _, err := Do(context.Background(), p, op); err != fatal {
		t.Fatalf("err = %v, want %v", err, fatal)
	}
	if *calls != 1 || len(seen()) != 0 {
		t.Errorf("calls = %d, outcomes = %v; want 1 and none", *calls, seen())
	}
}

func TestDo_MaxAttemptsBelowOneMakesOneAttempt(t *testing.T) {
	op, calls := script(errTransient, errTransient)
	if _, err := Do(context.Background(), fastPolicy(0), op); err != errTransient {
		t.Fatalf("err = %v, want %v", err, errTransient)
	}
	if *calls != 1 {
		t.Errorf("calls = %d, want 1", *calls)
	}
}

func TestDo_NilClassifyRetriesAllButContextErrors(t *testing.T) {
	op, calls := script(errTransient, context.DeadlineExceeded)
	if _, err := Do(context.Background(), fastPolicy(5), op); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the deadline error", err)
	}
	if *calls != 2 {
		t.Errorf("calls = %d, want 2", *calls)
	}
}

func TestDo_EndedContextMakesNoAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	op, calls := script()
	if _, err := Do(ctx, fastPolicy(3), op); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if *calls != 0 {
		t.Errorf("calls = %d, want 0", *calls)
	}
}

func TestDo_CancelDuringBackoffReturnsTheContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	op := func(context.Context) (string, error) {
		calls++
		cancel()
		return "partial", errTransient
	}
	p := Policy{MaxAttempts: 5, Backoff: Backoff{Initial: time.Hour}}

	start := time.Now()
	got, err := Do(ctx, p, op)
	if !errors.Is(err, context.Canceled) || got != "" {
		t.Fatalf("Do = %q, %v; want empty, context.Canceled", got, err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
	if time.Since(start) > time.Second {
		t.Error("the backoff was waited out")
	}
}

// A failure's own wait replaces the backoff: an hour-long backoff does not
// delay a retry the failure asked to make after a millisecond.
func TestDo_FailuresWaitReplacesTheBackoff(t *testing.T) {
	op, calls := script(errTransient)
	p := Policy{MaxAttempts: 2, Backoff: Backoff{Initial: time.Hour},
		Classify: func(error) (bool, time.Duration) { return true, time.Millisecond }}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := Do(ctx, p, op); err != nil {
		t.Fatalf("err = %v, want success after the short wait", err)
	}
	if *calls != 2 {
		t.Errorf("calls = %d, want 2", *calls)
	}
}

func TestBackoff_Delay(t *testing.T) {
	cases := []struct {
		name     string
		b        Backoff
		attempt  int
		min, max time.Duration
	}{
		{"exponential", Backoff{Initial: 100 * time.Millisecond}, 2, 400 * time.Millisecond, 400 * time.Millisecond},
		{"fixed", Backoff{Initial: 100 * time.Millisecond, Multiplier: 1}, 3, 100 * time.Millisecond, 100 * time.Millisecond},
		{"capped", Backoff{Initial: time.Second, Max: 3 * time.Second}, 5, 3 * time.Second, 3 * time.Second},
		{"overflowing attempt stays capped", Backoff{Initial: time.Second, Max: 3 * time.Second}, 100, 3 * time.Second, 3 * time.Second},
		{"overflowing attempt with no Max saturates", Backoff{Initial: time.Second}, 5000, maxUncapped, maxUncapped},
		{"saturated and jittered stays positive", Backoff{Initial: time.Second, JitterMin: 1, JitterMax: 1.5}, 5000, maxUncapped, maxUncapped},
		{"no initial delay is no wait", Backoff{Max: time.Second}, 5000, 0, 0},
		{"negative attempt is the first", Backoff{Initial: time.Second}, -1, time.Second, time.Second},
		{"jitter scales", Backoff{Initial: time.Second, JitterMin: 0.5, JitterMax: 1}, 0, 500 * time.Millisecond, time.Second},
		{"jitter never passes Max", Backoff{Initial: time.Second, Max: time.Second, JitterMin: 1, JitterMax: 1.5}, 0, time.Second, time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for range 50 {
				if d := tc.b.Delay(tc.attempt); d < tc.min || d > tc.max {
					t.Fatalf("Delay(%d) = %v, want within [%v, %v]", tc.attempt, d, tc.min, tc.max)
				}
			}
		})
	}
}

func TestBackoff_JitterVaries(t *testing.T) {
	b := Backoff{Initial: time.Second, JitterMin: 0, JitterMax: 1}
	first := b.Delay(0)
	for range 50 {
		if b.Delay(0) != first {
			return
		}
	}
	t.Fatalf("50 delays all %v: no jitter", first)
}

func TestParseRetryAfter(t *testing.T) {
	future := time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)
	saturated := []struct{ val string }{{"9300000000"}, {"99999999999999999999999"}}
	for _, tc := range saturated {
		h := http.Header{"Retry-After": {tc.val}}
		if got := ParseRetryAfter(h); got != maxUncapped {
			t.Errorf("ParseRetryAfter(%q) = %v, want the saturated %v", tc.val, got, maxUncapped)
		}
	}
	if got := ParseRetryAfter(http.Header{"Retry-After": {"-99999999999999999999999"}}); got != 0 {
		t.Errorf("ParseRetryAfter(huge negative) = %v, want 0", got)
	}
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	cases := []struct {
		val      string
		min, max time.Duration
	}{
		{"", 0, 0},
		{"7", 7 * time.Second, 7 * time.Second},
		{"0", 0, 0},
		{"-3", 0, 0},
		{"soon", 0, 0},
		{past, 0, 0},
		{future, 59 * time.Minute, time.Hour},
	}
	for _, tc := range cases {
		h := http.Header{}
		if tc.val != "" {
			h.Set("Retry-After", tc.val)
		}
		if got := ParseRetryAfter(h); got < tc.min || got > tc.max {
			t.Errorf("ParseRetryAfter(%q) = %v, want within [%v, %v]", tc.val, got, tc.min, tc.max)
		}
	}
}

func TestSleep(t *testing.T) {
	if err := Sleep(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("Sleep = %v, want nil", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep = %v, want context.Canceled", err)
	}
}

func TestPolicy_Wait(t *testing.T) {
	p := Policy{Backoff: Backoff{Initial: time.Second, Multiplier: 1}, MaxRetryAfter: 10 * time.Second}
	cases := []struct {
		name   string
		after  time.Duration
		want   time.Duration
		wantOK bool
	}{
		{"no wait asked: the backoff", 0, time.Second, true},
		{"a wait under the limit", 3 * time.Second, 3 * time.Second, true},
		{"a wait at the limit", 10 * time.Second, 10 * time.Second, true},
		{"a wait past the limit: no retry", 10*time.Second + 1, 0, false},
	}
	for _, tc := range cases {
		got, ok := p.Wait(0, tc.after)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("%s: Wait = %v, %v; want %v, %v", tc.name, got, ok, tc.want, tc.wantOK)
		}
	}
	unlimited := Policy{}
	if got, ok := unlimited.Wait(0, time.Hour); got != time.Hour || !ok {
		t.Errorf("unlimited Wait = %v, %v; want 1h, true", got, ok)
	}
}

// A one-shot call's policy names its site kind/provider, waits full jitter up
// to the ceiling, and caps a failure's own wait at the ceiling.
func TestFullJitterPolicy(t *testing.T) {
	classify := func(error) (bool, time.Duration) { return true, time.Hour }
	p := FullJitterPolicy("tts", "openai", 3, 10*time.Millisecond, time.Second, classify)
	if p.Name != "tts/openai" || p.MaxAttempts != 3 {
		t.Errorf("Name, MaxAttempts = %q, %d; want tts/openai, 3", p.Name, p.MaxAttempts)
	}
	if want := (Backoff{Initial: 10 * time.Millisecond, Max: time.Second, JitterMax: 1}); p.Backoff != want {
		t.Errorf("Backoff = %+v, want %+v", p.Backoff, want)
	}
	_, after := p.Classify(errTransient)
	if _, ok := p.Wait(0, after); ok {
		t.Error("an hour-long Retry-After past the 1s ceiling was retried")
	}
	if got, ok := p.Wait(0, time.Second); got != time.Second || !ok {
		t.Errorf("Wait for a Retry-After at the ceiling = %v, %v; want 1s, true", got, ok)
	}
}

// A failure asking to wait past MaxRetryAfter is returned at once, without a
// retry, and reported as such.
func TestDo_RetryAfterPastTheLimitStopsRetrying(t *testing.T) {
	seen := recordOutcomes(t)
	op, calls := script(errTransient)
	p := fastPolicy(5)
	p.MaxRetryAfter = time.Second
	p.Classify = func(error) (bool, time.Duration) { return true, time.Hour }

	start := time.Now()
	got, err := Do(context.Background(), p, op)
	if err != errTransient || got != "partial" {
		t.Fatalf("Do = %q, %v; want partial, %v", got, err, errTransient)
	}
	if *calls != 1 {
		t.Errorf("calls = %d, want 1", *calls)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Error("the Retry-After was waited")
	}
	if want := []string{"site:retry_after_too_long"}; !equal(seen(), want) {
		t.Errorf("outcomes = %v, want %v", seen(), want)
	}
}

// A failure after the context ended is not a retry: nothing is reported, and
// the context's error comes back.
func TestDo_FailureAfterCancelIsNotCountedAsARetry(t *testing.T) {
	seen := recordOutcomes(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	op := func(context.Context) (string, error) {
		cancel()
		return "partial", errTransient
	}
	got, err := Do(ctx, fastPolicy(5), op)
	if err != context.Canceled || got != "" {
		t.Fatalf("Do = %q, %v; want empty, context.Canceled", got, err)
	}
	if len(seen()) != 0 {
		t.Errorf("outcomes = %v, want none", seen())
	}

	// An attempt whose own error names the context's end keeps it.
	wrapped := fmt.Errorf("dial: %w", context.Canceled)
	ctx2, cancel2 := context.WithCancel(context.Background())
	op2 := func(context.Context) (string, error) {
		cancel2()
		return "partial", wrapped
	}
	if got, err := Do(ctx2, fastPolicy(5), op2); err != wrapped || got != "partial" {
		t.Fatalf("Do = %q, %v; want partial, %v", got, err, wrapped)
	}
	if len(seen()) != 0 {
		t.Errorf("outcomes = %v, want none", seen())
	}
}

// A context ending while Do waits to retry returns the context's error.
func TestDo_CancelDuringTheWaitReturnsTheContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	op, calls := script(errTransient, errTransient)
	p := Policy{MaxAttempts: 5, Backoff: Backoff{Initial: time.Hour}}
	time.AfterFunc(20*time.Millisecond, cancel)

	start := time.Now()
	if _, err := Do(ctx, p, op); err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if *calls != 1 || time.Since(start) > time.Second {
		t.Errorf("calls = %d after %v; want 1, promptly", *calls, time.Since(start))
	}
}

// One attempt configured is no retry policy at all: a retryable failure is
// returned and nothing is reported.
func TestDo_SingleAttemptReportsNothing(t *testing.T) {
	seen := recordOutcomes(t)
	op, calls := script(errTransient)
	if _, err := Do(context.Background(), fastPolicy(1), op); err != errTransient {
		t.Fatalf("err = %v, want %v", err, errTransient)
	}
	if *calls != 1 || len(seen()) != 0 {
		t.Errorf("calls = %d, outcomes = %v; want 1 and none", *calls, seen())
	}
}
