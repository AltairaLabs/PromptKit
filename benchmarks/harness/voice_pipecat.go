package main

import (
	"context"
	"encoding/binary"
	"math"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"

	pb "github.com/AltairaLabs/PromptKit/benchmarks/harness/pipecatpb"
)

func generateSineWavePCM(size int) []byte {
	buf := make([]byte, size)
	samples := size / 2
	for i := 0; i < samples; i++ {
		val := int16(16000 * math.Sin(2*math.Pi*440*float64(i)/16000))
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(val))
	}
	return buf
}

// PipecatVoiceConfig holds parameters for a Pipecat voice benchmark.
type PipecatVoiceConfig struct {
	TargetURL      string
	Concurrency    int
	Sessions       int
	AudioFrames    int
	FrameSize      int
	FrameInterval  time.Duration
	SessionTimeout time.Duration
	SampleRate     uint32
	NumChannels    uint32
}

// RunPipecatVoiceBenchmark drives concurrent Pipecat voice sessions.
func RunPipecatVoiceBenchmark(ctx context.Context, cfg PipecatVoiceConfig) (*Aggregator, error) {
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
				agg.Record(doPipecatSession(ctx, cfg))
			}
		}()
	}
	wg.Wait()
	return agg, nil
}

// doPipecatSession uses the exact same simple read loop pattern as the
// working raw Go test. No goroutines, no SetReadDeadline gymnastics.
// Sends audio first, then reads all response frames sequentially.
func doPipecatSession(_ context.Context, cfg PipecatVoiceConfig) RequestResult {
	start := time.Now()

	conn, _, err := websocket.DefaultDialer.Dial(cfg.TargetURL, nil)
	if err != nil {
		return RequestResult{Error: err, TotalDuration: time.Since(start)}
	}
	defer conn.Close()

	// Set an overall deadline for the entire session.
	conn.SetReadDeadline(time.Now().Add(cfg.SessionTimeout))  //nolint:errcheck
	conn.SetWriteDeadline(time.Now().Add(cfg.SessionTimeout)) //nolint:errcheck

	// Send audio frames at realtime pace.
	pcmData := generateSineWavePCM(cfg.FrameSize)
	for i := 0; i < cfg.AudioFrames; i++ {
		frame := &pb.Frame{
			Frame: &pb.Frame_Audio{
				Audio: &pb.AudioRawFrame{
					Audio:       pcmData,
					SampleRate:  cfg.SampleRate,
					NumChannels: cfg.NumChannels,
				},
			},
		}
		data, _ := proto.Marshal(frame)
		if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
			return RequestResult{Error: err, TotalDuration: time.Since(start)}
		}
		time.Sleep(cfg.FrameInterval)
	}

	// Now read all response frames — same simple loop as the working raw test.
	var (
		firstByteAt    time.Time
		lastChunkAt    time.Time
		chunkCount     int
		chunkIntervals []time.Duration
	)

	for i := 0; i < 500; i++ { // cap at 500 frames to avoid infinite loop
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var frame pb.Frame
		if proto.Unmarshal(data, &frame) != nil {
			continue
		}

		af, ok := frame.Frame.(*pb.Frame_Audio)
		if !ok || len(af.Audio.Audio) == 0 {
			continue // skip RTVI messages, text, etc.
		}

		now := time.Now()
		if chunkCount == 0 {
			firstByteAt = now
		} else {
			chunkIntervals = append(chunkIntervals, now.Sub(lastChunkAt))
		}
		lastChunkAt = now
		chunkCount++
	}

	if chunkCount == 0 {
		return RequestResult{TotalDuration: time.Since(start)}
	}

	return RequestResult{
		FirstByteLatency: firstByteAt.Sub(start),
		TotalDuration:    time.Since(start),
		ChunkCount:       chunkCount,
		ChunkIntervals:   chunkIntervals,
	}
}
