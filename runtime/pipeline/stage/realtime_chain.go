package stage

import (
	"errors"
	"fmt"
	"reflect"
	"sort"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/duplex"
)

// RealtimeSlot names a position in the realtime chain NewRealtimeChain builds.
// A host inserts its own stages after a slot (RealtimeChainConfig.After) and
// removes the stage in a slot (RealtimeChainConfig.Drop) by its name.
type RealtimeSlot string

// The slots of the realtime chain, in chain order.
const (
	// RealtimeSlotProvider holds the DuplexProviderStage. It is always present.
	RealtimeSlotProvider RealtimeSlot = "provider"
	// RealtimeSlotCue holds the cue stage (NewDuplexCueStage). Present unless
	// dropped.
	RealtimeSlotCue RealtimeSlot = "cue"
	// RealtimeSlotTranscriptReorder holds the TranscriptReorderStage, when
	// RealtimeChainConfig.ReorderInputTranscript is set.
	RealtimeSlotTranscriptReorder RealtimeSlot = "transcript-reorder"
	// RealtimeSlotPlayoutHold holds the PlayoutHoldStage, when
	// RealtimeChainConfig.PlayoutHold is set.
	RealtimeSlotPlayoutHold RealtimeSlot = "playout-hold"
	// RealtimeSlotRecording holds the output RecordingStage, when
	// RealtimeChainConfig.RecordingStore is set.
	RealtimeSlotRecording RealtimeSlot = "recording"
	// RealtimeSlotAudioTap holds RealtimeChainConfig.AudioTap.
	RealtimeSlotAudioTap RealtimeSlot = "audio-tap"
	// RealtimeSlotMemoryExtraction holds RealtimeChainConfig.MemoryExtraction.
	RealtimeSlotMemoryExtraction RealtimeSlot = "memory-extraction"
	// RealtimeSlotMessageBroadcast holds the MessageBroadcastStage, when
	// RealtimeChainConfig.MessageBroadcast is set.
	RealtimeSlotMessageBroadcast RealtimeSlot = "message-broadcast"
	// RealtimeSlotSave holds RealtimeChainConfig.Save.
	RealtimeSlotSave RealtimeSlot = "save"
	// RealtimeSlotPacing holds the output AudioPacingStage, named
	// "audio-pacing-output", when RealtimeChainConfig.Pacing is not
	// RealtimePacingNone.
	RealtimeSlotPacing RealtimeSlot = "pacing"
)

// realtimeSlots is the chain order. It is defined here and nowhere else.
var realtimeSlots = []RealtimeSlot{
	RealtimeSlotProvider,
	RealtimeSlotCue,
	RealtimeSlotTranscriptReorder,
	RealtimeSlotPlayoutHold,
	RealtimeSlotRecording,
	RealtimeSlotAudioTap,
	RealtimeSlotMemoryExtraction,
	RealtimeSlotMessageBroadcast,
	RealtimeSlotSave,
	RealtimeSlotPacing,
}

// RealtimePacing selects the output audio pacing stage of the realtime chain.
type RealtimePacing int

const (
	// RealtimePacingNone adds no pacing stage: the reply audio leaves the
	// chain as fast as the provider sends it. For a consumer with no realtime
	// speaker to protect.
	RealtimePacingNone RealtimePacing = iota
	// RealtimePacingFlushOnCut paces the reply audio to real time and drops
	// the queued audio of a reply that is cut (AudioPacingStage.WithFlushOnCut).
	// For a pipeline that plays to a listener who can barge in.
	RealtimePacingFlushOnCut
	// RealtimePacingPlayThrough paces the reply audio to real time and plays a
	// cut reply's queued audio out.
	RealtimePacingPlayThrough
)

// RealtimeChainConfig is what a host varies in the realtime chain. The zero
// value builds the provider stage followed by the cue stage.
type RealtimeChainConfig struct {
	// Cue configures the cue stage: the TTS service that voices its clips and
	// the cache that keeps them.
	Cue CueOptions

	// ReorderInputTranscript adds the transcript reorder stage, for a provider
	// that delivers the caller's transcript after the reply (OpenAI Realtime).
	ReorderInputTranscript bool
	// InputTranscriptPlaceholder is the caller text the reorder stage writes
	// for a turn that ends with no transcript; empty leaves the turn out.
	InputTranscriptPlaceholder string

	// PlayoutHold adds the playout hold stage, for a pipeline whose reply
	// audio is played to a listener in real time.
	PlayoutHold bool

	// RecordingStore, when set, adds an output recording stage writing to it
	// with Recording (its Position set to RecordingPositionOutput).
	RecordingStore events.EventStore
	// Recording configures the output recording stage.
	Recording RecordingStageConfig

	// AudioTap, MemoryExtraction and Save are host-built stages placed in
	// their slots; nil (typed or untyped) leaves the slot empty.
	AudioTap         Stage
	MemoryExtraction Stage
	Save             Stage

	// MessageBroadcast, when set, adds a MessageBroadcastStage publishing
	// each completed message on it.
	MessageBroadcast *events.Emitter

	// Pacing selects the output audio pacing stage; a value other than the
	// RealtimePacing constants is an error.
	Pacing RealtimePacing

	// After inserts stages directly after a slot, in the order given. A slot
	// that is empty or dropped still places its stages where it would be.
	After map[RealtimeSlot][]Stage

	// Drop removes the stage in each named slot. The provider slot cannot be
	// dropped.
	Drop []RealtimeSlot
}

// Errors returned by NewRealtimeChain.
var (
	// ErrRealtimeChainProvider is returned when the provider stage is nil or
	// dropped.
	ErrRealtimeChainProvider = errors.New("realtime chain: a provider stage is required")
	// ErrRealtimeChainSlot is returned for a slot name the chain does not have,
	// or a nil stage inserted after a slot.
	ErrRealtimeChainSlot = errors.New("realtime chain: invalid slot")
	// ErrRealtimeChainPacing is returned for a RealtimePacing value the chain
	// does not have.
	ErrRealtimeChainPacing = errors.New("realtime chain: unknown pacing mode")
)

// NewRealtimeChain returns the stages of a realtime (duplex) pipeline from
// the provider stage on, in the order every host runs them:
//
//	provider, cue, transcript-reorder, playout-hold, recording, audio-tap,
//	memory-extraction, message-broadcast, save, pacing
//
// The cue stage directly follows the provider, ahead of the stages that treat
// its audio as reply audio. The transcript reorder stage runs before
// everything that reads the transcript, the playout hold before everything
// that records what was heard, and pacing last, so the bookkeeping stages run
// at full speed while the reply reaches a realtime speaker at playback rate.
//
// The host puts its own front stages (prompt assembly, input recording,
// resampling, client-side turn detection) ahead of the returned stages and
// chains them with a PipelineBuilder.
//
// Build one chain per provider stage. The cue stage plays the provider's cues,
// so each call that succeeds rewires them: to the cue stage it returns, or to
// none when the cue slot is dropped. A chain built earlier for the same
// provider then plays none. A call that returns an error changes nothing.
//
// A nil stage, typed or untyped, in AudioTap, MemoryExtraction or Save leaves
// the slot empty; in After it is an error.
func NewRealtimeChain(provider *DuplexProviderStage, cfg RealtimeChainConfig) ([]Stage, error) {
	if provider == nil {
		return nil, ErrRealtimeChainProvider
	}
	dropped, err := realtimeDropped(cfg)
	if err != nil {
		return nil, err
	}
	if err := checkRealtimeAfter(cfg.After); err != nil {
		return nil, err
	}
	if !isRealtimePacing(cfg.Pacing) {
		return nil, fmt.Errorf("%w: %d", ErrRealtimeChainPacing, cfg.Pacing)
	}
	duplex.DetachCues(provider) // the cue slot's stage, if any, wires its own
	var out []Stage
	for _, slot := range realtimeSlots {
		if !dropped[slot] {
			if st := cfg.slotStage(slot, provider); !isNilStage(st) {
				out = append(out, st)
			}
		}
		out = append(out, cfg.After[slot]...)
	}
	return out, nil
}

// isNilStage reports whether st is nil, or an interface holding a nil value.
func isNilStage(st Stage) bool {
	if st == nil {
		return true
	}
	v := reflect.ValueOf(st)
	switch v.Kind() { //nolint:exhaustive // only nillable kinds can hold a nil
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
		return v.IsNil()
	default:
		return false
	}
}

func isRealtimePacing(p RealtimePacing) bool {
	switch p {
	case RealtimePacingNone, RealtimePacingFlushOnCut, RealtimePacingPlayThrough:
		return true
	default:
		return false
	}
}

// realtimeDropped returns the set of slots cfg drops.
func realtimeDropped(cfg RealtimeChainConfig) (map[RealtimeSlot]bool, error) {
	dropped := make(map[RealtimeSlot]bool, len(cfg.Drop))
	for _, slot := range cfg.Drop {
		if slot == RealtimeSlotProvider {
			return nil, ErrRealtimeChainProvider
		}
		if !isRealtimeSlot(slot) {
			return nil, fmt.Errorf("%w: drop %q", ErrRealtimeChainSlot, slot)
		}
		dropped[slot] = true
	}
	return dropped, nil
}

// checkRealtimeAfter rejects an insertion after an unknown slot, or of a nil
// stage. It checks the slots in name order, so the error names the same slot
// on every call.
func checkRealtimeAfter(after map[RealtimeSlot][]Stage) error {
	slots := make([]RealtimeSlot, 0, len(after))
	for slot := range after {
		slots = append(slots, slot)
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i] < slots[j] })
	for _, slot := range slots {
		if !isRealtimeSlot(slot) {
			return fmt.Errorf("%w: after %q", ErrRealtimeChainSlot, slot)
		}
		for _, st := range after[slot] {
			if isNilStage(st) {
				return fmt.Errorf("%w: nil stage after %q", ErrRealtimeChainSlot, slot)
			}
		}
	}
	return nil
}

func isRealtimeSlot(slot RealtimeSlot) bool {
	for _, s := range realtimeSlots {
		if s == slot {
			return true
		}
	}
	return false
}

// slotStage builds the stage cfg puts in slot, or nil for none.
func (cfg *RealtimeChainConfig) slotStage(slot RealtimeSlot, provider *DuplexProviderStage) Stage {
	switch slot {
	case RealtimeSlotProvider:
		return provider
	case RealtimeSlotCue:
		return NewDuplexCueStage(provider, cfg.Cue)
	case RealtimeSlotTranscriptReorder:
		if cfg.ReorderInputTranscript {
			return NewTranscriptReorderStage(cfg.InputTranscriptPlaceholder)
		}
	case RealtimeSlotPlayoutHold:
		if cfg.PlayoutHold {
			return NewPlayoutHoldStage()
		}
	case RealtimeSlotRecording:
		if cfg.RecordingStore != nil {
			rec := cfg.Recording
			rec.Position = RecordingPositionOutput
			return NewRecordingStage(cfg.RecordingStore, rec)
		}
	case RealtimeSlotAudioTap:
		return cfg.AudioTap
	case RealtimeSlotMemoryExtraction:
		return cfg.MemoryExtraction
	case RealtimeSlotMessageBroadcast:
		if cfg.MessageBroadcast != nil {
			return NewMessageBroadcastStage(cfg.MessageBroadcast)
		}
	case RealtimeSlotSave:
		return cfg.Save
	case RealtimeSlotPacing:
		return cfg.pacingStage()
	}
	return nil
}

// pacingStage builds the output pacing stage cfg.Pacing selects, or nil.
func (cfg *RealtimeChainConfig) pacingStage() Stage {
	switch cfg.Pacing {
	case RealtimePacingFlushOnCut:
		return NewNamedAudioPacingStage(realtimeOutputPacingName).WithFlushOnCut()
	case RealtimePacingPlayThrough:
		return NewNamedAudioPacingStage(realtimeOutputPacingName)
	case RealtimePacingNone:
		return nil
	default: // rejected by NewRealtimeChain
		return nil
	}
}

// realtimeOutputPacingName names the chain's pacing stage; the "-output"
// suffix routes its health metrics to the output direction.
const realtimeOutputPacingName = "audio-pacing-output"
