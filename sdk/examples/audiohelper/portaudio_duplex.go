package audiohelper

// portaudio_duplex.go holds the hardware-free duplex logic: the single
// synchronized read/write loop body and the duplex-mode Play/Flush paths. The
// loop calls duplexRead/duplexWrite (defined in portaudio_io_interactive.go),
// which dispatch to injectable readFn/writeFn fakes in tests and to PortAudio's
// blocking API on the real path — so everything here is unit-testable without a
// device and counts toward the per-file coverage threshold.

import (
	"context"
	"encoding/binary"
	"log/slog"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
)

// duplexBlockFrames is the per-tick mic/speaker block size: 480 samples = 10 ms
// at the 48 kHz duplex rate.
const duplexBlockFrames = audio.DuplexRate / 100

// duplexLoop is the single synchronized audio loop for duplex mode. Each tick:
//  1. read one 480-sample mic block @48 kHz (duplexRead),
//  2. resample 48 kHz → captureRate and emit it on captureCh (drop on
//     backpressure, matching the two-stream observer cadence),
//  3. pull one 480-sample playback block from the jitter buffer (always exactly
//     480, silence-filled on underrun) and write it to the device (duplexWrite).
//
// PTS stamping is intentionally left to portaudioSource.pump (its captureRate
// sample clock already yields correct wall-time), so the mic frames carry the
// same PTS contract as the two-stream path. The loop returns when ctx is
// canceled, p.done is closed, or a read/write returns a non-zero PortAudio rc.
func (p *portaudioIO) duplexLoop(ctx context.Context) {
	defer p.wg.Done()
	micBuf := make([]int16, duplexBlockFrames)
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.done:
			return
		default:
		}

		if rc := p.duplexRead(micBuf); rc != 0 {
			return
		}

		// Mic seam: 48 kHz int16 → bytes → resample to captureRate → captureCh.
		// AEC NOTE (#1506): when Speex AEC lands, the cancellation tap
		// `near = Cancel(micBuf, lastWrittenBlock)` must run on the RAW 48 kHz mic
		// block BELOW, BEFORE this 48→captureRate resample/emit. The mic emit then
		// moves under the AEC step and this loop must retain the last written
		// playback block (the speaker-seam `out` below) as the far-end reference.
		if resampled, err := audio.ResamplePCM16(int16ToBytes(micBuf), audio.DuplexRate, p.captureRate); err == nil {
			select {
			case p.captureCh <- resampled:
			default: // drop on backpressure — observer cadence
			}
		}

		// Speaker seam: pull a fixed 480-sample block (silence on underrun) and write it.
		out := p.jitter.Pull(duplexBlockFrames)
		if rc := p.duplexWrite(out); rc != 0 {
			return
		}
	}
}

// duplexPlay resamples a playback-rate frame up to audio.DuplexRate and pushes the
// result into the jitter buffer. The device loop drains the buffer one 10 ms
// block per tick. Frames are silently dropped only if resampling fails.
//
// The jitter buffer is sized for ~200 ms of timing jitter, NOT for whole
// utterances (see newDuplexCore's cadence contract). If a Push overflows it,
// the oldest audio is dropped; the first such overflow in a session logs a
// single warning so the silent loss is visible to a writer that isn't pacing.
func (p *portaudioIO) duplexPlay(frame []byte) {
	resampled, err := audio.ResamplePCM16(frame, p.playbackRate, audio.DuplexRate)
	if err != nil {
		return
	}
	p.jitter.Push(bytesToInt16(resampled))
	if p.jitter.Drops() > 0 {
		p.jitterOverflowOnce.Do(func() {
			slog.Warn("playback jitter buffer overflowing — audio being dropped; " +
				"the writer is exceeding real-time cadence (missing a pacing/chunking step?)")
		})
	}
}

// Play enqueues a PCM16 frame for speaker playback. In duplex mode it resamples
// (playbackRate → 48 kHz) and pushes to the jitter buffer; in two-stream mode it
// drops the frame onto playCh, dropping on backpressure.
func (p *portaudioIO) Play(frame []byte) {
	if p.duplex.Load() {
		p.duplexPlay(frame)
		return
	}
	select {
	case p.playCh <- frame:
	default:
	}
}

// Flush cuts playback immediately. In duplex mode it clears the jitter buffer so
// the loop writes silence on the next tick (no stream stop/start needed). In
// two-stream mode it drains playCh and restarts the output stream.
func (p *portaudioIO) Flush() {
	if p.duplex.Load() {
		p.jitter.Clear()
		return
	}
	p.requestFlush()
}

// int16ToBytes encodes PCM16 samples as little-endian bytes.
func int16ToBytes(samples []int16) []byte {
	out := make([]byte, len(samples)*bytesPerSample)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(out[i*bytesPerSample:], uint16(s))
	}
	return out
}

// bytesToInt16 decodes little-endian PCM16 bytes into samples. A trailing odd
// byte (incomplete sample) is ignored.
func bytesToInt16(data []byte) []int16 {
	n := len(data) / bytesPerSample
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		out[i] = int16(binary.LittleEndian.Uint16(data[i*bytesPerSample:]))
	}
	return out
}
