package sdk

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// An executor registered with OnToolExecutor that returns content parts
// through tools.MultimodalExecutor has them delivered to the model, as the
// tool registry delivers them for the same executor.

// badgePNG stands in for an image's base64 data.
const badgePNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

// mediaExecutor is a MultimodalExecutor: ExecuteMultimodal returns a caption
// and an image; Execute, which the registry does not call for it, returns
// text only.
type mediaExecutor struct{}

func (mediaExecutor) Name() string { return "media-exec" }

func (mediaExecutor) Execute(context.Context, *tools.ToolDescriptor, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"caption":"text only"}`), nil
}

func (mediaExecutor) ExecuteMultimodal(
	context.Context, *tools.ToolDescriptor, json.RawMessage,
) (json.RawMessage, []types.ContentPart, error) {
	return json.RawMessage(`{"caption":"badge"}`),
		[]types.ContentPart{types.NewImagePartFromData(badgePNG, types.MIMETypeImagePNG, nil)}, nil
}

// requestRecordingProvider is callerToolProvider recording each request's
// messages.
type requestRecordingProvider struct {
	*callerToolProvider
	mu   sync.Mutex
	reqs [][]types.Message
}

func (p *requestRecordingProvider) PredictWithTools(
	ctx context.Context, req providers.PredictionRequest, pt providers.ProviderTools, choice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, append([]types.Message(nil), req.Messages...))
	p.mu.Unlock()
	return p.callerToolProvider.PredictWithTools(ctx, req, pt, choice)
}

// imageData returns the base64 data of each image part in parts.
func imageData(parts []types.ContentPart) []string {
	var out []string
	for _, p := range parts {
		if p.Type == types.ContentTypeImage && p.Media != nil && p.Media.Data != nil {
			out = append(out, *p.Media.Data)
		}
	}
	return out
}

func TestOnToolExecutor_MultimodalExecutorPartsReachTheModel(t *testing.T) {
	prov := &requestRecordingProvider{callerToolProvider: newCallerToolProvider()}
	conv, err := Open(createTestPackFile(t, callerToolsPack), "chat",
		WithProvider(prov), WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnToolExecutor("lookup", mediaExecutor{})

	resp, err := conv.Send(context.Background(), "ada")
	require.NoError(t, err)
	assert.Equal(t, "done", resp.Text())

	prov.mu.Lock()
	defer prov.mu.Unlock()
	require.Len(t, prov.reqs, 2, "the tool round and the answer")
	msgs := prov.reqs[1]
	last := msgs[len(msgs)-1]
	require.Equal(t, "tool", last.Role)
	require.NotNil(t, last.ToolResult)
	assert.Equal(t, []string{badgePNG}, imageData(last.ToolResult.Parts), "the image goes to the model")
	// A result with parts is its parts; Execute's text-only result is not
	// what the model sees.
	assert.Empty(t, last.ToolResult.GetTextContent(), "the result is ExecuteMultimodal's, not Execute's")
}

// partsSession is a realtime session that takes tool result parts and
// records the responses it is sent.
type partsSession struct {
	*callerToolSession
	mu        sync.Mutex
	responses []providers.ToolResponse
}

func (s *partsSession) SupportsToolResponseParts() bool { return true }

func (s *partsSession) SendToolResponses(ctx context.Context, rs []providers.ToolResponse) error {
	s.mu.Lock()
	s.responses = append(s.responses, rs...)
	s.mu.Unlock()
	return s.callerToolSession.SendToolResponses(ctx, rs)
}

func (s *partsSession) SendToolResponse(ctx context.Context, id, result string) error {
	return s.SendToolResponses(ctx, []providers.ToolResponse{{ToolCallID: id, Result: result}})
}

func (s *partsSession) sent() []providers.ToolResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]providers.ToolResponse(nil), s.responses...)
}

type partsStreamProvider struct {
	*mock.StreamingProvider
	sess *partsSession
}

func (p *partsStreamProvider) CreateStreamSession(
	context.Context, *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	return p.sess, nil
}

func TestOnToolExecutor_MultimodalExecutorPartsReachARealtimeModel(t *testing.T) {
	sess := &partsSession{callerToolSession: &callerToolSession{MockStreamSession: mock.NewMockStreamSession()}}
	conv, err := OpenDuplex(createTestPackFile(t, callerToolsPack), "chat",
		WithProvider(&partsStreamProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt", false), sess: sess}),
		WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnToolExecutor("lookup", mediaExecutor{})
	convDrain(t, conv)

	require.NoError(t, conv.SendText(context.Background(), "ada"))
	require.Eventually(t, func() bool { return len(sess.sent()) > 0 }, 5*time.Second, 5*time.Millisecond)

	got := sess.sent()[0]
	assert.Equal(t, callerCallID, got.ToolCallID)
	assert.Equal(t, []string{badgePNG}, imageData(got.Parts), "the image goes to the realtime model")
	assert.Empty(t, got.Result, "the result is ExecuteMultimodal's parts, not Execute's text")
}

// A call an OnToolAsync check lets through runs the OnToolExecutor executor,
// and its parts reach the model too.
func TestOnToolExecutor_MultimodalPartsOnACallTheApprovalCheckLetsThrough(t *testing.T) {
	prov := &requestRecordingProvider{callerToolProvider: newCallerToolProvider()}
	conv, err := Open(createTestPackFile(t, callerToolsPack), "chat",
		WithProvider(prov), WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnToolExecutor("lookup", mediaExecutor{})
	conv.OnToolAsync("lookup",
		func(map[string]any) sdktools.PendingResult { return sdktools.PendingResult{} },
		func(map[string]any) (any, error) { return "async", nil })

	_, err = conv.Send(context.Background(), "ada")
	require.NoError(t, err)

	prov.mu.Lock()
	defer prov.mu.Unlock()
	require.Len(t, prov.reqs, 2)
	msgs := prov.reqs[1]
	last := msgs[len(msgs)-1]
	require.NotNil(t, last.ToolResult)
	assert.Equal(t, []string{badgePNG}, imageData(last.ToolResult.Parts))
}
