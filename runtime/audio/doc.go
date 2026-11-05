// Package audio provides voice activity detection (VAD), turn detection,
// and audio session management for real-time voice AI applications.
//
// The package follows industry-standard patterns for voice AI:
//   - VAD (Voice Activity Detection): Detects when someone is speaking vs. silent
//   - Turn Detection: Determines when a speaker has finished their turn
//   - Interruption Handling: Manages user interrupting bot output
//   - G.711 Codec: mu-law/a-law companding <-> PCM16 (telephony/SIP/PSTN)
//
// # Architecture
//
// Audio processing follows a two-stage approach:
//
//  1. VADAnalyzer detects voice activity in real-time
//  2. TurnDetector uses VAD output plus additional signals to detect turn boundaries
//
// # Usage Example
//
//	vad, err := audio.NewAdaptiveVAD(audio.DefaultVADParams())
//	if err != nil {
//	    return err
//	}
//	detector := audio.NewSilenceDetector(500 * time.Millisecond)
//
//	for chunk := range audioStream {
//	    if _, err := vad.Analyze(ctx, chunk); err != nil {
//	        return err
//	    }
//	    _, _ = detector.ProcessAudio(ctx, chunk)
//	    if ended, _ := detector.ProcessVADState(ctx, vad.State()); ended {
//	        // User finished speaking
//	    }
//	}
package audio
