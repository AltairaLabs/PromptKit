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
//   - Multiple provider implementations (OpenAI Whisper, etc.)
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
//   - OpenAI Whisper (whisper-1 model)
//   - More providers can be added following the Service interface
package stt
