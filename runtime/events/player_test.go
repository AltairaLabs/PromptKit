package events

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestSession(t *testing.T, eventCount int, interval time.Duration) (*FileEventStore, string) {
	t.Helper()
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)

	sessionID := "test-session"
	baseTime := time.Now()

	for i := 0; i < eventCount; i++ {
		event := &Event{
			Type:      EventMessageCreated,
			Timestamp: baseTime.Add(time.Duration(i) * interval),
			SessionID: sessionID,
			Data: &MessageCreatedData{
				Role:    "user",
				Content: "Message " + string(rune('A'+i)),
			},
		}
		require.NoError(t, store.Append(context.Background(), event))
	}
	require.NoError(t, store.Sync())

	return store, sessionID
}

func TestNewSessionPlayer(t *testing.T) {
	store, sessionID := createTestSession(t, 5, 100*time.Millisecond)
	defer store.Close()

	t.Run("creates player with default config", func(t *testing.T) {
		player := NewSessionPlayer(store, sessionID, nil)
		assert.NotNil(t, player)
		assert.Equal(t, PlayerStateStopped, player.State())
		assert.Equal(t, 1.0, player.config.Speed)
	})

	t.Run("creates player with custom config", func(t *testing.T) {
		config := &PlayerConfig{Speed: 2.0}
		player := NewSessionPlayer(store, sessionID, config)
		assert.Equal(t, 2.0, player.config.Speed)
	})

	t.Run("normalizes invalid speed", func(t *testing.T) {
		config := &PlayerConfig{Speed: -1.0}
		player := NewSessionPlayer(store, sessionID, config)
		assert.Equal(t, 1.0, player.config.Speed)
	})
}

func TestSessionPlayer_Load(t *testing.T) {
	store, sessionID := createTestSession(t, 5, 100*time.Millisecond)
	defer store.Close()

	player := NewSessionPlayer(store, sessionID, nil)
	err := player.Load(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 5, player.EventCount())
	assert.Len(t, player.Events(), 5)
}

func TestSessionPlayer_Duration(t *testing.T) {
	store, sessionID := createTestSession(t, 5, 100*time.Millisecond)
	defer store.Close()

	player := NewSessionPlayer(store, sessionID, nil)
	require.NoError(t, player.Load(context.Background()))

	// 5 events at 100ms intervals = 400ms total (first to last)
	duration := player.Duration()
	assert.InDelta(t, 400*time.Millisecond, duration, float64(10*time.Millisecond))
}

func TestSessionPlayer_PlayWithSkipTiming(t *testing.T) {
	store, sessionID := createTestSession(t, 5, 100*time.Millisecond)
	defer store.Close()

	var receivedEvents []*Event
	var mu sync.Mutex

	config := &PlayerConfig{
		SkipTiming: true,
		OnEvent: func(event *Event, position time.Duration) bool {
			mu.Lock()
			receivedEvents = append(receivedEvents, event)
			mu.Unlock()
			return true
		},
	}

	player := NewSessionPlayer(store, sessionID, config)
	require.NoError(t, player.Load(context.Background()))

	player.Play(context.Background())
	player.Wait()

	mu.Lock()
	assert.Len(t, receivedEvents, 5)
	mu.Unlock()
	assert.Equal(t, PlayerStateStopped, player.State())
}

func TestSessionPlayer_PlayRealTime(t *testing.T) {
	// Use short intervals for fast tests
	store, sessionID := createTestSession(t, 3, 50*time.Millisecond)
	defer store.Close()

	var receivedTimes []time.Time
	var mu sync.Mutex

	config := &PlayerConfig{
		Speed: 1.0,
		OnEvent: func(event *Event, position time.Duration) bool {
			mu.Lock()
			receivedTimes = append(receivedTimes, time.Now())
			mu.Unlock()
			return true
		},
	}

	player := NewSessionPlayer(store, sessionID, config)
	require.NoError(t, player.Load(context.Background()))

	start := time.Now()
	player.Play(context.Background())
	player.Wait()
	elapsed := time.Since(start)

	// Should take approximately 100ms (2 intervals at 50ms each)
	assert.InDelta(t, 100*time.Millisecond, elapsed, float64(50*time.Millisecond))
}

func TestSessionPlayer_PlayFastForward(t *testing.T) {
	store, sessionID := createTestSession(t, 3, 100*time.Millisecond)
	defer store.Close()

	config := &PlayerConfig{
		Speed: 10.0, // 10x speed
		OnEvent: func(event *Event, position time.Duration) bool {
			return true
		},
	}

	player := NewSessionPlayer(store, sessionID, config)
	require.NoError(t, player.Load(context.Background()))

	start := time.Now()
	player.Play(context.Background())
	player.Wait()
	elapsed := time.Since(start)

	// At 10x speed, 200ms of events should take ~20ms
	assert.Less(t, elapsed, 100*time.Millisecond)
}

func TestSessionPlayer_PauseResume(t *testing.T) {
	store, sessionID := createTestSession(t, 10, 50*time.Millisecond)
	defer store.Close()

	var eventCount int
	var mu sync.Mutex

	config := &PlayerConfig{
		Speed: 1.0,
		OnEvent: func(event *Event, position time.Duration) bool {
			mu.Lock()
			eventCount++
			mu.Unlock()
			return true
		},
	}

	player := NewSessionPlayer(store, sessionID, config)
	require.NoError(t, player.Load(context.Background()))

	// Start playing
	player.Play(context.Background())

	// Wait a bit then pause
	time.Sleep(100 * time.Millisecond)
	player.Pause()

	mu.Lock()
	countAtPause := eventCount
	mu.Unlock()

	assert.Equal(t, PlayerStatePaused, player.State())
	assert.Greater(t, countAtPause, 0)
	assert.Less(t, countAtPause, 10)

	// Wait and verify no more events
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	assert.Equal(t, countAtPause, eventCount)
	mu.Unlock()

	// Resume
	player.Play(context.Background())
	player.Wait()

	mu.Lock()
	assert.Equal(t, 10, eventCount)
	mu.Unlock()
}

func TestSessionPlayer_Stop(t *testing.T) {
	store, sessionID := createTestSession(t, 10, 50*time.Millisecond)
	defer store.Close()

	var eventCount int
	var mu sync.Mutex

	config := &PlayerConfig{
		Speed: 1.0,
		OnEvent: func(event *Event, position time.Duration) bool {
			mu.Lock()
			eventCount++
			mu.Unlock()
			return true
		},
	}

	player := NewSessionPlayer(store, sessionID, config)
	require.NoError(t, player.Load(context.Background()))

	player.Play(context.Background())
	time.Sleep(100 * time.Millisecond)
	player.Stop()

	mu.Lock()
	countAtStop := eventCount
	mu.Unlock()

	assert.Equal(t, PlayerStateStopped, player.State())
	assert.Equal(t, 0, player.Position()) // Position should reset

	// Verify no more events
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	assert.Equal(t, countAtStop, eventCount)
	mu.Unlock()
}

func TestSessionPlayer_Seek(t *testing.T) {
	store, sessionID := createTestSession(t, 10, 100*time.Millisecond)
	defer store.Close()

	player := NewSessionPlayer(store, sessionID, nil)
	require.NoError(t, player.Load(context.Background()))

	t.Run("seek to middle", func(t *testing.T) {
		player.Seek(450 * time.Millisecond) // Between event 4 and 5
		assert.Equal(t, 5, player.Position())
	})

	t.Run("seek to start", func(t *testing.T) {
		player.Seek(0)
		assert.Equal(t, 0, player.Position())
	})

	t.Run("seek past end", func(t *testing.T) {
		player.Seek(10 * time.Second)
		assert.Equal(t, 10, player.Position())
	})
}

func TestSessionPlayer_SeekToEvent(t *testing.T) {
	store, sessionID := createTestSession(t, 10, 100*time.Millisecond)
	defer store.Close()

	player := NewSessionPlayer(store, sessionID, nil)
	require.NoError(t, player.Load(context.Background()))

	t.Run("seek to valid index", func(t *testing.T) {
		player.SeekToEvent(5)
		assert.Equal(t, 5, player.Position())
	})

	t.Run("seek to negative index", func(t *testing.T) {
		player.SeekToEvent(-1)
		assert.Equal(t, 0, player.Position())
	})

	t.Run("seek past end", func(t *testing.T) {
		player.SeekToEvent(100)
		assert.Equal(t, 10, player.Position())
	})
}

func TestSessionPlayer_SetSpeed(t *testing.T) {
	store, sessionID := createTestSession(t, 5, 100*time.Millisecond)
	defer store.Close()

	player := NewSessionPlayer(store, sessionID, nil)
	require.NoError(t, player.Load(context.Background()))

	player.SetSpeed(2.0)
	assert.Equal(t, 2.0, player.config.Speed)

	// Invalid speed should normalize to 1.0
	player.SetSpeed(0)
	assert.Equal(t, 1.0, player.config.Speed)

	player.SetSpeed(-5)
	assert.Equal(t, 1.0, player.config.Speed)
}

func TestSessionPlayer_OnComplete(t *testing.T) {
	store, sessionID := createTestSession(t, 3, 10*time.Millisecond)
	defer store.Close()

	var completed bool
	var mu sync.Mutex

	config := &PlayerConfig{
		SkipTiming: true,
		OnComplete: func() {
			mu.Lock()
			completed = true
			mu.Unlock()
		},
	}

	player := NewSessionPlayer(store, sessionID, config)
	require.NoError(t, player.Load(context.Background()))

	player.Play(context.Background())
	player.Wait()

	mu.Lock()
	assert.True(t, completed)
	mu.Unlock()
}

func TestSessionPlayer_OnStateChange(t *testing.T) {
	store, sessionID := createTestSession(t, 3, 10*time.Millisecond)
	defer store.Close()

	var states []PlayerState
	var mu sync.Mutex

	config := &PlayerConfig{
		SkipTiming: true,
		OnStateChange: func(state PlayerState) {
			mu.Lock()
			states = append(states, state)
			mu.Unlock()
		},
	}

	player := NewSessionPlayer(store, sessionID, config)
	require.NoError(t, player.Load(context.Background()))

	player.Play(context.Background())
	player.Wait()

	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, states, PlayerStatePlaying)
	assert.Contains(t, states, PlayerStateStopped)
}

func TestSessionPlayer_CallbackStopsPlayback(t *testing.T) {
	store, sessionID := createTestSession(t, 10, 10*time.Millisecond)
	defer store.Close()

	var eventCount int
	var mu sync.Mutex

	config := &PlayerConfig{
		SkipTiming: true,
		OnEvent: func(event *Event, position time.Duration) bool {
			mu.Lock()
			eventCount++
			count := eventCount
			mu.Unlock()
			return count < 5 // Stop after 5 events
		},
	}

	player := NewSessionPlayer(store, sessionID, config)
	require.NoError(t, player.Load(context.Background()))

	player.Play(context.Background())
	player.Wait()

	mu.Lock()
	assert.Equal(t, 5, eventCount)
	mu.Unlock()
}

func TestSessionPlayer_EmptySession(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	player := NewSessionPlayer(store, "empty-session", nil)
	require.NoError(t, player.Load(context.Background()))

	assert.Equal(t, 0, player.EventCount())
	assert.Equal(t, time.Duration(0), player.Duration())

	// Play should handle empty gracefully
	player.Play(context.Background())
	assert.Equal(t, PlayerStateStopped, player.State())
}

func TestSessionPlayer_ContextCancellation(t *testing.T) {
	store, sessionID := createTestSession(t, 100, 50*time.Millisecond)
	defer store.Close()

	var eventCount int
	var mu sync.Mutex

	config := &PlayerConfig{
		Speed: 1.0,
		OnEvent: func(event *Event, position time.Duration) bool {
			mu.Lock()
			eventCount++
			mu.Unlock()
			return true
		},
	}

	player := NewSessionPlayer(store, sessionID, config)
	require.NoError(t, player.Load(context.Background()))

	ctx, cancel := context.WithCancel(context.Background())
	player.Play(ctx)

	time.Sleep(100 * time.Millisecond)
	cancel()

	// Wait for playback to stop
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	assert.Less(t, eventCount, 100)
	mu.Unlock()
}

func TestSessionPlayer_CurrentTime(t *testing.T) {
	store, sessionID := createTestSession(t, 5, 100*time.Millisecond)
	defer store.Close()

	player := NewSessionPlayer(store, sessionID, nil)
	require.NoError(t, player.Load(context.Background()))

	// At start
	assert.Equal(t, time.Duration(0), player.CurrentTime())

	// Seek to middle
	player.SeekToEvent(3)
	assert.InDelta(t, 300*time.Millisecond, player.CurrentTime(), float64(10*time.Millisecond))

	// At end
	player.SeekToEvent(5)
	assert.Equal(t, player.Duration(), player.CurrentTime())
}
