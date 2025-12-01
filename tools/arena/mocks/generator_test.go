package mocks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestWriteFiles_DryRunCombined(t *testing.T) {
	hw := loadRunResult(t, filepath.Join("..", "templates", "testdata", "2025-11-30T19-49Z_openai-gpt4o_default_hardware-faults_18c25790.json"))
	rt := loadRunResult(t, filepath.Join("..", "templates", "testdata", "2025-11-30T19-49Z_openai-gpt4o_default_redteam-selfplay_83be345a.json"))

	file, err := BuildFile([]engine.RunResult{hw, rt})
	require.NoError(t, err)

	outputs, err := WriteFiles(file, WriteOptions{
		OutputPath: "mock.yaml",
		DryRun:     true,
	})
	require.NoError(t, err)
	require.Len(t, outputs, 1)

	content := string(outputs["mock.yaml"])
	assert.Contains(t, content, "hardware-faults:")
	assert.Contains(t, content, "redteam-selfplay:")
}

func TestWriteFiles_MergePerScenario(t *testing.T) {
	tmp := t.TempDir()

	// Existing file with default response and a single turn
	existing := File{
		DefaultResponse: "base default",
		Scenarios: map[string]ScenarioTurnHistory{
			"hardware-faults": {
				Turns: map[int]TurnTemplate{
					1: {Response: "old response"},
				},
			},
		},
	}
	existingYAML, err := renderYAML(existing)
	require.NoError(t, err)

	existingPath := filepath.Join(tmp, "hardware-faults.yaml")
	require.NoError(t, os.WriteFile(existingPath, existingYAML, 0o644))

	// Incoming file with updated turn 1 and a new turn 2
	incoming := File{
		Scenarios: map[string]ScenarioTurnHistory{
			"hardware-faults": {
				Turns: map[int]TurnTemplate{
					1: {Response: "new response"},
					2: {Response: "extra turn"},
				},
			},
		},
	}

	_, err = WriteFiles(incoming, WriteOptions{
		OutputPath:  tmp,
		PerScenario: true,
		Merge:       true,
	})
	require.NoError(t, err)

	// Verify merged file
	raw, err := os.ReadFile(existingPath)
	require.NoError(t, err)

	var merged File
	require.NoError(t, yaml.Unmarshal(raw, &merged))

	assert.Equal(t, "base default", merged.DefaultResponse, "default response should be preserved from base when incoming empty")

	hist, ok := merged.Scenarios["hardware-faults"]
	require.True(t, ok)
	require.Len(t, hist.Turns, 2)
	assert.Equal(t, "new response", hist.Turns[1].Response)
	assert.Equal(t, "extra turn", hist.Turns[2].Response)
}

func TestWriteFiles_DefaultResponseAndDryRunPerScenario(t *testing.T) {
	file := File{
		Scenarios: map[string]ScenarioTurnHistory{
			"demo": {
				Turns: map[int]TurnTemplate{
					1: {Response: "hi"},
				},
			},
		},
	}

	outputs, err := WriteFiles(file, WriteOptions{
		OutputPath:      "outdir",
		PerScenario:     true,
		DefaultResponse: "fallback",
		DryRun:          true,
	})
	require.NoError(t, err)
	require.Len(t, outputs, 1)

	content := outputs["outdir/demo.yaml"]
	assert.Contains(t, string(content), "defaultResponse: fallback")
	assert.Contains(t, string(content), "demo:")
}
