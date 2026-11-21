package retry

import (
	"context"
	"errors"
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
		name  string
		after time.Duration
		want  time.Duration
	}{
		{"no wait asked: the backoff", 0, time.Second},
		{"a wait under the cap", 3 * time.Second, 3 * time.Second},
		{"a wait at the cap", 10 * time.Second, 10 * time.Second},
		{"a wait past the cap", time.Minute, 10 * time.Second},
	}
	for _, tc := range cases {
		if got := p.Wait(0, tc.after); got != tc.want {
			t.Errorf("%s: Wait = %v, want %v", tc.name, got, tc.want)
		}
	}
	uncapped := Policy{}
	if got := uncapped.Wait(0, time.Hour); got != time.Hour {
		t.Errorf("uncapped Wait = %v, want 1h", got)
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
	if got := p.Wait(0, after); got != time.Second {
		t.Errorf("Wait for an hour-long Retry-After = %v, want the 1s ceiling", got)
	}
}
