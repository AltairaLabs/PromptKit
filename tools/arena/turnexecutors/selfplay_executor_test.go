package turnexecutors

import (
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestBuildSelfPlayMetadata_MergesFields(t *testing.T) {
	e := &SelfPlayExecutor{}
	req := TurnRequest{SelfPlayRole: "challenger"}
	exec := &pipeline.ExecutionResult{Metadata: map[string]interface{}{"persona": "p1", "custom": 123}}

	meta := e.buildSelfPlayMetadata(&req, exec)
	if meta["role"] != "challenger" || meta["self_play_execution"] != true {
		t.Fatalf("missing base metadata: %+v", meta)
	}
	if meta["persona"] != "p1" || meta["custom"].(int) != 123 {
		t.Fatalf("did not merge exec metadata: %+v", meta)
	}
}

func TestBuildUserMessageFromResult_SetsLatencyAndMeta(t *testing.T) {
	e := &SelfPlayExecutor{}
	req := TurnRequest{SelfPlayRole: "challenger"}
	start := time.Now().Add(-1500 * time.Millisecond)
	end := time.Now()

	exec := &pipeline.ExecutionResult{
		Response: &pipeline.Response{Content: "hello"},
		Trace:    pipeline.ExecutionTrace{CompletedAt: &end, StartedAt: start},
		CostInfo: types.CostInfo{InputTokens: 1, OutputTokens: 2},
		Metadata: map[string]interface{}{"persona": "p1"},
	}

	msg := e.buildUserMessageFromResult(&req, exec)
	if msg.Role != "user" || msg.Content != "hello" {
		t.Fatalf("unexpected message: %+v", msg)
	}
	if msg.LatencyMs <= 0 {
		t.Fatalf("expected latency to be set, got %d", msg.LatencyMs)
	}
	raw, ok := msg.Meta["raw_response"].(map[string]interface{})
	if !ok || raw["role"] != "challenger" || raw["persona"] != "p1" {
		t.Fatalf("raw_response metadata missing/incorrect: %+v", msg.Meta)
	}
}
