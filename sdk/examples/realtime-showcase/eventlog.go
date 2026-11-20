package main

// eventlog.go prints the session as it happens, one timestamped line per
// event: the conversation's messages, barge-in, guardrail blocks, tool calls
// and what each provider call cost — in VAD mode, each STT and TTS call too.

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

const (
	// maxShownText is how much of a message's text a line shows.
	maxShownText = 72
	// playoutPollEvery and playoutPollFor bound how long the log waits for a
	// reply to reach the state store to read how much of it was heard.
	playoutPollEvery = 20 * time.Millisecond
	playoutPollFor   = 3 * time.Second
)

// eventLog writes the event lines. Its methods are safe to call from any
// goroutine: the event bus delivers each event type on its own goroutine.
type eventLog struct {
	mu     sync.Mutex
	out    io.Writer
	now    func() time.Time
	store  statestore.Store
	convID string
	calls  int
	// callNums numbers each provider call by its CallID, in the order the
	// calls completed, so late usage is labeled with the call it belongs to.
	callNums map[string]int
	// pendingUsage is usage for a call whose provider.call.completed has not
	// arrived yet, printed after it; pendingOrder is those calls in the
	// order their usage first arrived, so what is still pending at the end
	// prints in call order.
	pendingUsage map[string][]lateUsage
	pendingOrder []string
	total        float64
	wg           sync.WaitGroup
	// cutText is the text Response() carried since the last Interrupted
	// chunk, and cut whether one arrived this turn.
	cutText strings.Builder
	cut     bool
	// cuePlaying is set while Response() carries a cue's audio.
	cuePlaying bool
	// speechCosts is set in VAD mode: the STT and TTS services' cost is
	// stamped on the stored messages (stt_cost on the caller's, tts_cost on
	// the reply), and the log reads it from there.
	speechCosts bool
	// ended is closed by end: a speech cost not stored by then never will be.
	ended     chan struct{}
	endedOnce sync.Once
}

// newEventLog returns a log writing to out. The conversation's messages are
// read from store under convID, the conversation's ID.
func newEventLog(out io.Writer, store statestore.Store, convID string) *eventLog {
	return &eventLog{out: out, now: time.Now, store: store, convID: convID, ended: make(chan struct{})}
}

// attach subscribes the log to the conversation's event bus.
func (l *eventLog) attach(bus events.Bus) {
	bus.Subscribe(events.EventMessageCreated, l.onEvent)
	bus.Subscribe(events.EventValidationFailed, l.onEvent)
	bus.Subscribe(events.EventToolCallStarted, l.onEvent)
	bus.Subscribe(events.EventToolCallCompleted, l.onEvent)
	bus.Subscribe(events.EventToolCallFailed, l.onEvent)
	bus.Subscribe(events.EventProviderCallCompleted, l.onEvent)
	bus.Subscribe(events.EventProviderCallUsage, l.onEvent)
	bus.Subscribe(events.EventContextNoteAdded, l.onEvent)
}

// logf writes one line, stamped with the time.
func (l *eventLog) logf(kind, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logLockedf(kind, format, args...)
}

func (l *eventLog) logLockedf(kind, format string, args ...any) {
	fmt.Fprintf(l.out, "%s  %-9s %s\n", l.now().Format("15:04:05.000"), kind, fmt.Sprintf(format, args...))
}

// onEvent prints one bus event.
func (l *eventLog) onEvent(e *events.Event) {
	switch d := e.Data.(type) {
	case *events.MessageCreatedData:
		l.onMessage(d)
	case *events.ValidationEventData:
		l.onGuardrail(d)
	case *events.ToolCallEventData:
		l.onTool(e.Type, d)
	case *events.ProviderCallCompletedData:
		l.onCost(d.Cost, d.InputTokens, d.OutputTokens, d.CallID, false)
	case *events.ProviderCallUsageData:
		l.onCost(d.Cost, d.InputTokens, d.OutputTokens, d.CallID, true)
	case *events.ContextNoteData:
		// The model is told a guardrail withheld its reply or the caller's
		// words; the caller never hears or sees the note. The note does not
		// name the guardrails; the event does, for the log.
		if d.Validator == "" {
			l.logf("note", "told the model, not the caller: %q", d.Text)
			break
		}
		l.logf("note", "told the model, not the caller (%s): %q", d.Validator, d.Text)
	}
}

// onMessage prints a message as it joins the conversation. For an assistant
// reply it also reads the stored copy, which says how much of the reply the
// caller heard when it was cut.
func (l *eventLog) onMessage(d *events.MessageCreatedData) {
	text := d.GetContent()
	var extra string
	for _, tc := range d.ToolCalls {
		extra += " [calls " + tc.Name + "]"
	}
	if d.ToolResult != nil && hasImage(d.ToolResult.Parts) {
		extra += " [image]"
	}
	l.logf(d.Role, "message.created #%d %q%s", d.Index, shorten(text), extra)
	if d.Role == "assistant" && l.store != nil {
		l.wg.Add(1)
		go l.reportPlayout(d.Index)
	}
	if l.speechCosts && l.store != nil && text != "" {
		switch d.Role {
		case "user":
			l.wg.Add(1)
			go l.reportSpeechCost(d.Index, sttCostKey, "STT")
		case "assistant":
			l.wg.Add(1)
			go l.reportSpeechCost(d.Index, ttsCostKey, "TTS")
		}
	}
}

// The message metadata keys the VAD pipeline stamps speech cost under.
const (
	sttCostKey = "stt_cost"
	ttsCostKey = "tts_cost"
)

// reportSpeechCost waits for the message at index to be stored with its
// speech cost under key and prints it, adding it to the session total. The
// caller's message is stored with its STT cost only when its turn ends, which
// can be long after it is created, so the wait lasts until the log ends. A
// message with no cost there (an unpriced service, or a reply the caller
// barged in on before it was synthesized) prints nothing.
func (l *eventLog) reportSpeechCost(index int, key, service string) {
	defer l.wg.Done()
	for {
		ended := false
		select {
		case <-l.ended:
			ended = true
		default:
		}
		if cost, ok := l.storedMeta(index, key); ok {
			total, _ := cost["total_cost_usd"].(float64)
			l.mu.Lock()
			l.total += total
			l.logLockedf("cost", "%s for #%d: $%.4f (%s); session total $%.4f",
				service, index, total, quantities(cost["quantities"]), l.total)
			l.mu.Unlock()
			return
		}
		if ended {
			return
		}
		select {
		case <-l.ended:
		case <-time.After(playoutPollEvery):
		}
	}
}

// storedMeta returns the cost map stored under key on the message at index.
func (l *eventLog) storedMeta(index int, key string) (map[string]any, bool) {
	state, err := l.store.Load(context.Background(), l.convID)
	if err != nil || state == nil || len(state.Messages) <= index {
		return nil, false
	}
	cost, ok := state.Messages[index].Meta[key].(map[string]any)
	return cost, ok
}

// quantities names what a speech call was priced on, such as "5 second".
func quantities(q any) string {
	var parts []string
	switch m := q.(type) {
	case map[string]float64:
		for unit, n := range m {
			parts = append(parts, fmt.Sprintf("%g %s", n, unit))
		}
	case map[string]any:
		for unit, n := range m {
			parts = append(parts, fmt.Sprintf("%v %s", n, unit))
		}
	}
	if len(parts) == 0 {
		return "no quantities"
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// reportPlayout waits for the reply at index to be stored and prints how
// much of it was heard, if its playback was cut (barge-in, or the session
// closed while it played). A reply heard in full has no playout_cut.
func (l *eventLog) reportPlayout(index int) {
	defer l.wg.Done()
	for deadline := time.Now().Add(playoutPollFor); time.Now().Before(deadline); time.Sleep(playoutPollEvery) {
		state, err := l.store.Load(context.Background(), l.convID)
		if err != nil || state == nil || len(state.Messages) <= index {
			continue
		}
		meta := state.Messages[index].Meta
		cut, _ := meta[stage.MetaPlayoutCut].(string)
		if cut == "" {
			return
		}
		kind := "playout"
		if cut == stage.PlayoutCutBargeIn {
			kind = "barge-in"
		}
		l.logf(kind, "reply #%d cut (%s): heard_ms=%v audio_ms=%v played_fraction=%v",
			index, cut, meta[stage.MetaHeardMs], meta[stage.MetaAudioMs], meta[stage.MetaPlayedFraction])
		return
	}
}

// onGuardrail prints a guardrail that blocked a message.
func (l *eventLog) onGuardrail(d *events.ValidationEventData) {
	name := d.ValidatorName
	if d.Validator != nil && d.Validator.ID != "" {
		name = d.Validator.ID
	}
	l.logf("guardrail", "%s blocked the %s (%s); the turn ends, the session continues",
		name, guardrailTarget(d.Direction), strings.Join(d.Violations, "; "))
}

// guardrailTarget names what a guardrail in direction judged.
func guardrailTarget(direction string) string {
	if direction == "input" {
		return "user's input"
	}
	return "assistant's reply"
}

// onTool prints a tool call starting, returning or failing.
func (l *eventLog) onTool(t events.EventType, d *events.ToolCallEventData) {
	switch t {
	case events.EventToolCallStarted:
		l.logf("tool", "%s called", d.ToolName)
	case events.EventToolCallCompleted:
		result := "returned text"
		if mt := imageMIME(d.Parts); mt != "" {
			result = "returned an image; image sent to the model (" + mt + ")"
		}
		l.logf("tool", "%s %s", d.ToolName, result)
	case events.EventToolCallFailed:
		l.logf("tool", "%s failed: %v", d.ToolName, d.Error)
	}
}

// onCost prints what a provider call cost and the session's running total.
// usage is a cost reported after its call completed (provider.call.usage);
// callID names the call, so late usage is labeled with the call it belongs
// to, whatever completed since.
func (l *eventLog) onCost(cost float64, in, out int, callID string, usage bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if usage {
		line := lateUsage{cost: cost, in: in, out: out}
		if _, known := l.callNums[callID]; callID != "" && !known {
			// The bus delivers each event type on its own goroutine, so a
			// call's usage can arrive before its provider.call.completed: it
			// is printed after the call's line.
			if l.pendingUsage == nil {
				l.pendingUsage = map[string][]lateUsage{}
			}
			if _, queued := l.pendingUsage[callID]; !queued {
				l.pendingOrder = append(l.pendingOrder, callID)
			}
			l.pendingUsage[callID] = append(l.pendingUsage[callID], line)
			return
		}
		l.printLateUsageLocked(callID, line)
		return
	}
	l.total += cost
	l.calls++
	if callID != "" {
		if l.callNums == nil {
			l.callNums = map[string]int{}
		}
		l.callNums[callID] = l.calls
	}
	l.logLockedf("cost", "call %d: $%.4f (%d in / %d out tokens); session total $%.4f",
		l.calls, cost, in, out, l.total)
	for _, line := range l.pendingUsage[callID] {
		l.printLateUsageLocked(callID, line)
	}
	delete(l.pendingUsage, callID)
}

// lateUsage is a provider.call.usage the log has not printed yet.
type lateUsage struct {
	cost    float64
	in, out int
}

// printLateUsageLocked prints usage reported for callID's call after it
// completed: "the session" when it names no call the log has seen complete.
// Called with l.mu held.
func (l *eventLog) printLateUsageLocked(callID string, u lateUsage) {
	l.total += u.cost
	call := "the session"
	if n, ok := l.callNums[callID]; ok && callID != "" {
		call = fmt.Sprintf("call %d", n)
	}
	l.logLockedf("cost", "late usage for %s: +$%.4f (%d in / %d out tokens); session total $%.4f",
		call, u.cost, u.in, u.out, l.total)
}

// observe prints what the caller is told on Response(): a chunk with
// Interrupted set means stop playing the current reply (barge-in, or a
// guardrail cutting it). When a guardrail cut the reply, its message follows
// the Interrupted chunk as text, and the turn ends with finish reason safety.
func (l *eventLog) observe(c providers.StreamChunk) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if c.Error != nil {
		l.logLockedf("error", "%v", c.Error)
	}
	if c.Interrupted {
		l.logLockedf("interrupt", "Interrupted: stop playing the current reply")
		l.cut = true
		l.cutText.Reset()
	}
	if l.cut {
		l.cutText.WriteString(c.Delta)
	}
	// Audio marked as a cue's is the guardrail's message, spoken by the
	// pipeline (a TTS service, or a tone), not by the model.
	kind, _ := c.Metadata["cue"].(string)
	if kind != "" && !l.cuePlaying {
		l.logLockedf("cue", "the guardrail's message plays as audio (%s), not spoken by the model", kind)
	}
	l.cuePlaying = kind != ""
	// In VAD mode the TTS service speaks each reply once it is complete; the
	// chunk names the words it spoke.
	if spoken, _ := c.Metadata["spoken_text"].(string); spoken != "" && kind == "" && c.MediaData != nil {
		l.logLockedf("speech", "TTS speaks %q (%d ms of audio)",
			shorten(spoken), pcmDuration(len(c.MediaData.Data), c.MediaData.SampleRate).Milliseconds())
	}
	if done, _ := c.Metadata["assistant_turn_complete"].(bool); !done {
		return
	}
	if fr, _ := c.Metadata["finish_reason"].(string); fr == types.FinishReasonSafety && l.cut {
		l.logLockedf("interrupt", "turn ended (safety); text after the cut: %q", shorten(l.cutText.String()))
	}
	l.cut = false
	l.cutText.Reset()
}

// end waits for pending reads of the store and prints the session's end.
// Call it after closing the conversation and its event bus.
func (l *eventLog) end() {
	l.endedOnce.Do(func() { close(l.ended) })
	l.wg.Wait()
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, callID := range l.pendingOrder { // usage for a call never seen to complete
		for _, line := range l.pendingUsage[callID] { // none when its call completed
			l.printLateUsageLocked(callID, line)
		}
	}
	l.pendingUsage, l.pendingOrder = nil, nil
	what := ""
	if l.speechCosts {
		what = " (with STT and TTS)"
	}
	l.logLockedf("session", "end: %d provider calls, total cost%s $%.4f", l.calls, what, l.total)
}

// shorten cuts text to maxShownText characters.
func shorten(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > maxShownText {
		return string(r[:maxShownText]) + "..."
	}
	return text
}

// hasImage reports whether parts hold an image.
func hasImage(parts []types.ContentPart) bool { return imageMIME(parts) != "" }

// imageMIME returns the MIME type of the first image in parts, or "".
func imageMIME(parts []types.ContentPart) string {
	for _, p := range parts {
		if p.Type == types.ContentTypeImage && p.Media != nil {
			return p.Media.MIMEType
		}
	}
	return ""
}
