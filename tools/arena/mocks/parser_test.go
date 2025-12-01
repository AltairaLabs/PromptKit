package mocks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

func TestBuildScenarioFromResult_HardwareFaults(t *testing.T) {
	result := loadRunResult(t, filepath.Join("..", "templates", "testdata", "2025-11-30T19-49Z_openai-gpt4o_default_hardware-faults_18c25790.json"))

	history, err := BuildScenarioFromResult(result)
	require.NoError(t, err)

	require.Len(t, history.Turns, 6)

	first := history.Turns[1]
	require.Len(t, first.ToolCalls, 1)
	assert.Equal(t, "list_devices", first.ToolCalls[0].Name)
	assert.Equal(t, "acme-corp", first.ToolCalls[0].Arguments["customer_id"])
	assert.Empty(t, first.Response)

	second := history.Turns[2]
	require.Len(t, second.ToolCalls, 3)
	assert.ElementsMatch(t,
		[]string{"get_sensor_data", "get_error_logs", "check_maintenance_schedule"},
		[]string{
			second.ToolCalls[0].Name,
			second.ToolCalls[1].Name,
			second.ToolCalls[2].Name,
		})

	response := history.Turns[5]
	assert.Contains(t, response.Response, "PUMP-002")
	assert.Contains(t, response.Response, "vibration")
}

func TestBuildScenarioFromResult_RedTeamSelfPlay(t *testing.T) {
	result := loadRunResult(t, filepath.Join("..", "templates", "testdata", "2025-11-30T19-49Z_openai-gpt4o_default_redteam-selfplay_83be345a.json"))

	history, err := BuildScenarioFromResult(result)
	require.NoError(t, err)

	require.Len(t, history.Turns, 4)

	assert.NotEmpty(t, history.Turns[1].Response)

	second := history.Turns[2]
	require.Len(t, second.ToolCalls, 1)
	assert.Equal(t, "get_sensor_data", second.ToolCalls[0].Name)

	third := history.Turns[3]
	require.Len(t, third.ToolCalls, 1)
	assert.Equal(t, "list_devices", third.ToolCalls[0].Name)

	assert.Contains(t, history.Turns[4].Response, "TURBINE-101")
}

func loadRunResult(t *testing.T, path string) engine.RunResult {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err, "failed to read fixture")

	var result engine.RunResult
	err = json.Unmarshal(data, &result)
	require.NoError(t, err, "failed to parse fixture JSON")

	return result
}

func TestBuildScenarioFromResult_SkipsEmptyAssistant(t *testing.T) {
	text := "non-empty"
	run := engine.RunResult{
		Messages: []types.Message{
			{Role: "assistant"}, // empty
			{Role: "assistant", Content: text},
		},
	}

	history, err := BuildScenarioFromResult(run)
	require.NoError(t, err)
	require.Len(t, history.Turns, 1)
	assert.Equal(t, text, history.Turns[1].Response)
}

func TestBuildScenarioFromResult_MultimodalAndRawArgs(t *testing.T) {
	rawArgs := json.RawMessage(`"raw-string-args"`)
	imageURL := "mock://image.png"
	audioURL := "mock://audio.mp3"
	videoURL := "mock://video.mp4"
	text := "hello"

	run := engine.RunResult{
		Messages: []types.Message{
			{Role: "assistant",
				ToolCalls: []types.MessageToolCall{
					{Name: "raw_tool", Args: rawArgs},
				},
				Parts: []types.ContentPart{
					{Type: types.ContentTypeText, Text: &text},
					{Type: types.ContentTypeImage, Media: &types.MediaContent{URL: &imageURL, Detail: nil}},
					{Type: types.ContentTypeAudio, Media: &types.MediaContent{URL: &audioURL}},
					{Type: types.ContentTypeVideo, Media: &types.MediaContent{URL: &videoURL}},
				},
			},
		},
	}

	history, err := BuildScenarioFromResult(run)
	require.NoError(t, err)
	require.Len(t, history.Turns, 1)

	turn := history.Turns[1]
	require.Len(t, turn.ToolCalls, 1)
	assert.Equal(t, "raw_tool", turn.ToolCalls[0].Name)
	assert.Equal(t, "raw-string-args", turn.ToolCalls[0].Arguments["_raw"])

	require.Len(t, turn.Parts, 4)
	assert.Equal(t, types.ContentTypeText, turn.Parts[0].Type)
	assert.Equal(t, text, turn.Parts[0].Text)
	assert.Equal(t, imageURL, turn.Parts[1].ImageURL.URL)
	assert.Equal(t, audioURL, turn.Parts[2].AudioURL.URL)
	assert.Equal(t, videoURL, turn.Parts[3].VideoURL.URL)
}

func TestBuildScenarioFromResult_UnsupportedPartType(t *testing.T) {
	run := engine.RunResult{
		Messages: []types.Message{
			{
				Role: "assistant",
				Parts: []types.ContentPart{
					{Type: "file"},
				},
			},
		},
	}

	_, err := BuildScenarioFromResult(run)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported content part type")
}

func TestBuildFile_ErrorsOnEmptyScenario(t *testing.T) {
	run := engine.RunResult{
		RunID:      "run-1",
		ScenarioID: "",
		ProviderID: "p",
	}

	_, err := BuildFile([]engine.RunResult{run})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty ScenarioID")
}
