package engine

import (
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/artifacts"
	arenaaudio "github.com/AltairaLabs/PromptKit/tools/arena/audio"
)

// TestEngine_SimpleAccessors covers option setters and simple accessors on
// Engine that have no dedicated test home.
func TestEngine_SimpleAccessors(t *testing.T) {
	config.SchemaValidationDisabled.Store(true)

	cfg := filepath.Join("testdata", "interactive", "config.arena.yaml")
	eng, err := NewEngineFromConfigFile(filepath.Clean(cfg))
	if err != nil {
		t.Fatalf("NewEngineFromConfigFile: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// WithOutputDir — simple setter.
	eng.WithOutputDir("/tmp/test-output")

	// WithArtifactStore — simple setter (nil is valid to test the path).
	eng.WithArtifactStore(artifacts.Store(nil))

	// AudioMonitorEnabled + AudioMonitorOptions — both false/zero before enable.
	if eng.AudioMonitorEnabled() {
		t.Error("AudioMonitorEnabled should be false before EnableAudioMonitor")
	}
	opts := eng.AudioMonitorOptions()
	if opts != (arenaaudio.Options{}) {
		t.Errorf("AudioMonitorOptions should be zero before EnableAudioMonitor, got %v", opts)
	}
}
