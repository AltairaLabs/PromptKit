package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/selfplay"
)

// openTextSynthesisStream returns the TTS stream for pre-known text using the
// arena voice catalog (capability=tts provider yaml). It looks up the TTS
// service from the registry's TTSRegistry using the loaded provider config and
// calls SynthesizeTextStream on an AudioContentGenerator backed by that service.
//
// Single seam: any time the arena needs TTS audio for a scripted-text turn —
// where the text came from the scenario YAML rather than a persona LLM — it
// goes through this function. No direct access to ttsRegistry.
func (de *DuplexConversationExecutor) openTextSynthesisStream(
	ctx context.Context,
	text string,
	ttsProvider *config.Provider,
) (*selfplay.AudioStreamResult, error) {
	ttsService, err := de.selfPlayRegistry.GetTTSRegistry().GetForProvider(ttsProvider)
	if err != nil {
		return nil, fmt.Errorf("get TTS service for provider %s: %w", ttsProvider.ID, err)
	}
	gen := selfplay.NewAudioContentGenerator(nil, ttsService, ttsProvider)
	return gen.SynthesizeTextStream(ctx, text)
}

// turnAudioMirror is a temp file written incrementally as TTS chunks
// flow through the executor. The streamed audio never lives in RAM as
// a complete buffer — each chunk is written through to disk as soon as
// it's been forwarded to the pipeline.
//
// Lifecycle:
//   - newTurnAudioMirror creates a temp file under the OS temp dir
//   - write appends the next chunk
//   - finalize closes the file and returns its path so the user Message
//     can reference it via MediaContent.FilePath
//   - cleanup removes the temp file; safe to call from a defer regardless
//     of whether finalize ran (on success the file has already been
//     handed off to media storage and re-removing is fine)
type turnAudioMirror struct {
	file      *os.File
	path      string
	finalized bool
}

func newTurnAudioMirror(turnID string) (*turnAudioMirror, error) {
	pattern := fmt.Sprintf("promptarena-tts-%s-*.pcm", turnID)
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return nil, fmt.Errorf("create temp audio file: %w", err)
	}
	return &turnAudioMirror{file: f, path: f.Name()}, nil
}

// write appends a TTS chunk to the mirror file. Best-effort: a write
// error is reported but the caller is expected to abort the turn.
func (m *turnAudioMirror) write(chunk []byte) error {
	if m == nil || m.file == nil {
		return nil
	}
	_, err := m.file.Write(chunk)
	return err
}

// finalize closes the file and returns its absolute path. The path is
// what the caller embeds in the user Message's MediaContent.FilePath.
// Subsequent cleanup() calls are no-ops on the file but still try to
// remove the path; that's safe even after media storage has copied the
// data away.
func (m *turnAudioMirror) finalize() (string, error) {
	if m == nil || m.file == nil {
		return "", nil
	}
	if err := m.file.Sync(); err != nil {
		return "", fmt.Errorf("sync audio mirror: %w", err)
	}
	if err := m.file.Close(); err != nil {
		return "", fmt.Errorf("close audio mirror: %w", err)
	}
	m.file = nil
	m.finalized = true
	abs, err := filepath.Abs(m.path)
	if err != nil {
		return m.path, nil
	}
	return abs, nil
}

// cleanup removes the temp file. Idempotent; ignores ENOENT.
func (m *turnAudioMirror) cleanup() {
	if m == nil {
		return
	}
	if m.file != nil {
		_ = m.file.Close()
		m.file = nil
	}
	if m.path != "" {
		_ = os.Remove(m.path)
		m.path = ""
	}
}
