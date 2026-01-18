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

		turn, ok, err := buildTurnFromMessage(&msg)
		if err != nil {
			return ScenarioTurnHistory{}, fmt.Errorf("turn %d: %w", turnNumber, err)
		}
		if !ok {
			continue
		}

		turns[turnNumber] = turn
		turnNumber++
	}

	return ScenarioTurnHistory{
		Turns: turns,
	}, nil
}

func buildTurnFromMessage(msg *types.Message) (TurnTemplate, bool, error) {
	turn := TurnTemplate{}

	if len(msg.ToolCalls) > 0 {
		toolCalls, err := convertToolCalls(msg.ToolCalls)
		if err != nil {
			return TurnTemplate{}, false, err
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
			return TurnTemplate{}, false, err
		}
		turn.Parts = parts
	}

	if turn.Response == "" && len(turn.ToolCalls) == 0 && len(turn.Parts) == 0 {
		return TurnTemplate{}, false, nil
	}

	return turn, true, nil
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

func convertContentParts(parts []types.ContentPart) ([]mock.ContentPart, error) {
	out := make([]mock.ContentPart, 0, len(parts))
	for i := range parts {
		part := parts[i]
		converted, ok, err := convertSinglePart(part)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, converted)
		}
	}
	return out, nil
}

func convertSinglePart(p types.ContentPart) (mock.ContentPart, bool, error) {
	switch p.Type {
	case types.ContentTypeText:
		if p.Text == nil {
			return mock.ContentPart{}, false, nil
		}
		return mock.ContentPart{
			Type: types.ContentTypeText,
			Text: *p.Text,
		}, true, nil
	case types.ContentTypeImage:
		if p.Media == nil || p.Media.URL == nil {
			return mock.ContentPart{}, false, nil
		}
		return mock.ContentPart{
			Type: types.ContentTypeImage,
			ImageURL: &mock.ImageURL{
				URL:    *p.Media.URL,
				Detail: p.Media.Detail,
			},
		}, true, nil
	case types.ContentTypeAudio:
		if p.Media == nil || p.Media.URL == nil {
			return mock.ContentPart{}, false, nil
		}
		return mock.ContentPart{
			Type: types.ContentTypeAudio,
			AudioURL: &mock.AudioURL{
				URL: *p.Media.URL,
			},
		}, true, nil
	case types.ContentTypeVideo:
		if p.Media == nil || p.Media.URL == nil {
			return mock.ContentPart{}, false, nil
		}
		return mock.ContentPart{
			Type: types.ContentTypeVideo,
			VideoURL: &mock.VideoURL{
				URL: *p.Media.URL,
			},
		}, true, nil
	case types.ContentTypeDocument:
		if p.Media == nil || p.Media.URL == nil {
			return mock.ContentPart{}, false, nil
		}
		return mock.ContentPart{
			Type: types.ContentTypeDocument,
			DocumentURL: &mock.DocumentURL{
				URL: *p.Media.URL,
			},
		}, true, nil
	default:
		return mock.ContentPart{}, false, fmt.Errorf("unsupported content part type: %s", p.Type)
	}
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
