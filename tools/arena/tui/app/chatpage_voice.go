package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	arenastore "github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/voice"
)

// voiceLevelMsg carries normalized RMS audio levels emitted by the voice driver
// on each frame. Stored in ChatPage.micLevel / ChatPage.agentLevel so Task 3
// can render a live meter.
type voiceLevelMsg struct {
	user  float32
	agent float32
}

// chatRefreshMsg signals that a message.created event arrived from the voice
// pipeline and the conversation panel should reload from the state store.
type chatRefreshMsg struct{}

// echoGuardThreshold is the RMS floor below which mic frames are dropped while
// the agent is speaking (≈ −34 dBFS, comfortably above silence).
// See voice.NewDriverWithGuard for the v1 limitation note on buffered drivers.
const echoGuardThreshold = 0.02

// startVoice wires the voice backend and launches the driver goroutine.
//
// It mirrors the blueprint from the old runVoiceChat (ea6c2dcc^1) with one
// change: the TUI host is the hub ChatPage instead of a standalone tea.Program.
// The send func is used to push voiceLevelMsg and chatRefreshMsg back into the
// bubbletea event loop from the driver goroutine.
//
// On success the cancel func is stored in p.voiceCancel; callers invoke it via
// ChatPage.Close() to stop the driver and release the mic.
//
// Returns nil when voice.ErrVoiceNotCompiled is detected (sets p.engineErr with
// build instructions instead of propagating the error as a crash).
func (p *ChatPage) startVoice(send func(tea.Msg)) tea.Cmd {
	if send == nil {
		send = func(tea.Msg) {}
	}

	// 1. Check AudioIO availability before doing anything expensive.
	audioIO, err := voice.NewAudioIO()
	if err != nil {
		if errors.Is(err, voice.ErrVoiceNotCompiled) {
			p.engineErr = fmt.Errorf(
				"voice requires a build with -tags voice; run: make build-arena-voice")
			return nil
		}
		p.engineErr = fmt.Errorf("open audio device: %w", err)
		return nil
	}

	return p.runVoice(audioIO, send)
}

// runVoice wires a live AudioIO to the duplex pipeline and launches the driver
// goroutine. It is separated from startVoice so tests can inject a fake AudioIO
// without needing a real PortAudio device.
//
// Steps 2-9 of the startVoice blueprint live here.
//
//nolint:cyclop // splitting would obscure the sequential lifecycle.
func (p *ChatPage) runVoice(audioIO voice.AudioIO, send func(tea.Msg)) tea.Cmd {
	// 2. Resolve duplex executor.
	duplexExec := p.engine.GetDuplexExecutor()
	if duplexExec == nil {
		p.engineErr = fmt.Errorf("duplex executor unavailable (engine built without duplex support)")
		return nil
	}

	// 3. Resolve the provider from the already-created InteractiveSession.
	// startSession creates p.session before calling startVoice, so the provider
	// has already been resolved through the engine's registry.
	resolvedProvider := p.session.Provider()
	cfg := p.engine.GetConfig()

	// 4. Build a minimal duplex scenario for ASM mode (provider-native turn detection).
	conversationID := fmt.Sprintf("interactive-voice-%d", time.Now().UnixNano())
	scenario := &config.Scenario{
		ID:       "interactive-voice",
		TaskType: p.taskType,
		Duplex: &config.DuplexConfig{
			Timeout: "30m",
			TurnDetection: &config.TurnDetectionConfig{
				Mode: config.TurnDetectionModeASM,
			},
		},
	}

	// 5. Resolve optional STT provider (VAD path; ignored by the ASM executor).
	var voiceSTT *config.Provider
	if p.voice.STTProviderID != "" {
		if prov, ok := cfg.LoadedSTTProviders[p.voice.STTProviderID]; ok {
			voiceSTT = prov
		}
	}

	// 6. Build an in-memory state store and event bus.
	stateStore := arenastore.NewArenaStateStore()
	p.voiceStore = stateStore
	p.voiceConvID = conversationID
	eventBus := events.NewEventBus()
	p.engine.SetEventBus(eventBus, engine.WithMessageEvents())

	req := &engine.ConversationRequest{
		Provider:       resolvedProvider,
		Scenario:       scenario,
		Config:         cfg,
		RunID:          conversationID,
		ConversationID: conversationID,
		StateStoreConfig: &engine.StateStoreConfig{
			Store: stateStore,
		},
		EventBus:         eventBus,
		VoiceSTT:         voiceSTT,
		VoiceOutputVoice: p.voice.OutputVoice,
	}

	// 7. Subscribe directly to the event bus so each message.created event
	// triggers a chatRefreshMsg in the bubbletea loop. We use a direct
	// SubscribeAll rather than the full tui.EventAdapter to avoid routing
	// all tui.*Msg types through ChatPage.Update (which does not handle them).
	// The unsubscribe func returned by SubscribeAll is intentionally discarded:
	// the driver goroutine defers eventBus.Close(), which drains all listeners,
	// making an explicit unsubscribe unnecessary.
	eventBus.SubscribeAll(func(event *events.Event) {
		if event.Type == events.EventMessageCreated {
			send(chatRefreshMsg{})
		}
	})

	// 8. Build the voice driver.
	onLevel := func(userLevel, agentLevel float32) {
		send(voiceLevelMsg{user: userLevel, agent: agentLevel})
	}

	runner := voice.LiveRunner(func(runCtx context.Context, mic <-chan []byte, play func([]byte)) error {
		return duplexExec.RunInteractiveVoice(runCtx, req, mic, play)
	})

	var drv *voice.Driver
	if p.voice.EchoGuard {
		guard := voice.NewEchoGuard(echoGuardThreshold)
		drv = voice.NewDriverWithGuard(audioIO, runner, onLevel, guard)
	} else {
		drv = voice.NewDriver(audioIO, runner, onLevel)
	}

	// 9. Launch the driver goroutine. The cancel func is stored so Close() can
	// signal RunInteractiveVoice to return and the mic to release.
	ctx, cancel := context.WithCancel(context.Background())
	p.voiceCancel = cancel

	go func() {
		defer eventBus.Close()
		driverErr := drv.Run(ctx)
		// Always tell the UI the voice session ended, however it ended (idle
		// timeout, pipeline error, or mic close). Without this the driver
		// goroutine exited silently and the console looked hung — a dead mic
		// meter with no explanation. A context cancellation is a clean end (idle
		// timeout or user quit), so it carries no error.
		if errors.Is(driverErr, context.Canceled) {
			driverErr = nil
		}
		send(voiceEndedMsg{err: driverErr})
	}()

	return nil
}
