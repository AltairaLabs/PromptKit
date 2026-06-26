package main

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"
)

type fakePlanner struct {
	resp   *deploy.PlanResponse
	err    error
	called bool
}

func (f *fakePlanner) Plan(_ context.Context, _ *deploy.PlanRequest) (*deploy.PlanResponse, error) {
	f.called = true
	return f.resp, f.err
}

func TestApplyWarnings_SavedPlan_UsesSavedWarningsWithoutReplanning(t *testing.T) {
	saved := &deploy.SavedPlan{Plan: &deploy.PlanResponse{Warnings: []string{"a", "b"}}}
	fp := &fakePlanner{}
	got := applyWarnings(context.Background(), fp, true, saved, &deploy.PlanRequest{})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("want [a b], got %v", got)
	}
	if fp.called {
		t.Error("must not re-plan when a saved plan is reused")
	}
}

func TestApplyWarnings_Replan_FetchesFreshWarnings(t *testing.T) {
	fp := &fakePlanner{resp: &deploy.PlanResponse{Warnings: []string{"fresh"}}}
	got := applyWarnings(context.Background(), fp, false, nil, &deploy.PlanRequest{})
	if !fp.called {
		t.Error("expected a fresh plan call when not using a saved plan")
	}
	if len(got) != 1 || got[0] != "fresh" {
		t.Fatalf("want [fresh], got %v", got)
	}
}

func TestApplyWarnings_PlanError_NoWarnings(t *testing.T) {
	fp := &fakePlanner{err: errors.New("boom")}
	got := applyWarnings(context.Background(), fp, false, nil, &deploy.PlanRequest{})
	if got != nil {
		t.Fatalf("want nil on plan error, got %v", got)
	}
}
