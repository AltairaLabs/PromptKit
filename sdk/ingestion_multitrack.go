package sdk

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	intpipeline "github.com/AltairaLabs/PromptKit/sdk/internal/pipeline"
)

// roleUser is the message role for a transcribed turn handed to the agent chain.
const roleUser = "user"

// IngestionTrack describes one named audio track fed into the multi-track
// ingestion graph built by MultiTrackIngestion. Each track gets its own
// independent resample→VAD-turn→STT→label pipeline, keyed on the audio's
// StreamChunk.Source.
type IngestionTrack struct {
	// Source matches StreamChunk.Source on inbound audio destined for this
	// track — the value passed to Conversation.SendChunk. It also uniquifies
	// this track's stage names, so it must be distinct across tracks.
	Source string

	// Speaker prefixes each transcribed Message from this track, e.g.
	// "CUSTOMER" yields "CUSTOMER: <transcript>", so a multi-party agent chain
	// can attribute turns to a speaker. Empty means no prefix.
	Speaker string

	// STT transcribes this track's audio. Required.
	STT base.STTProvider

	// TurnConfig optionally overrides turn-detection/VAD config for this track.
	// Nil uses stage.DefaultAudioTurnConfig(), which lets each track's
	// AudioTurnStage construct its own VAD. MultiTrackIngestion always forces
	// EmitEndOfTurn on the effective config so the agent fires once per turn.
	//
	// If you set an explicit VAD (TurnConfig.VAD), give each track its own
	// instance — the detector is stateful, so sharing one across tracks
	// interleaves their audio into a single VAD state and corrupts turn
	// detection on both.
	TurnConfig *stage.AudioTurnConfig
}

// MultiTrackIngestionConfig configures MultiTrackIngestion.
type MultiTrackIngestionConfig struct {
	// Tracks are the audio tracks to ingest. At least one is required.
	Tracks []IngestionTrack

	// OnTranscript, if set, is called with the speaker label and raw
	// transcribed text for every completed turn on any track — a convenient
	// hook for mirroring the live transcript to a UI without subscribing to
	// EventAudioTranscription.
	OnTranscript func(speaker, text string)
}

// MultiTrackIngestion returns an IngestionFunc (for WithIngestion) that fans
// inbound audio out to one independent per-track pipeline each — resample → VAD
// turn detection → STT → speaker-labeled Message — keyed on StreamChunk.Source,
// then merges the labeled transcripts back into the single stream the standard
// agent chain consumes.
//
// It exists so a two-party / multi-party voice application does not have to
// re-derive the routing topology by hand, which hides two footguns:
//
//   - Control signals (EndOfStream/EndOfTurn/Interrupt) carry no Source, so the
//     router broadcasts them to every track. Routing only on Source would drop
//     them, the merge would never complete, and a duplex session's Drain/Close
//     would deadlock waiting on it.
//   - PipelineBuilder.Chain already registers the stages it wires, so each
//     track's stages are added exactly once (via Chain), never AddStage'd
//     first, which would trip Build's duplicate-name check.
//
// The returned function forces EmitEndOfTurn on every track so the streaming
// ProviderStage installed by the WithIngestion duplex path fires the agent once
// per turn (per track) rather than only at session close.
func MultiTrackIngestion(cfg MultiTrackIngestionConfig) IngestionFunc {
	return func(b *stage.PipelineBuilder) (string, error) {
		if len(cfg.Tracks) == 0 {
			return "", fmt.Errorf("MultiTrackIngestion: at least one track is required")
		}

		trackStages, err := buildAllTrackStages(cfg)
		if err != nil {
			return "", err
		}
		heads, tails, headBySource := trackEndpoints(cfg.Tracks, trackStages)

		router := stage.NewRouterStage("router", routeBySource(heads, headBySource))
		merge := stage.NewMergeStage("merge", len(cfg.Tracks))

		// Chain registers each track's stages exactly once (it appends to the
		// builder's stage list as well as wiring sequential edges), so no
		// AddStage precedes it — that would double-register and fail Build's
		// duplicate stage-name check.
		b.AddStage(router)
		b.AddStage(merge)
		b.Branch("router", heads...)
		for _, stages := range trackStages {
			b.Chain(stages...)
		}
		b.Merge("merge", tails...)

		return "merge", nil
	}
}

// buildAllTrackStages builds the per-track stage slices, rejecting a missing
// STT provider or a duplicate track source.
func buildAllTrackStages(cfg MultiTrackIngestionConfig) ([][]stage.Stage, error) {
	trackStages := make([][]stage.Stage, len(cfg.Tracks))
	seen := make(map[string]bool, len(cfg.Tracks))
	for i, track := range cfg.Tracks {
		if track.STT == nil {
			return nil, fmt.Errorf("MultiTrackIngestion: track %q has no STT provider", track.Source)
		}
		if seen[track.Source] {
			return nil, fmt.Errorf("MultiTrackIngestion: duplicate track source %q", track.Source)
		}
		seen[track.Source] = true

		stages, err := buildTrackStages(track, cfg.OnTranscript)
		if err != nil {
			return nil, err
		}
		trackStages[i] = stages
	}
	return trackStages, nil
}

// trackEndpoints returns each track's head and tail stage name, plus a map from
// track source to head stage name for the router.
func trackEndpoints(
	tracks []IngestionTrack, trackStages [][]stage.Stage,
) (heads, tails []string, headBySource map[string]string) {
	heads = make([]string, len(trackStages))
	tails = make([]string, len(trackStages))
	headBySource = make(map[string]string, len(trackStages))
	for i, stages := range trackStages {
		heads[i] = stages[0].Name()
		tails[i] = stages[len(stages)-1].Name()
		headBySource[tracks[i].Source] = stages[0].Name()
	}
	return heads, tails, headBySource
}

// routeBySource routes an element to its track by Source, broadcasting Source-less
// control signals (EndOfStream/EndOfTurn/Interrupt) to every track so they
// cascade through each per-track sub-chain and let the merge's ProcessMultiple
// complete. Routing only on Source would silently drop them (no source matches)
// and deadlock a duplex session's Drain/Close.
func routeBySource(heads []string, headBySource map[string]string) stage.RouterFunc {
	return func(e *stage.StreamElement) []string {
		if e.EndOfStream || e.EndOfTurn || e.Interrupt {
			return heads
		}
		if head, ok := headBySource[e.Source]; ok {
			return []string{head}
		}
		return nil
	}
}

// buildTrackStages returns the ordered stages for one track:
// resample → AudioTurn(VAD, EmitEndOfTurn) → STT → labeled ToMessage.
//
// The first three come from the shared internal BuildAudioTrackStages so the
// resample/turn/STT segment stays identical to the single-track VAD front. The
// trailing ToMessage stage is specific to ingestion sub-graphs: STTStage emits
// StreamElement{Text}, but the agent chain (ProviderStage) only ever acts on
// StreamElement.Message, so a bare transcript would silently vanish without
// this conversion. IngestionFunc callbacks own their whole sub-graph up to the
// node they hand back, so adding it here is squarely within the API surface.
func buildTrackStages(track IngestionTrack, onTranscript func(speaker, text string)) ([]stage.Stage, error) {
	turnCfg := stage.DefaultAudioTurnConfig()
	if track.TurnConfig != nil {
		turnCfg = *track.TurnConfig
	}
	// Fire the agent per turn: the streaming ProviderStage the WithIngestion
	// duplex path installs keys off EndOfTurn.
	turnCfg.EmitEndOfTurn = true

	stages, err := intpipeline.BuildAudioTrackStages(
		track.Source, true, turnCfg, stage.DefaultSTTStageConfig(), track.STT,
	)
	if err != nil {
		return nil, fmt.Errorf("MultiTrackIngestion: track %q: %w", track.Source, err)
	}

	speaker := track.Speaker
	label := func(elem stage.StreamElement) (stage.StreamElement, error) {
		if elem.Text == nil {
			return elem, nil
		}
		text := *elem.Text
		if onTranscript != nil {
			onTranscript(speaker, text)
		}
		labeled := text
		if speaker != "" {
			labeled = speaker + ": " + text
		}
		msg := types.NewTextMessage(roleUser, labeled)
		elem.Message = &msg
		elem.Text = nil
		return elem, nil
	}
	toMessage := stage.NewMapStage(track.Source+"_to_message", label)

	return append(stages, toMessage), nil
}
