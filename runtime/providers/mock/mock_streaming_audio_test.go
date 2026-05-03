package mock

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// scenarioRepo is a tiny in-memory ResponseRepository that returns Turn
// values keyed by (scenarioID, turnNumber). It exists only for these tests
// so we don't need to touch InMemoryMockRepository (which is text-only).
type scenarioRepo struct {
	turns map[string]map[int]*Turn
}

func newScenarioRepo() *scenarioRepo {
	return &scenarioRepo{turns: make(map[string]map[int]*Turn)}
}

func (r *scenarioRepo) set(scenarioID string, turnNumber int, turn *Turn) {
	if r.turns[scenarioID] == nil {
		r.turns[scenarioID] = make(map[int]*Turn)
	}
	r.turns[scenarioID][turnNumber] = turn
}

func (r *scenarioRepo) GetResponse(_ context.Context, params ResponseParams) (string, error) {
	turn, err := r.GetTurn(context.Background(), params)
	if err != nil || turn == nil {
		return "", err
	}
	return turn.Content, nil
}

func (r *scenarioRepo) GetTurn(_ context.Context, params ResponseParams) (*Turn, error) {
	if scenario, ok := r.turns[params.ScenarioID]; ok {
		if turn, ok := scenario[params.TurnNumber]; ok {
			return turn, nil
		}
	}
	return &Turn{Type: turnTypeText}, nil
}

// writeSyntheticPCM writes a small block of synthetic 16-bit mono PCM bytes
// to the given path and returns the full byte slice. The bytes themselves
// are arbitrary — these tests verify framing and plumbing, not audio content.
func writeSyntheticPCM(t *testing.T, path string, sampleRate, durationMs int) []byte {
	t.Helper()
	totalSamples := sampleRate * durationMs / 1000
	data := make([]byte, totalSamples*2)
	for i := range data {
		data[i] = byte(i % 251) // deterministic pattern
	}
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return data
}

// drainResponses collects every chunk emitted on the session's response
// channel until either the channel closes, the deadline fires, or no chunk
// arrives within idleTimeout (whichever comes first).
func drainResponses(t *testing.T, session *MockStreamSession, idleTimeout time.Duration) []providers.StreamChunk {
	t.Helper()
	var chunks []providers.StreamChunk
	deadline := time.After(2 * time.Second)
	for {
		select {
		case chunk, ok := <-session.Response():
			if !ok {
				return chunks
			}
			chunks = append(chunks, chunk)
		case <-time.After(idleTimeout):
			return chunks
		case <-deadline:
			return chunks
		}
	}
}

func TestMockStreamSession_EmitsAudioChunksFromFixture(t *testing.T) {
	dir := t.TempDir()
	fixturePath := filepath.Join(dir, "greeting.pcm")
	const sampleRate = 24000
	const durationMs = 100 // 4800 samples * 2 bytes = 9600 bytes
	pcmBytes := writeSyntheticPCM(t, fixturePath, sampleRate, durationMs)

	repo := newScenarioRepo()
	repo.set("duplex-basic", 1, &Turn{
		Type:            turnTypeText,
		Content:         "Hello there",
		AudioFile:       "greeting.pcm",
		AudioSampleRate: sampleRate,
		AudioMIMEType:   "audio/pcm",
	})

	session := NewMockStreamSession().
		WithAutoRespond("fallback").
		WithRepository(repo, dir).
		WithScenarioID("duplex-basic")

	require.NoError(t, session.SendText(context.Background(), "user said hi"))
	chunks := drainResponses(t, session, 50*time.Millisecond)

	var mediaChunks []providers.StreamChunk
	var textChunk *providers.StreamChunk
	for i := range chunks {
		switch {
		case chunks[i].MediaData != nil:
			mediaChunks = append(mediaChunks, chunks[i])
		case chunks[i].FinishReason != nil:
			textChunk = &chunks[i]
		}
	}

	require.NotEmpty(t, mediaChunks, "expected MediaData chunks to be emitted")
	require.NotNil(t, textChunk, "expected a final text/finish chunk")

	totalAudio := 0
	for _, c := range mediaChunks {
		totalAudio += len(c.MediaData.Data)
		assert.Equal(t, "audio/pcm", c.MediaData.MIMEType)
		assert.Equal(t, sampleRate, c.MediaData.SampleRate)
		assert.Equal(t, 1, c.MediaData.Channels)
	}
	assert.Equal(t, len(pcmBytes), totalAudio, "total audio bytes must equal fixture size")

	assert.Equal(t, "Hello there", textChunk.Content)
	require.NotNil(t, textChunk.FinishReason)
	assert.Equal(t, "stop", *textChunk.FinishReason)
}

func TestMockStreamSession_TextOnlyTurnsStillWork(t *testing.T) {
	repo := newScenarioRepo()
	repo.set("text-only", 1, &Turn{
		Type:    turnTypeText,
		Content: "no audio here",
	})

	session := NewMockStreamSession().
		WithAutoRespond("fallback").
		WithRepository(repo, t.TempDir()).
		WithScenarioID("text-only")

	require.NoError(t, session.SendText(context.Background(), "ping"))
	chunks := drainResponses(t, session, 50*time.Millisecond)

	for _, c := range chunks {
		assert.Nil(t, c.MediaData, "text-only turns must not emit MediaData chunks")
	}
	require.Len(t, chunks, 1, "expected a single text+finish chunk for text-only turn")
	assert.Equal(t, "no audio here", chunks[0].Content)
	require.NotNil(t, chunks[0].FinishReason)
	assert.Equal(t, "stop", *chunks[0].FinishReason)
}

func TestMockStreamSession_AudioFixtureCached(t *testing.T) {
	dir := t.TempDir()
	fixturePath := filepath.Join(dir, "shared.pcm")
	const sampleRate = 16000
	writeSyntheticPCM(t, fixturePath, sampleRate, 40)

	repo := newScenarioRepo()
	for turn := 1; turn <= 3; turn++ {
		repo.set("loop", turn, &Turn{
			Type:            turnTypeText,
			Content:         "again",
			AudioFile:       "shared.pcm",
			AudioSampleRate: sampleRate,
		})
	}

	session := NewMockStreamSession().
		WithAutoRespond("fallback").
		WithRepository(repo, dir).
		WithScenarioID("loop")

	for turn := 0; turn < 3; turn++ {
		require.NoError(t, session.SendText(context.Background(), "drive turn"))
		// Drain — we don't care about content here, just that loading works.
		drainResponses(t, session, 30*time.Millisecond)
	}

	// One file path => one cache entry, regardless of how many turns hit it.
	require.NotNil(t, session.audioCache)
	require.Len(t, session.audioCache, 1, "audio fixture should be loaded only once across turns")

	// Replace the file on disk; the session must still serve the cached bytes.
	require.NoError(t, os.WriteFile(fixturePath, []byte("not-a-pcm-anymore"), 0o600))
	cached := session.audioCache[filepath.Join(dir, "shared.pcm")]
	require.NotNil(t, cached)
	assert.NotEqual(t, []byte("not-a-pcm-anymore"), cached.Bytes,
		"cached bytes must not reflect on-disk changes")
}

func TestMockStreamSession_AudioFixtureMissingFile(t *testing.T) {
	repo := newScenarioRepo()
	repo.set("broken", 1, &Turn{
		Type:      turnTypeText,
		Content:   "still respond",
		AudioFile: "does-not-exist.pcm",
	})

	dir := t.TempDir()
	session := NewMockStreamSession().
		WithAutoRespond("fallback").
		WithRepository(repo, dir).
		WithScenarioID("broken")

	// Must not panic and must still emit the text response.
	require.NoError(t, session.SendText(context.Background(), "go"))
	chunks := drainResponses(t, session, 50*time.Millisecond)

	for _, c := range chunks {
		assert.Nil(t, c.MediaData, "missing audio fixture must not produce MediaData chunks")
	}
	require.Len(t, chunks, 1)
	assert.Equal(t, "still respond", chunks[0].Content)
	require.NotNil(t, chunks[0].FinishReason)
}

func TestMockStreamSession_AudioFixtureChunkSize(t *testing.T) {
	cases := []struct {
		name       string
		sampleRate int
		durationMs int
	}{
		{"24kHz exact", 24000, 100}, // 5 frames * 960 bytes = 4800 bytes total — wait: 24000*0.02*2 = 960; 100ms => 5 frames
		{"16kHz with tail", 16000, 25},
		{"48kHz exact", 48000, 60},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			fixturePath := filepath.Join(dir, "f.pcm")
			pcmBytes := writeSyntheticPCM(t, fixturePath, tc.sampleRate, tc.durationMs)

			repo := newScenarioRepo()
			repo.set("scn", 1, &Turn{
				Type:            turnTypeText,
				Content:         "ok",
				AudioFile:       "f.pcm",
				AudioSampleRate: tc.sampleRate,
			})

			session := NewMockStreamSession().
				WithAutoRespond("fallback").
				WithRepository(repo, dir).
				WithScenarioID("scn")

			require.NoError(t, session.SendText(context.Background(), "x"))
			chunks := drainResponses(t, session, 50*time.Millisecond)

			expectedFrameBytes := tc.sampleRate * audioFrameMillis / 1000 * bytesPerPCMSample
			var mediaChunks []providers.StreamChunk
			for i := range chunks {
				if chunks[i].MediaData != nil {
					mediaChunks = append(mediaChunks, chunks[i])
				}
			}
			require.NotEmpty(t, mediaChunks)

			// All but the last frame must be exactly expectedFrameBytes.
			for i, c := range mediaChunks[:len(mediaChunks)-1] {
				assert.Equal(t, expectedFrameBytes, len(c.MediaData.Data),
					"frame %d should be a full %dms frame", i, audioFrameMillis)
			}
			tail := mediaChunks[len(mediaChunks)-1]
			assert.LessOrEqual(t, len(tail.MediaData.Data), expectedFrameBytes,
				"final frame may be smaller than full")
			assert.Greater(t, len(tail.MediaData.Data), 0)

			total := 0
			for _, c := range mediaChunks {
				total += len(c.MediaData.Data)
			}
			assert.Equal(t, len(pcmBytes), total,
				"sum of frame sizes must equal fixture size")
		})
	}
}

func TestMockStreamSession_AudioFixtureWithToolCalls(t *testing.T) {
	dir := t.TempDir()
	fixturePath := filepath.Join(dir, "tool-response.pcm")
	const sampleRate = 24000
	writeSyntheticPCM(t, fixturePath, sampleRate, 60)

	// Tool-call turns reach the streaming path through PredictWithTools, not
	// through emitAutoResponse. Here we just verify the Turn struct cleanly
	// carries audio metadata alongside ToolCalls so the YAML form remains
	// composable. The streaming session itself only uses Content+AudioFile.
	repo := newScenarioRepo()
	repo.set("tools", 1, &Turn{
		Type:    "tool_calls",
		Content: "Looking that up",
		ToolCalls: []ToolCall{
			{Name: "get_weather", Arguments: map[string]interface{}{"location": "Seattle"}},
		},
		AudioFile:       "tool-response.pcm",
		AudioSampleRate: sampleRate,
	})

	session := NewMockStreamSession().
		WithAutoRespond("fallback").
		WithRepository(repo, dir).
		WithScenarioID("tools")

	require.NoError(t, session.SendText(context.Background(), "weather please"))
	chunks := drainResponses(t, session, 50*time.Millisecond)

	var media, text int
	for _, c := range chunks {
		if c.MediaData != nil {
			media++
		}
		if c.FinishReason != nil {
			text++
		}
	}
	assert.Greater(t, media, 0)
	assert.Equal(t, 1, text)
}

func TestStreamingProvider_WithMockResponses(t *testing.T) {
	dir := t.TempDir()
	fixturePath := filepath.Join(dir, "p.pcm")
	const sampleRate = 24000
	writeSyntheticPCM(t, fixturePath, sampleRate, 40)

	repo := newScenarioRepo()
	repo.set("from-provider", 1, &Turn{
		Type:            turnTypeText,
		Content:         "via provider",
		AudioFile:       "p.pcm",
		AudioSampleRate: sampleRate,
	})

	provider := NewStreamingProvider("test", "test-model", false).
		WithAutoRespond("fallback").
		WithMockResponses(repo, "from-provider", dir)

	session, err := provider.CreateStreamSession(context.Background(), &providers.StreamingInputConfig{})
	require.NoError(t, err)
	mockSession, ok := session.(*MockStreamSession)
	require.True(t, ok)

	require.NoError(t, mockSession.SendText(context.Background(), "go"))
	chunks := drainResponses(t, mockSession, 50*time.Millisecond)

	var media int
	var foundText bool
	for _, c := range chunks {
		if c.MediaData != nil {
			media++
		}
		if c.FinishReason != nil {
			foundText = true
			assert.Equal(t, "via provider", c.Content)
		}
	}
	assert.Greater(t, media, 0, "provider-driven session should emit audio chunks")
	assert.True(t, foundText)
}

func TestStreamingProvider_MetadataScenarioOverride(t *testing.T) {
	repo := newScenarioRepo()
	repo.set("default", 1, &Turn{Type: turnTypeText, Content: "default-text"})
	repo.set("override", 1, &Turn{Type: turnTypeText, Content: "override-text"})

	provider := NewStreamingProvider("p", "m", false).
		WithAutoRespond("fallback").
		WithMockResponses(repo, "default", "")

	cfg := &providers.StreamingInputConfig{
		Metadata: map[string]interface{}{"mock_scenario_id": "override"},
	}
	session, err := provider.CreateStreamSession(context.Background(), cfg)
	require.NoError(t, err)
	mockSession := session.(*MockStreamSession)

	require.NoError(t, mockSession.SendText(context.Background(), "x"))
	chunks := drainResponses(t, mockSession, 50*time.Millisecond)

	require.NotEmpty(t, chunks)
	assert.Equal(t, "override-text", chunks[len(chunks)-1].Content)
}

func TestFileMockRepository_AudioFixtureFields(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mock-responses.yaml")
	yamlBody := []byte(`
defaultResponse: "default"
scenarios:
  duplex-basic:
    turns:
      1:
        text: "Hi from Nova"
        audio_file: audio/turn1.pcm
        audio_sample_rate: 16000
        audio_mime_type: "audio/pcm"
      2: "plain string still ok"
`)
	require.NoError(t, os.WriteFile(cfgPath, yamlBody, 0o600))

	repo, err := NewFileMockRepository(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, dir, repo.BaseDir())

	turn1, err := repo.GetTurn(context.Background(), ResponseParams{ScenarioID: "duplex-basic", TurnNumber: 1})
	require.NoError(t, err)
	require.NotNil(t, turn1)
	assert.Equal(t, "Hi from Nova", turn1.Content)
	assert.Equal(t, "audio/turn1.pcm", turn1.AudioFile)
	assert.Equal(t, 16000, turn1.AudioSampleRate)
	assert.Equal(t, "audio/pcm", turn1.AudioMIMEType)

	turn2, err := repo.GetTurn(context.Background(), ResponseParams{ScenarioID: "duplex-basic", TurnNumber: 2})
	require.NoError(t, err)
	require.NotNil(t, turn2)
	assert.Equal(t, "plain string still ok", turn2.Content)
	assert.Empty(t, turn2.AudioFile, "bare-string turns must not pick up audio fields")
}
