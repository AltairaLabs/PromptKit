package assertions

import (
	"testing"
)

func TestToolCallSequenceValidator(t *testing.T) {
	tests := []struct {
		name       string
		sequence   []string
		toolCalls  []testToolCall
		wantPassed bool
	}{
		{
			name:     "exact sequence match",
			sequence: []string{"read_file", "edit_file"},
			toolCalls: []testToolCall{
				{id: "c1", name: "read_file", result: "content", round: 0},
				{id: "c2", name: "edit_file", result: "ok", round: 1},
			},
			wantPassed: true,
		},
		{
			name:     "subsequence match - extra tools ignored",
			sequence: []string{"read_file", "edit_file"},
			toolCalls: []testToolCall{
				{id: "c1", name: "search", result: "found", round: 0},
				{id: "c2", name: "read_file", result: "content", round: 1},
				{id: "c3", name: "think", result: "thought", round: 1},
				{id: "c4", name: "edit_file", result: "ok", round: 2},
			},
			wantPassed: true,
		},
		{
			name:     "wrong order fails",
			sequence: []string{"read_file", "edit_file"},
			toolCalls: []testToolCall{
				{id: "c1", name: "edit_file", result: "ok", round: 0},
				{id: "c2", name: "read_file", result: "content", round: 1},
			},
			wantPassed: false,
		},
		{
			name:     "missing tool in sequence",
			sequence: []string{"read_file", "edit_file", "commit"},
			toolCalls: []testToolCall{
				{id: "c1", name: "read_file", result: "content", round: 0},
				{id: "c2", name: "edit_file", result: "ok", round: 1},
			},
			wantPassed: false,
		},
		{
			name:       "empty sequence always passes",
			sequence:   []string{},
			toolCalls:  []testToolCall{{id: "c1", name: "a", result: "ok", round: 0}},
			wantPassed: true,
		},
		{
			name:     "no tool calls with non-empty sequence",
			sequence: []string{"read_file"},
			toolCalls: []testToolCall{},
			wantPassed: false,
		},
		{
			name:       "duplex path skipped",
			sequence:   []string{"read_file"},
			wantPassed: true,
		},
		{
			name:     "repeated tool in sequence",
			sequence: []string{"read_file", "read_file"},
			toolCalls: []testToolCall{
				{id: "c1", name: "read_file", result: "a", round: 0},
				{id: "c2", name: "read_file", result: "b", round: 0},
			},
			wantPassed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]interface{}{
				"sequence": tt.sequence,
			}
			validator := NewToolCallSequenceValidator(params)

			var enhancedParams map[string]interface{}
			if tt.name == "duplex path skipped" {
				enhancedParams = map[string]interface{}{}
			} else {
				enhancedParams = map[string]interface{}{
					"_turn_messages": buildTurnMessages(tt.toolCalls...),
				}
			}

			result := validator.Validate("", enhancedParams)
			if result.Passed != tt.wantPassed {
				t.Errorf("Validate() Passed = %v, want %v, details = %v",
					result.Passed, tt.wantPassed, result.Details)
			}
		})
	}
}
