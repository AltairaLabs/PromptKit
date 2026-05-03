package engine

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/selfplay"
)

func TestTurnAudioMirror_WriteFinalizeCleanup(t *testing.T) {
	m, err := newTurnAudioMirror("turn-abc")
	require.NoError(t, err)
	require.NotEmpty(t, m.path)

	require.NoError(t, m.write([]byte("chunk1")))
	require.NoError(t, m.write([]byte("chunk2")))

	abs, err := m.finalize()
	require.NoError(t, err)
	assert.True(t, m.finalized)
	assert.NotEmpty(t, abs)

	contents, err := os.ReadFile(abs)
	require.NoError(t, err)
	assert.Equal(t, "chunk1chunk2", string(contents))

	m.cleanup()
	_, err = os.Stat(abs)
	assert.True(t, os.IsNotExist(err), "cleanup should remove the file")

	// idempotent
	m.cleanup()
}

func TestTurnAudioMirror_NilSafe(t *testing.T) {
	var m *turnAudioMirror
	assert.NoError(t, m.write([]byte("ignored")))
	abs, err := m.finalize()
	assert.NoError(t, err)
	assert.Empty(t, abs)
	m.cleanup()
}

func TestTurnAudioMirror_WriteAfterFinalizeIsNoop(t *testing.T) {
	m, err := newTurnAudioMirror("turn-finalized")
	require.NoError(t, err)
	defer m.cleanup()

	_, err = m.finalize()
	require.NoError(t, err)

	// After finalize, m.file is nil — write should silently succeed (no-op).
	assert.NoError(t, m.write([]byte("late")))
}

func TestOpenTextSynthesisStream_RegistryErrorPropagates(t *testing.T) {
	// Empty registry → GetTextSynthesisGenerator returns an error
	// because no providers are registered for "missing".
	reg := selfplay.NewRegistryWithTTS(
		nil,
		map[string]string{},
		map[string]*config.UserPersonaPack{},
		[]config.SelfPlayRoleGroup{},
		selfplay.NewTTSRegistry(),
	)
	de := &DuplexConversationExecutor{selfPlayRegistry: reg}

	_, err := de.openTextSynthesisStream(
		context.Background(),
		"hello",
		&config.TTSConfig{Provider: "missing", Voice: "v1"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get audio generator")
}
