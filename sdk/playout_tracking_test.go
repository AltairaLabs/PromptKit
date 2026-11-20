package sdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// playoutTrackingOutsideRealtime is Open's error for WithPlayoutTracking on a
// session the model does not speak in.
const playoutTrackingOutsideRealtime = "WithPlayoutTracking applies only to a realtime (ASM) duplex session, " +
	"where the model speaks; this session's replies are not tracked"

// WithPlayoutTracking tracks the playout of a realtime session's reply audio.
// A text conversation, and a duplex session whose input side the pipeline
// authors (VAD mode, custom ingestion), have no such playout, so
// opening one with it is an error rather than an option that does nothing.
func TestWithPlayoutTracking_RefusedOutsideRealtime(t *testing.T) {
	opens := map[string]func() (*Conversation, error){
		"text": func() (*Conversation, error) {
			return Open(writeIngestionTestPack(t), "main", WithProvider(&recordingTextProvider{}),
				WithSkipSchemaValidation(), WithPlayoutTracking())
		},
		"vad": func() (*Conversation, error) {
			return OpenDuplex(writeIngestionTestPack(t), "main", WithProvider(&recordingTextProvider{}),
				WithSkipSchemaValidation(), WithPlayoutTracking(),
				WithVADMode(&convMockSTTService{}, newConvMockTTSService(), nil))
		},
		"ingestion": func() (*Conversation, error) {
			return OpenDuplex(writeIngestionTestPack(t), "main",
				WithProvider(mock.NewStreamingProvider("mock", "mock-model", false)),
				WithSkipSchemaValidation(), WithPlayoutTracking(),
				WithIngestion(func(b *stage.PipelineBuilder) (string, error) {
					b.AddStage(stage.NewMapStage("ingest", func(e stage.StreamElement) (stage.StreamElement, error) {
						return e, nil
					}))
					return "ingest", nil
				}))
		},
	}
	for name, open := range opens {
		t.Run(name, func(t *testing.T) {
			conv, err := open()
			if conv != nil {
				_ = conv.Close()
			}
			require.Error(t, err)
			assert.Nil(t, conv)
			assert.Contains(t, err.Error(), playoutTrackingOutsideRealtime)
		})
	}
}
