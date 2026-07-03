package stage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingSession is a controllable StreamInputSession that records the calls
// made against it. It also implements EndInputter and ToolResponseSupport so the
// stage's optional-interface branches are exercised.
type recordingSession struct {
	providers.BargeInSignal

	mu            sync.Mutex
	chunks        []*types.MediaChunk
	texts         []string
	systemCtx     []string
	toolResponses [][]providers.ToolResponse
	endInputCount int
	closeCount    int

	sendChunkErr  error
	sendTextErr   error
	sendSystemErr error
	sendToolErr   error

	respCh  chan providers.StreamChunk
	respNil bool
	doneCh  chan struct{}
}

func newRecordingSession() *recordingSession {
	return &recordingSession{
		respCh: make(chan providers.StreamChunk, 16),
		doneCh: make(chan struct{}),
	}
}

func (r *recordingSession) SendChunk(_ context.Context, c *types.MediaChunk) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chunks = append(r.chunks, c)
	return r.sendChunkErr
}

func (r *recordingSession) SendText(_ context.Context, t string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.texts = append(r.texts, t)
	return r.sendTextErr
}

func (r *recordingSession) SendSystemContext(_ context.Context, t string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.systemCtx = append(r.systemCtx, t)
	return r.sendSystemErr
}

func (r *recordingSession) SendToolResponse(_ context.Context, _, _ string) error { return nil }

func (r *recordingSession) SendToolResponses(_ context.Context, resp []providers.ToolResponse) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.toolResponses = append(r.toolResponses, resp)
	return r.sendToolErr
}

func (r *recordingSession) EndInput() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.endInputCount++
}

func (r *recordingSession) Response() <-chan providers.StreamChunk {
	if r.respNil {
		return nil
	}
	return r.respCh
}

func (r *recordingSession) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closeCount == 0 {
		close(r.doneCh)
	}
	r.closeCount++
	return nil
}

func (r *recordingSession) Error() error          { return nil }
func (r *recordingSession) Done() <-chan struct{} { return r.doneCh }

// bareSession implements only StreamInputSession — NOT EndInputter or
// ToolResponseSupport — so the stage's "session does not support X" warn
// branches are exercised.
type bareSession struct {
	providers.BargeInSignal
	doneCh chan struct{}
}

func newBareSession() *bareSession { return &bareSession{doneCh: make(chan struct{})} }

func (b *bareSession) SendChunk(_ context.Context, _ *types.MediaChunk) error { return nil }
func (b *bareSession) SendText(_ context.Context, _ string) error             { return nil }
func (b *bareSession) SendSystemContext(_ context.Context, _ string) error    { return nil }
func (b *bareSession) Response() <-chan providers.StreamChunk                 { return nil }
func (b *bareSession) Close() error                                           { return nil }
func (b *bareSession) Error() error                                           { return nil }
func (b *bareSession) Done() <-chan struct{}                                  { return b.doneCh }

// pcm builds deterministic PCM bytes of length n.
func pcm(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 256)
	}
	return b
}

// audioElem wraps PCM samples in a StreamElement.
func duplexAudioElem(samples []byte) StreamElement {
	return StreamElement{Audio: &AudioData{
		Samples:    samples,
		SampleRate: 16000,
		Channels:   1,
		Format:     AudioFormatPCM16,
	}}
}

// stageWithSession builds a stage with the session pre-wired for direct
// unit-testing of the send/response helpers.
func stageWithSession(sess providers.StreamInputSession) *DuplexProviderStage {
	s := NewDuplexProviderStage(providersmock.NewStreamingProvider("t", "m", false), baseConfig())
	s.session = sess
	s.systemPromptSent = true // suppress the system-prompt path unless a test enables it
	return s
}

// =============================================================================
// Constructors / observer
// =============================================================================

func TestNewDuplexProviderStageWithEmitter(t *testing.T) {
	em := &events.Emitter{}
	s := NewDuplexProviderStageWithEmitter(providersmock.NewStreamingProvider("t", "m", false), baseConfig(), em)
	assert.Same(t, em, s.emitter)
	assert.NotNil(t, s.inputDoneCh)
}

func TestNewDuplexProviderStageWithTurnState(t *testing.T) {
	em := &events.Emitter{}
	ts := &TurnState{SystemPrompt: "be helpful"}
	s := NewDuplexProviderStageWithTurnState(providersmock.NewStreamingProvider("t", "m", false), baseConfig(), em, ts)
	assert.Same(t, em, s.emitter)
	assert.Same(t, ts, s.turnState)
}

func TestSetSessionObserver(t *testing.T) {
	s := newDuplexStageForUnit()
	var got providers.StreamInputSession
	s.SetSessionObserver(func(sess providers.StreamInputSession) { got = sess })
	require.NotNil(t, s.onSession)
	sess := newRecordingSession()
	s.onSession(sess)
	assert.Same(t, sess, got)
}

// =============================================================================
// send* helpers via sendElementToSession
// =============================================================================

func TestSendElementToSession_Audio(t *testing.T) {
	sess := newRecordingSession()
	s := stageWithSession(sess)
	// Pre-set transcriptionCaptured so the reset branch runs.
	s.transcriptionCaptured = true

	audio := duplexAudioElem(pcm(64))
	s.sendElementToSession(context.Background(), &audio)

	require.Len(t, sess.chunks, 1)
	assert.Equal(t, pcm(64), sess.chunks[0].Data)
	assert.False(t, s.transcriptionCaptured, "transcription reset on new user turn")
	assert.Equal(t, 1, s.audioChunkCount)
}

func TestSendElementToSession_AudioSendError(t *testing.T) {
	sess := newRecordingSession()
	sess.sendChunkErr = errors.New("send failed")
	s := stageWithSession(sess)

	audio := duplexAudioElem(pcm(32))
	// Must not panic; error is logged internally.
	s.sendElementToSession(context.Background(), &audio)
	require.Len(t, sess.chunks, 1)
}

func TestSendElementToSession_Video(t *testing.T) {
	sess := newRecordingSession()
	s := stageWithSession(sess)

	elem := StreamElement{Video: &VideoData{
		Data:       []byte{1, 2, 3, 4},
		MIMEType:   "video/mp4",
		IsKeyFrame: true,
		Timestamp:  time.Now(),
	}}
	s.sendElementToSession(context.Background(), &elem)

	require.Len(t, sess.chunks, 1)
	assert.Equal(t, "video/mp4", sess.chunks[0].Metadata["mime_type"])
	assert.Equal(t, "true", sess.chunks[0].Metadata["is_key_frame"])
}

func TestSendElementToSession_VideoSendError(t *testing.T) {
	sess := newRecordingSession()
	sess.sendChunkErr = errors.New("video send failed")
	s := stageWithSession(sess)

	elem := StreamElement{Video: &VideoData{Data: []byte{1, 2}, MIMEType: "video/mp4"}}
	s.sendElementToSession(context.Background(), &elem) // must not panic
	require.Len(t, sess.chunks, 1)
}

func TestSendElementToSession_TextSendError(t *testing.T) {
	sess := newRecordingSession()
	sess.sendTextErr = errors.New("text send failed")
	s := stageWithSession(sess)

	txt := "hello"
	elem := StreamElement{Text: &txt}
	s.sendElementToSession(context.Background(), &elem) // must not panic
	require.Len(t, sess.texts, 1)
}

func TestSendElementToSession_ImageDefaultsTimestamp(t *testing.T) {
	sess := newRecordingSession()
	s := stageWithSession(sess)

	elem := StreamElement{Image: &ImageData{
		Data:     []byte{5, 6, 7, 8},
		MIMEType: "image/jpeg",
	}}
	s.sendElementToSession(context.Background(), &elem)

	require.Len(t, sess.chunks, 1)
	assert.False(t, sess.chunks[0].Timestamp.IsZero(), "timestamp defaulted to now")
	assert.Equal(t, "image/jpeg", sess.chunks[0].Metadata["mime_type"])
}

func TestSendElementToSession_Text(t *testing.T) {
	sess := newRecordingSession()
	s := stageWithSession(sess)

	txt := "hello there"
	elem := StreamElement{Text: &txt}
	s.sendElementToSession(context.Background(), &elem)

	require.Len(t, sess.texts, 1)
	assert.Equal(t, "hello there", sess.texts[0])
}

func TestSendElementToSession_EndOfStreamTriggersEndInput(t *testing.T) {
	sess := newRecordingSession()
	s := stageWithSession(sess)

	elem := StreamElement{EndOfStream: true}
	s.sendElementToSession(context.Background(), &elem)

	assert.Equal(t, 1, sess.endInputCount)
}

func TestSendElementToSession_EndOfStreamWithoutEndInputter(t *testing.T) {
	// bareSession does not implement EndInputter — must hit the warn branch, no panic.
	s := stageWithSession(newBareSession())
	elem := StreamElement{EndOfStream: true}
	s.sendElementToSession(context.Background(), &elem)
}

func TestSendElementToSession_ToolResponses(t *testing.T) {
	sess := newRecordingSession()
	s := stageWithSession(sess)

	elem := StreamElement{}
	elem.Meta.ToolResponses = []providers.ToolResponse{{ToolCallID: "c1", Result: "42"}}
	s.sendElementToSession(context.Background(), &elem)

	require.Len(t, sess.toolResponses, 1)
	assert.Equal(t, "c1", sess.toolResponses[0][0].ToolCallID)
}

func TestSendElementToSession_ToolResponsesUnsupported(t *testing.T) {
	// bareSession lacks ToolResponseSupport — warn branch, no panic.
	s := stageWithSession(newBareSession())
	elem := StreamElement{}
	elem.Meta.ToolResponses = []providers.ToolResponse{{ToolCallID: "c1", Result: "42"}}
	s.sendElementToSession(context.Background(), &elem)
}

func TestSendElementToSession_SystemPromptSentAsContext(t *testing.T) {
	sess := newRecordingSession()
	s := NewDuplexProviderStageWithTurnState(
		providersmock.NewStreamingProvider("t", "m", false), baseConfig(), nil,
		&TurnState{SystemPrompt: "you are a bot"},
	)
	s.session = sess
	s.systemPromptSent = false

	txt := "hi"
	elem := StreamElement{Text: &txt}
	s.sendElementToSession(context.Background(), &elem)

	require.Len(t, sess.systemCtx, 1)
	assert.Equal(t, "you are a bot", sess.systemCtx[0])
	assert.True(t, s.systemPromptSent)
}

// =============================================================================
// forwardInputElements
// =============================================================================

func TestForwardInputElements_ForwardsMessagesToolResultsAndSignals(t *testing.T) {
	sess := newRecordingSession()
	s := stageWithSession(sess)

	input := make(chan StreamElement, 8)
	output := make(chan StreamElement, 8)
	done := make(chan error, 1)

	// 1. User message with a turn_id -> forwarded to output, turn_id queued.
	turnID := "turn-99"
	userMsg := StreamElement{Message: &types.Message{Role: roleUser, Content: "hi"}}
	userMsg.Meta.TurnID = &turnID
	input <- userMsg

	// 2. AllResponsesReceived marker -> consumed, not forwarded.
	arr := StreamElement{}
	arr.Meta.AllResponsesReceived = true
	input <- arr

	// 3. Tool result messages -> forwarded to output.
	trm := StreamElement{}
	trm.Meta.ToolResultMessages = []types.Message{{
		Role:       roleAssistant,
		Content:    "tool out",
		ToolResult: &types.MessageToolResult{Name: "mytool"},
	}}
	input <- trm

	// 4. Audio -> sent to session.
	input <- duplexAudioElem(pcm(16))

	close(input)

	go s.forwardInputElements(context.Background(), input, output, done)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("forwardInputElements did not signal done")
	}

	// inputDoneCh must be closed.
	select {
	case <-s.inputDoneCh:
	default:
		t.Error("inputDoneCh not closed")
	}
	// allResponsesReceivedCh must be closed by the marker element.
	select {
	case <-s.allResponsesReceivedCh:
	default:
		t.Error("allResponsesReceivedCh not closed")
	}

	assert.True(t, s.hasQueuedTurnID(), "turn_id queued from user message")
	assert.False(t, s.turnStartTime.IsZero(), "turn timing started for user message")
	require.Len(t, sess.chunks, 1, "audio forwarded to session")

	// Drain output: expect the user message + the tool-result message.
	var messages int
	for len(output) > 0 {
		e := <-output
		if e.Message != nil {
			messages++
		}
	}
	assert.Equal(t, 2, messages, "user message and tool-result message forwarded")
}

func TestForwardInputElements_ContextCanceled(t *testing.T) {
	s := stageWithSession(newRecordingSession())
	input := make(chan StreamElement) // never fed, never closed
	output := make(chan StreamElement, 1)
	done := make(chan error, 1)

	ctx, cancel := context.WithCancel(context.Background())
	go s.forwardInputElements(ctx, input, output, done)
	cancel()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("forwardInputElements did not return on context cancel")
	}
}

// =============================================================================
// small predicates
// =============================================================================

func TestTranscriptionFinal(t *testing.T) {
	s := newDuplexStageForUnit()
	assert.False(t, s.transcriptionFinal(&providers.StreamChunk{}), "nil metadata")
	assert.False(t, s.transcriptionFinal(&providers.StreamChunk{Metadata: map[string]interface{}{"other": 1}}))
	assert.True(t, s.transcriptionFinal(&providers.StreamChunk{Metadata: map[string]interface{}{"transcription_final": true}}))
}

func TestHasQueuedTurnID(t *testing.T) {
	s := newDuplexStageForUnit()
	assert.False(t, s.hasQueuedTurnID())
	s.turnIDQueue = []string{"t1"}
	assert.True(t, s.hasQueuedTurnID())
}

// =============================================================================
// handleResponseChunk
// =============================================================================

func TestHandleResponseChunk_ErrorChunk(t *testing.T) {
	s := newDuplexStageForUnit()
	out := make(chan StreamElement, 4)
	want := errors.New("boom")
	err := s.handleResponseChunk(context.Background(), &providers.StreamChunk{Error: want}, out)
	require.ErrorIs(t, err, want)
	require.Len(t, out, 1)
	got := <-out
	require.NotNil(t, got.Error)
}

func TestHandleResponseChunk_AccumulatesContentMediaAndToolCalls(t *testing.T) {
	s := newDuplexStageForUnit()
	out := make(chan StreamElement, 4)

	err := s.handleResponseChunk(context.Background(), &providers.StreamChunk{
		Content:   "spoken words",
		MediaData: &providers.StreamMediaData{Data: []byte{1, 2, 3}},
		ToolCalls: []types.MessageToolCall{{ID: "1", Name: "lookup"}},
	}, out)
	require.NoError(t, err)
	assert.Equal(t, "spoken words", s.accumulatedText.String())
	assert.Equal(t, []byte{1, 2, 3}, s.accumulatedMedia)
	require.Len(t, s.accumulatedToolCalls, 1)
}

func TestHandleResponseChunk_InputTranscriptionAccumulates(t *testing.T) {
	s := newDuplexStageForUnit()
	s.turnIDQueue = []string{"t1"} // suppress fast-path materialization
	out := make(chan StreamElement, 4)

	chunk := &providers.StreamChunk{Metadata: map[string]interface{}{
		"type":          "input_transcription",
		"transcription": "user said hi",
	}}
	require.NoError(t, s.handleResponseChunk(context.Background(), chunk, out))
	assert.Equal(t, "user said hi", s.inputTranscription.String())
}

func TestHandleResponseChunk_TranscriptionFinalFastPath(t *testing.T) {
	s := newDuplexStageForUnit()
	out := make(chan StreamElement, 4)

	chunk := &providers.StreamChunk{Metadata: map[string]interface{}{
		"type":                "input_transcription",
		"transcription":       "book a flight",
		"transcription_final": true,
	}}
	require.NoError(t, s.handleResponseChunk(context.Background(), chunk, out))
	require.Len(t, out, 1)
	msg := <-out
	require.NotNil(t, msg.Message)
	assert.Equal(t, "book a flight", msg.Message.Content)
	assert.Equal(t, 0, s.inputTranscription.Len(), "buffer reset after fast-path emit")
}

func TestHandleResponseChunk_OutputTranscriptionAppends(t *testing.T) {
	s := newDuplexStageForUnit()
	s.turnIDQueue = []string{"t1"}
	out := make(chan StreamElement, 4)

	chunk := &providers.StreamChunk{
		Delta:    "partial ",
		Metadata: map[string]interface{}{"type": "output_transcription"},
	}
	require.NoError(t, s.handleResponseChunk(context.Background(), chunk, out))
	assert.Equal(t, "partial ", s.accumulatedText.String())
}

func TestHandleResponseChunk_ReasoningEmitsDelta(t *testing.T) {
	s := newDuplexStageForUnit()
	s.turnIDQueue = []string{"t1"}
	out := make(chan StreamElement, 4)

	chunk := &providers.StreamChunk{Reasoning: "thinking..."}
	require.NoError(t, s.handleResponseChunk(context.Background(), chunk, out))
	assert.Equal(t, "thinking...", s.accumulatedReasoning.String())

	// handleResponseChunk emits a ReasoningDelta element plus the trailing
	// chunkToElement passthrough — find the reasoning one.
	var reasoning *ReasoningDelta
	for len(out) > 0 {
		e := <-out
		if e.Reasoning != nil {
			reasoning = e.Reasoning
		}
	}
	require.NotNil(t, reasoning)
	assert.Equal(t, "thinking...", reasoning.Text)
}

// =============================================================================
// forwardResponseElements
// =============================================================================

func TestForwardResponseElements_NilResponseChannel(t *testing.T) {
	sess := newRecordingSession()
	sess.respNil = true
	s := stageWithSession(sess)
	out := make(chan StreamElement, 1)
	err := s.forwardResponseElements(context.Background(), out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "response channel is nil")
}

func TestForwardResponseElements_EmitsPartialOnSessionClose(t *testing.T) {
	sess := newRecordingSession()
	s := stageWithSession(sess)
	out := make(chan StreamElement, 4)

	// Feed one content chunk, then close the response channel to simulate the
	// provider ending the session.
	sess.respCh <- providers.StreamChunk{Content: "final answer"}
	close(sess.respCh)

	err := s.forwardResponseElements(context.Background(), out)
	require.NoError(t, err)

	// Expect the accumulated content emitted as a partial message.
	var got *StreamElement
	for len(out) > 0 {
		e := <-out
		if e.Message != nil {
			got = &e
		}
	}
	require.NotNil(t, got, "expected a message element on session close")
	assert.Equal(t, "final answer", got.Message.Content)
	assert.True(t, got.EndOfStream)
}

func TestForwardResponseElements_ContextCancelEmitsPartial(t *testing.T) {
	sess := newRecordingSession()
	s := stageWithSession(sess)
	s.accumulatedText.WriteString("interrupted content")

	out := make(chan StreamElement, 4)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.forwardResponseElements(ctx, out)
	require.ErrorIs(t, err, context.Canceled)

	require.GreaterOrEqual(t, len(out), 1)
	got := <-out
	require.NotNil(t, got.Message)
	assert.Equal(t, "interrupted content", got.Message.Content)
}

// =============================================================================
// replayAndMerge
// =============================================================================

func TestReplayAndMerge_ReplaysThenForwards(t *testing.T) {
	s := newDuplexStageForUnit()
	remaining := make(chan StreamElement, 2)
	a := "a"
	b := "b"
	remaining <- StreamElement{Text: &b}
	close(remaining)

	merged := s.replayAndMerge(context.Background(), []StreamElement{{Text: &a}}, remaining)

	var texts []string
	for e := range merged {
		if e.Text != nil {
			texts = append(texts, *e.Text)
		}
	}
	assert.Equal(t, []string{"a", "b"}, texts)
}
