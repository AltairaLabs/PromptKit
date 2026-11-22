package voice_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/voice"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// recordingSTT answers every transcription with text and cost, and records
// each request.
func recordingSTT(text string, cost *types.CostInfo, reqs *[]base.STTRequest) *mockSTTService {
	return &mockSTTService{transcribeFunc: func(_ context.Context, req base.STTRequest) (base.STTResponse, error) {
		*reqs = append(*reqs, req)
		return base.STTResponse{Text: text, Cost: cost}, nil
	}}
}

func TestSTTStage_RequestCarriesTheAudiosFormatAsHints(t *testing.T) {
	var reqs []base.STTRequest
	config := voice.DefaultSTTStageConfig()
	config.Language = "fr"
	s := voice.NewSTTStage(recordingSTT("bonjour", nil, &reqs), config)

	audio := generateTestPCMAudio(3200)
	elem := makeAudioElement(audio, 24000)
	elem.Audio.Channels = 2
	runStage(t, s, []core.StreamElement{elem}, 2*time.Second)

	require.Len(t, reqs, 1)
	assert.Equal(t, map[string]string{core.MetaKeySampleRate: "24000", "channels": "2", "language": "fr"},
		reqs[0].Hints)
	assert.Equal(t, "audio/pcm", reqs[0].MIMEType)
	assert.Equal(t, audio, reqs[0].Audio)
}

func TestSTTStage_TranscriptCarriesTheTranscriptionsCost(t *testing.T) {
	cost := &types.CostInfo{InputCostUSD: 0.25, TotalCost: 0.25}
	var reqs []base.STTRequest
	s := voice.NewSTTStage(recordingSTT("hello", cost, &reqs), voice.DefaultSTTStageConfig())

	out := runStage(t, s, []core.StreamElement{makeAudioElement(generateTestPCMAudio(3200), 16000)}, 2*time.Second)
	require.Len(t, out, 1)
	require.NotNil(t, out[0].Message)
	assert.Equal(t, map[string]interface{}{"stt_cost": base.CostInfoToMetaMap(cost)}, out[0].Message.Meta)
	assert.Equal(t, "hello", out[0].Message.GetContent())
	assert.Equal(t, core.RoleUser, out[0].Message.Role)
}

func TestSTTStage_AnUnpricedTranscriptCarriesNoCost(t *testing.T) {
	var reqs []base.STTRequest
	s := voice.NewSTTStage(recordingSTT("hello", nil, &reqs), voice.DefaultSTTStageConfig())

	out := runStage(t, s, []core.StreamElement{makeAudioElement(generateTestPCMAudio(3200), 16000)}, 2*time.Second)
	require.Len(t, out, 1)
	require.NotNil(t, out[0].Message)
	assert.Nil(t, out[0].Message.Meta)
}

// Audio of exactly MinAudioBytes is transcribed; one byte less is skipped.
func TestSTTStage_MinAudioBytesIsTheSmallestTranscribed(t *testing.T) {
	config := voice.DefaultSTTStageConfig()
	config.MinAudioBytes = 1000

	var reqs []base.STTRequest
	s := voice.NewSTTStage(recordingSTT("hi", nil, &reqs), config)
	out := runStage(t, s, []core.StreamElement{makeAudioElement(generateTestPCMAudio(1000), 16000)}, 2*time.Second)
	assert.Len(t, reqs, 1)
	assert.Len(t, out, 1)

	reqs = nil
	s = voice.NewSTTStage(recordingSTT("hi", nil, &reqs), config)
	out = runStage(t, s, []core.StreamElement{makeAudioElement(generateTestPCMAudio(999), 16000)}, 2*time.Second)
	assert.Empty(t, reqs)
	assert.Empty(t, out)
}
