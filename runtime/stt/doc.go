// Package stt provides speech-to-text services for converting audio to text.
//
// The package defines a common Service interface that abstracts STT providers,
// enabling voice AI applications to transcribe speech from users.
//
// # Architecture
//
// The package provides:
//   - Service interface for STT providers
//   - TranscriptionConfig for audio format configuration
//   - Multiple provider implementations (OpenAI Whisper, etc.)
//
// # Usage
//
// Basic usage with OpenAI Whisper:
//
//	service := stt.NewOpenAI(os.Getenv("OPENAI_API_KEY"))
//	text, err := service.Transcribe(ctx, audioData, stt.TranscriptionConfig{
//	    Format:     "pcm",
//	    SampleRate: 16000,
//	    Channels:   1,
//	    Language:   "en",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println("User said:", text)
//
// # Available Providers
//
// The package includes implementations for:
//   - OpenAI Whisper (whisper-1 model)
//   - More providers can be added following the Service interface
package stt
