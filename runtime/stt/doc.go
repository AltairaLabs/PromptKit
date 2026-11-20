// Package stt provides speech-to-text services for converting audio to text.
//
// The package defines a Service interface that extends base.STTProvider,
// enabling voice AI applications to transcribe speech from users.
//
// # Architecture
//
// The package provides:
//   - Service interface (extends base.STTProvider) for STT providers
//   - TranscriptionConfig for audio format configuration
//   - Provider implementations for OpenAI, ElevenLabs and Cartesia
//
// # Usage
//
// Basic usage with OpenAI Whisper via base.STTProvider:
//
//	service := stt.NewOpenAI(os.Getenv("OPENAI_API_KEY"))
//	resp, err := service.Transcribe(ctx, base.STTRequest{
//	    Audio:    audioData,
//	    MIMEType: "audio/pcm",
//	    Hints:    map[string]string{"sample_rate": "16000", "language": "en"},
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println("User said:", resp.Text)
//
// # Available Providers
//
// The package includes implementations for:
//   - OpenAI transcription (NewOpenAI, whisper-1 by default)
//   - ElevenLabs Scribe (NewElevenLabs, scribe_v3 by default)
//   - Cartesia Ink (NewCartesia, ink-whisper by default)
//   - More providers can be added following the Service interface
package stt
