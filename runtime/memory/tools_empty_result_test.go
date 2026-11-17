package memory

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// A recall or list that matches nothing returns an empty memories array, so
// the result satisfies the tool's own output schema (which requires an array)
// instead of failing validation with "memories: Expected array, given null".
func TestMemoryTools_EmptyResultIsAnEmptyArray(t *testing.T) {
	cases := map[string]struct {
		tool string
		args map[string]any
	}{
		"recall": {RecallToolName, map[string]any{"query": "nothing matches this"}},
		"list":   {ListToolName, map[string]any{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			registry := tools.NewRegistry()
			RegisterMemoryTools(registry)
			registry.RegisterExecutor(NewExecutor(NewInMemoryStore(), map[string]string{"user_id": "u1"}))

			args, err := json.Marshal(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			res, err := registry.Execute(context.Background(), tc.tool, args)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if res.Error != "" {
				t.Fatalf("result error = %q, want none", res.Error)
			}
			if got, want := string(res.Result), `{"count":0,"memories":[]}`; got != want {
				t.Errorf("result = %s, want %s", got, want)
			}
		})
	}
}
