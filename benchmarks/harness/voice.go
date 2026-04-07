package main

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// VoiceConfig holds parameters for a voice pipeline benchmark run.
type VoiceConfig struct {
	TargetURL      string
	Concurrency    int
	Sessions       int
	AudioFrames    int
	FrameSize      int           // 640 for 16kHz 16-bit 20ms
	FrameInterval  time.Duration // 20ms for realtime
	SessionTimeout time.Duration
}

// RunVoiceBenchmark runs a voice pipeline benchmark and returns the aggregated results.
func RunVoiceBenchmark(ctx context.Context, cfg VoiceConfig) (*Aggregator, error) {
	work := make(chan struct{}, cfg.Sessions)
	for i := 0; i < cfg.Sessions; i++ {
		work <- struct{}{}
	}
	close(work)

	agg := &Aggregator{}
	var wg sync.WaitGroup

	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range work {
				result := doVoiceSession(ctx, cfg)
				agg.Record(result)
			}
		}()
	}

	wg.Wait()
	return agg, nil
}

// doVoiceSession dials a WebSocket, streams audio frames, and measures latency.
func doVoiceSession(ctx context.Context, cfg VoiceConfig) RequestResult {
	sessionCtx, cancel := context.WithTimeout(ctx, cfg.SessionTimeout)
	defer cancel()

	start := time.Now()

	dialer := websocket.Dialer{}
	conn, _, err := dialer.DialContext(sessionCtx, cfg.TargetURL, nil)
	if err != nil {
		return RequestResult{Error: err, TotalDuration: time.Since(start)}
	}
	defer conn.Close()

	// Set deadline based on session timeout.
	if deadline, ok := sessionCtx.Deadline(); ok {
		conn.SetReadDeadline(deadline)  //nolint:errcheck
		conn.SetWriteDeadline(deadline) //nolint:errcheck
	}

	// Send audio frames at the configured interval.
	frame := make([]byte, cfg.FrameSize)
	ticker := time.NewTicker(cfg.FrameInterval)
	defer ticker.Stop()

	for i := 0; i < cfg.AudioFrames; i++ {
		select {
		case <-sessionCtx.Done():
			return RequestResult{Error: sessionCtx.Err(), TotalDuration: time.Since(start)}
		case <-ticker.C:
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			return RequestResult{Error: err, TotalDuration: time.Since(start)}
		}
	}

	// Signal end of audio.
	endMsg, _ := json.Marshal(map[string]string{"type": "end_audio"})
	if err := conn.WriteMessage(websocket.TextMessage, endMsg); err != nil {
		return RequestResult{Error: err, TotalDuration: time.Since(start)}
	}
	audioEndTime := time.Now()

	// Read responses until done signal.
	var firstByteLatency time.Duration
	var firstChunk bool
	var chunkIntervals []time.Duration
	var lastChunkTime time.Time
	chunkCount := 0

	for {
		select {
		case <-sessionCtx.Done():
			return RequestResult{Error: sessionCtx.Err(), TotalDuration: time.Since(start)}
		default:
		}

		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			return RequestResult{Error: err, TotalDuration: time.Since(start)}
		}

		now := time.Now()

		switch msgType {
		case websocket.BinaryMessage:
			// Audio chunk received.
			if !firstChunk {
				firstByteLatency = now.Sub(audioEndTime)
				firstChunk = true
				lastChunkTime = now
			} else {
				chunkIntervals = append(chunkIntervals, now.Sub(lastChunkTime))
				lastChunkTime = now
			}
			chunkCount++

		case websocket.TextMessage:
			// Check for done signal.
			var sig map[string]string
			if err := json.Unmarshal(msg, &sig); err == nil && sig["type"] == "done" {
				return RequestResult{
					FirstByteLatency: firstByteLatency,
					TotalDuration:    time.Since(start),
					ChunkCount:       chunkCount,
					ChunkIntervals:   chunkIntervals,
				}
			}
		}
	}
}
