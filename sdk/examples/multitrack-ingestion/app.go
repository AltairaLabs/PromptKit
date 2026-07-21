package main

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// newConversation opens a WithIngestion duplex whose input side is a two-track
// sdk.MultiTrackIngestion graph. Audio tagged Source=speaker-a|speaker-b is
// routed to independent resample->VAD->STT->label pipelines and merged into the
// stream the agent chain fires on, once per turn.
//
// TurnConfig is left nil, so each track's AudioTurnStage builds its own VAD — a
// stateful detector must not be shared across tracks.
func newConversationWithOpts(onTranscript func(speaker, text string), providerOpts []sdk.Option) (*sdk.Conversation, error) {
	ingest := sdk.MultiTrackIngestion(sdk.MultiTrackIngestionConfig{
		Tracks: []sdk.IngestionTrack{
			{Source: "speaker-a", Speaker: "SPEAKER-A", STT: newScriptedSTT("speaker-a")},
			{Source: "speaker-b", Speaker: "SPEAKER-B", STT: newScriptedSTT("speaker-b")},
		},
		OnTranscript: onTranscript,
	})

	opts := append([]sdk.Option{sdk.WithIngestion(ingest)}, providerOpts...)
	conv, err := sdk.OpenDuplex("./assistant.pack.json", "assist", opts...)
	if err != nil {
		return nil, fmt.Errorf("open duplex: %w", err)
	}
	return conv, nil
}

// newConversation is the mock-provider convenience used by the test.
func newConversation(provider providers.Provider, onTranscript func(speaker, text string)) (*sdk.Conversation, error) {
	return newConversationWithOpts(onTranscript, []sdk.Option{sdk.WithProvider(provider)})
}

// feed streams the built-in script through the ingestion graph: one synthetic
// PCM turn per script line, tagged with that line's Source so the router splits
// it onto the right track. Burst-fed (no pacing) — VAD closes turns on audio
// timing, not wall-clock.
func feed(ctx context.Context, conv *sdk.Conversation) error {
	for _, tn := range script {
		pcm := synthTurnPCM()
		for off := 0; off < len(pcm); off += bytesPerFrame {
			end := min(off+bytesPerFrame, len(pcm))
			if err := conv.SendChunk(ctx, &providers.StreamChunk{
				Source: tn.Source,
				MediaData: &providers.StreamMediaData{
					Data:       pcm[off:end],
					MIMEType:   "audio/pcm",
					SampleRate: 8000,
					Channels:   1,
				},
			}); err != nil {
				return fmt.Errorf("send %s: %w", tn.Source, err)
			}
		}
	}
	return nil
}
