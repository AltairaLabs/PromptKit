package voice_test

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/voice"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// spokenTexts is a TTSService that records the text of each synthesis.
type spokenTexts struct {
	mu    sync.Mutex
	texts []string
}

func (s *spokenTexts) Synthesize(_ context.Context, text string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.texts = append(s.texts, text)
	return []byte("pcm"), nil
}
func (*spokenTexts) MIMEType() string { return "audio/pcm" }

func (s *spokenTexts) got() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.texts...)
}

// interruptibleSpoken is a TTSStageWithInterruption whose service records the
// text of each synthesis.
func interruptibleSpoken(t *testing.T, config voice.TTSStageWithInterruptionConfig, in ...core.StreamElement) []string {
	t.Helper()
	var mu sync.Mutex
	var spoken []string
	svc := &mockTTSService{synthesizeFunc: func(_ context.Context, text string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
		mu.Lock()
		spoken = append(spoken, text)
		mu.Unlock()
		return io.NopCloser(strings.NewReader(generateTestPCM(10))), nil
	}}
	config.NoPlaybackPacing = true
	runStage(t, voice.NewTTSStageWithInterruption(svc, config), in, 2*time.Second)
	mu.Lock()
	defer mu.Unlock()
	return spoken
}

// partsMessage is an assistant Message with no Content whose first text part
// is empty and second is text.
func partsMessage(text string) *types.Message {
	empty := ""
	return &types.Message{Role: "assistant", Parts: []types.ContentPart{
		{Type: types.ContentTypeText, Text: &empty},
		{Type: types.ContentTypeText, Text: &text},
	}}
}

func TestTTSStage_SpeaksTheFirstNonEmptyTextPart(t *testing.T) {
	svc := &spokenTexts{}
	runStage(t, voice.NewTTSStage(svc, voice.DefaultTTSConfig()),
		[]core.StreamElement{core.NewMessageElement(partsMessage("from parts"))}, 2*time.Second)
	assert.Equal(t, []string{"from parts"}, svc.got())
}

func TestTTSStageWithInterruption_SpeaksTheFirstNonEmptyTextPart(t *testing.T) {
	spoken := interruptibleSpoken(t, voice.DefaultTTSStageWithInterruptionConfig(),
		core.NewMessageElement(partsMessage("from parts")))
	assert.Equal(t, []string{"from parts"}, spoken)
}

// Text of exactly MinTextLength is spoken; shorter text is not.
func TestTTSStage_MinTextLengthIsTheShortestSpoken(t *testing.T) {
	svc := &spokenTexts{}
	config := voice.TTSConfig{SkipEmpty: true, MinTextLength: 3}
	runStage(t, voice.NewTTSStage(svc, config),
		[]core.StreamElement{makeTextElement("ab"), makeTextElement("abc")}, 2*time.Second)
	assert.Equal(t, []string{"abc"}, svc.got())
}

func TestTTSStageWithInterruption_MinTextLengthIsTheShortestSpoken(t *testing.T) {
	config := voice.DefaultTTSStageWithInterruptionConfig()
	config.MinTextLength = 3
	spoken := interruptibleSpoken(t, config, makeTextElement(" ab "), makeTextElement(" abc "))
	assert.Equal(t, []string{" abc "}, spoken)
}
