package adaptersdk

import (
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"
)

func TestParsePack_Valid(t *testing.T) {
	input := `{"id":"my-pack","name":"My Pack","version":"1.0.0"}`
	pack, err := ParsePack([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pack.ID != "my-pack" {
		t.Errorf("expected id my-pack, got %s", pack.ID)
	}
	if pack.Name != "My Pack" {
		t.Errorf("expected name My Pack, got %s", pack.Name)
	}
}

func TestParsePack_Invalid(t *testing.T) {
	_, err := ParsePack([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParsePack_Empty(t *testing.T) {
	pack, err := ParsePack([]byte("{}"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pack == nil {
		t.Fatal("expected non-nil pack")
	}
}

func TestProgressReporter_Progress(t *testing.T) {
	var events []*deploy.ApplyEvent
	cb := func(e *deploy.ApplyEvent) error {
		events = append(events, e)
		return nil
	}
	pr := NewProgressReporter(cb)

	err := pr.Progress("deploying", 0.5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "progress" {
		t.Errorf("expected type progress, got %s", events[0].Type)
	}
	if events[0].Message != "deploying (50%)" {
		t.Errorf("expected 'deploying (50%%)', got %s", events[0].Message)
	}
}

func TestProgressReporter_ProgressOutOfRange(t *testing.T) {
	var events []*deploy.ApplyEvent
	cb := func(e *deploy.ApplyEvent) error {
		events = append(events, e)
		return nil
	}
	pr := NewProgressReporter(cb)

	_ = pr.Progress("bad value", -0.5)
	if events[0].Message != "bad value" {
		t.Errorf("expected no percentage for negative, got %s", events[0].Message)
	}

	_ = pr.Progress("too high", 1.5)
	if events[1].Message != "too high" {
		t.Errorf("expected no percentage for >100%%, got %s", events[1].Message)
	}
}

func TestProgressReporter_Resource(t *testing.T) {
	var events []*deploy.ApplyEvent
	cb := func(e *deploy.ApplyEvent) error {
		events = append(events, e)
		return nil
	}
	pr := NewProgressReporter(cb)

	err := pr.Resource(&deploy.ResourceResult{
		Type:   "agent_runtime",
		Name:   "main",
		Action: deploy.ActionCreate,
		Status: "created",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "resource" {
		t.Errorf("expected type resource, got %s", events[0].Type)
	}
	if events[0].Resource == nil {
		t.Fatal("expected non-nil resource")
	}
	if events[0].Resource.Name != "main" {
		t.Errorf("expected resource name main, got %s", events[0].Resource.Name)
	}
}

func TestProgressReporter_Error(t *testing.T) {
	var events []*deploy.ApplyEvent
	cb := func(e *deploy.ApplyEvent) error {
		events = append(events, e)
		return nil
	}
	pr := NewProgressReporter(cb)

	err := pr.Error(errors.New("something went wrong"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "error" {
		t.Errorf("expected type error, got %s", events[0].Type)
	}
	if events[0].Message != "something went wrong" {
		t.Errorf("expected error message, got %s", events[0].Message)
	}
}

func TestProgressReporter_CallbackError(t *testing.T) {
	cbErr := errors.New("callback failed")
	cb := func(_ *deploy.ApplyEvent) error {
		return cbErr
	}
	pr := NewProgressReporter(cb)

	err := pr.Progress("test", 0.5)
	if !errors.Is(err, cbErr) {
		t.Errorf("expected callback error, got %v", err)
	}
}

func TestFormatProgress_Boundaries(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		pct  float64
		want string
	}{
		{"zero", "start", 0.0, "start (0%)"},
		{"full", "done", 1.0, "done (100%)"},
		{"mid", "half", 0.5, "half (50%)"},
		{"negative", "neg", -0.1, "neg"},
		{"over", "over", 1.01, "over"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatProgress(tt.msg, tt.pct)
			if got != tt.want {
				t.Errorf("formatProgress(%q, %f) = %q, want %q",
					tt.msg, tt.pct, got, tt.want)
			}
		})
	}
}
