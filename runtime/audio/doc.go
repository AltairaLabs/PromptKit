// Package audio provides voice activity detection (VAD), turn detection,
// and audio session management for real-time voice AI applications.
//
// The package follows industry-standard patterns for voice AI:
//   - VAD (Voice Activity Detection): Detects when someone is speaking vs. silent
//   - Turn Detection: Determines when a speaker has finished their turn
//   - Interruption Handling: Manages user interrupting bot output
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
//	vad := audio.NewSimpleVAD(audio.DefaultVADParams())
//	detector := audio.NewSilenceDetector(500 * time.Millisecond)
//
//	for chunk := range audioStream {
//	    vad.Analyze(ctx, chunk)
//	    if detector.DetectTurnEnd(ctx, vad) {
//	        // User finished speaking
//	    }
//	}
package audio
