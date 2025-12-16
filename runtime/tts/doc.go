// Package tts provides text-to-speech services for converting text responses to audio.
//
// The package defines a common Service interface that abstracts TTS providers,
// enabling voice AI applications to convert text-only LLM responses to speech.
//
// # Architecture
//
// The package provides:
//   - Service interface for TTS providers
//   - SynthesisConfig for voice/format configuration
//   - Voice and AudioFormat types for provider capabilities
//   - Multiple provider implementations (OpenAI, ElevenLabs, etc.)
//
// # Usage
//
// Basic usage with OpenAI TTS:
//
//	service := tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))
//	reader, err := service.Synthesize(ctx, "Hello world", tts.SynthesisConfig{
//	    Voice:  "alloy",
//	    Format: tts.FormatMP3,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer reader.Close()
//
//	// Stream audio to speaker or save to file
//	io.Copy(audioOutput, reader)
//
// # Streaming TTS
//
// For low-latency applications, use StreamingService:
//
//	streamer := tts.NewCartesia(os.Getenv("CARTESIA_API_KEY"))
//	chunks, err := streamer.SynthesizeStream(ctx, "Hello world", config)
//	for chunk := range chunks {
//	    // Play audio chunk immediately
//	    speaker.Write(chunk)
//	}
//
// # Available Providers
//
// The package includes implementations for:
//   - OpenAI TTS (tts-1, tts-1-hd models)
//   - ElevenLabs (high-quality voice cloning)
//   - Cartesia (ultra-low latency streaming)
//   - Google Cloud Text-to-Speech (multi-language)
package tts
