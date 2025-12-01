package mocks

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

// File represents a mock response YAML document compatible with the mock provider.
// It mirrors the structure used by mock repository config files.
type File struct {
	DefaultResponse string                         `yaml:"defaultResponse,omitempty"`
	Scenarios       map[string]ScenarioTurnHistory `yaml:"scenarios,omitempty"`
}

// ScenarioTurnHistory contains the generated turns for a scenario.
type ScenarioTurnHistory struct {
	// DefaultResponse can be used to set a scenario-specific fallback.
	DefaultResponse string               `yaml:"defaultResponse,omitempty"`
	Turns           map[int]TurnTemplate `yaml:"turns,omitempty"`
}

// TurnTemplate captures either tool calls or an assistant response for a single turn.
type TurnTemplate struct {
	Response  string             `yaml:"response,omitempty"`
	ToolCalls []mock.ToolCall    `yaml:"tool_calls,omitempty"`
	Parts     []mock.ContentPart `yaml:"parts,omitempty"`
}

// BuildScenarioFromResult converts a single Arena RunResult into a ScenarioTurnHistory.
// It extracts assistant messages in order, preserving tool calls and responses.
func BuildScenarioFromResult(result engine.RunResult) (ScenarioTurnHistory, error) { //nolint:gocognit,gocritic
	turns := make(map[int]TurnTemplate)

	turnNumber := 1
	for i := range result.Messages {
		msg := result.Messages[i]
		if msg.Role != "assistant" {
			continue
		}

		turn := TurnTemplate{}

		if len(msg.ToolCalls) > 0 {
			toolCalls, err := convertToolCalls(msg.ToolCalls)
			if err != nil {
				return ScenarioTurnHistory{}, fmt.Errorf("turn %d: %w", turnNumber, err)
			}
			turn.ToolCalls = toolCalls
		}

		content := msg.GetContent()
		if content != "" {
			turn.Response = content
		}

		if len(msg.Parts) > 0 {
			parts, err := convertContentParts(msg.Parts)
			if err != nil {
				return ScenarioTurnHistory{}, fmt.Errorf("turn %d: %w", turnNumber, err)
			}
			turn.Parts = parts
		}

		// Skip empty assistant turns (no tool calls, no response, no parts)
		if turn.Response == "" && len(turn.ToolCalls) == 0 && len(turn.Parts) == 0 {
			continue
		}

		turns[turnNumber] = turn
		turnNumber++
	}

	return ScenarioTurnHistory{
		Turns: turns,
	}, nil
}

// BuildFile merges multiple RunResults into a mock config File grouped by ScenarioID.
func BuildFile(results []engine.RunResult) (File, error) {
	file := File{
		Scenarios: make(map[string]ScenarioTurnHistory),
	}

	for i := range results {
		res := results[i]
		if res.ScenarioID == "" {
			return File{}, fmt.Errorf("run %s has empty ScenarioID", res.RunID)
		}
		history, err := BuildScenarioFromResult(res)
		if err != nil {
			return File{}, fmt.Errorf("scenario %s: %w", res.ScenarioID, err)
		}
		file.Scenarios[res.ScenarioID] = history
	}

	// Stabilize turn ordering for deterministic marshaling (maps are unordered).
	for key, hist := range file.Scenarios {
		file.Scenarios[key] = sortTurns(hist)
	}

	return file, nil
}

func convertToolCalls(calls []types.MessageToolCall) ([]mock.ToolCall, error) {
	out := make([]mock.ToolCall, 0, len(calls))
	for _, tc := range calls {
		var args map[string]interface{}
		if len(tc.Args) > 0 {
			// Try to decode args as JSON; if it fails, fall back to string.
			if err := json.Unmarshal(tc.Args, &args); err != nil {
				var asString string
				if err2 := json.Unmarshal(tc.Args, &asString); err2 == nil {
					args = map[string]interface{}{"_raw": asString}
				} else {
					return nil, fmt.Errorf("tool call %s: failed to decode args: %w", tc.Name, err)
				}
			}
		}

		out = append(out, mock.ToolCall{
			Name:      tc.Name,
			Arguments: args,
		})
	}
	return out, nil
}

func convertContentParts(parts []types.ContentPart) ([]mock.ContentPart, error) { //nolint:gocognit
	out := make([]mock.ContentPart, 0, len(parts))
	for _, p := range parts {
		switch p.Type {
		case types.ContentTypeText:
			if p.Text == nil {
				continue
			}
			out = append(out, mock.ContentPart{
				Type: types.ContentTypeText,
				Text: *p.Text,
			})
		case types.ContentTypeImage:
			if p.Media == nil || p.Media.URL == nil {
				continue
			}
			out = append(out, mock.ContentPart{
				Type: types.ContentTypeImage,
				ImageURL: &mock.ImageURL{
					URL:    *p.Media.URL,
					Detail: p.Media.Detail,
				},
			})
		case types.ContentTypeAudio:
			if p.Media == nil || p.Media.URL == nil {
				continue
			}
			out = append(out, mock.ContentPart{
				Type: types.ContentTypeAudio,
				AudioURL: &mock.AudioURL{
					URL: *p.Media.URL,
				},
			})
		case types.ContentTypeVideo:
			if p.Media == nil || p.Media.URL == nil {
				continue
			}
			out = append(out, mock.ContentPart{
				Type: types.ContentTypeVideo,
				VideoURL: &mock.VideoURL{
					URL: *p.Media.URL,
				},
			})
		default:
			return nil, fmt.Errorf("unsupported content part type: %s", p.Type)
		}
	}
	return out, nil
}

func sortTurns(history ScenarioTurnHistory) ScenarioTurnHistory {
	if len(history.Turns) == 0 {
		return history
	}
	keys := make([]int, 0, len(history.Turns))
	for k := range history.Turns {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	sorted := make(map[int]TurnTemplate, len(history.Turns))
	for _, k := range keys {
		sorted[k] = history.Turns[k]
	}
	history.Turns = sorted
	return history
}
