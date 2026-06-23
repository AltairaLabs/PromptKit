package audio

import (
	"sync"
	"time"
)

// vadStateMachine is a shared VAD state machine that can be embedded by any
// VAD analyzer implementation. It handles all state transitions, event emission,
// and thread-safe state access, so individual VAD implementations only need to
// supply a probability value.
type vadStateMachine struct {
	params VADParams

	mu          sync.RWMutex
	state       VADState
	prevState   VADState
	stateChange chan VADEvent
	stateStart  time.Time
}

// newVADStateMachine creates a vadStateMachine with the given params.
func newVADStateMachine(params VADParams) *vadStateMachine {
	return &vadStateMachine{
		params:      params,
		state:       VADStateQuiet,
		stateChange: make(chan VADEvent, stateChangeBufferSize),
		stateStart:  time.Now(),
	}
}

// update advances the state machine with the given voice probability and emits
// a VADEvent on state change. The send is non-blocking; events are dropped when
// the channel is full.
func (m *vadStateMachine) update(probability float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	stateDuration := now.Sub(m.stateStart)

	newState := m.computeNextState(m.state, probability, stateDuration.Seconds())

	if newState != m.state {
		event := VADEvent{
			State:      newState,
			PrevState:  m.state,
			Timestamp:  now,
			Duration:   stateDuration,
			Confidence: probability,
		}

		m.prevState = m.state
		m.state = newState
		m.stateStart = now

		// Non-blocking send — drop the event if the channel is full.
		select {
		case m.stateChange <- event:
		default:
		}
	}
}

// computeNextState is a pure function that determines the next VAD state given
// the current state, voice probability, and how long the VAD has been in the
// current state.
func (m *vadStateMachine) computeNextState(
	current VADState, probability float64, stateDurationSecs float64,
) VADState {
	aboveThreshold := probability >= m.params.Confidence

	switch current {
	case VADStateQuiet:
		if aboveThreshold {
			return VADStateStarting
		}
	case VADStateStarting:
		if !aboveThreshold {
			return VADStateQuiet
		}
		if stateDurationSecs >= m.params.StartSecs {
			return VADStateSpeaking
		}
	case VADStateSpeaking:
		if !aboveThreshold {
			return VADStateStopping
		}
	case VADStateStopping:
		if aboveThreshold {
			return VADStateSpeaking
		}
		if stateDurationSecs >= m.params.StopSecs {
			return VADStateQuiet
		}
	}
	return current
}

// State returns the current VAD state.
func (m *vadStateMachine) State() VADState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// OnStateChange returns a channel that receives VADEvent values on each state
// transition. The channel is buffered; events are dropped when it is full.
func (m *vadStateMachine) OnStateChange() <-chan VADEvent {
	return m.stateChange
}

// Reset clears accumulated state and drains the event channel.
func (m *vadStateMachine) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.state = VADStateQuiet
	m.prevState = VADStateQuiet
	m.stateStart = time.Now()

	for len(m.stateChange) > 0 {
		<-m.stateChange
	}
}
