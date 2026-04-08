package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	profilePath := flag.String("profile", "", "path to latency profile YAML (default: built-in defaults)")
	openaiPort := flag.Int("openai-port", 8081, "port for OpenAI SSE server")
	sttPort := flag.Int("stt-port", 8082, "port for STT WebSocket server")
	ttsPort := flag.Int("tts-port", 8083, "port for TTS WebSocket server")
	flag.Parse()

	var profile Profile
	if *profilePath != "" {
		var err error
		profile, err = LoadProfile(*profilePath)
		if err != nil {
			log.Fatalf("load profile: %v", err)
		}
		log.Printf("loaded profile from %s", *profilePath)
	} else {
		profile = DefaultProfile()
		log.Println("using default profile")
	}

	openaiMux := http.NewServeMux()
	openaiMux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	openaiMux.Handle("/v1/chat/completions", NewOpenAIHandler(profile.OpenAI))
	openaiMux.Handle("/v1/audio/transcriptions", NewOpenAISTTHandler(profile.STT))
	openaiMux.Handle("/v1/audio/speech", NewOpenAITTSHandler(profile.TTS))

	// STT and TTS use catch-all handlers so SDKs (Deepgram, Cartesia) that
	// append query params or construct paths from base_url still match.
	sttHandler := NewSTTHandler(profile.STT)
	ttsHandler := NewTTSHandler(profile.TTS)
	toolHandler := NewToolHandler(profile.Tool)

	go serve("openai-sse", *openaiPort, openaiMux)
	go serve("stt-ws", *sttPort, sttHandler)
	go serve("tts-ws", *ttsPort, ttsHandler)
	go serve("tool", 8085, toolHandler)

	log.Printf("mock upstream ready: openai=:%d stt=:%d tts=:%d tool=:8085", *openaiPort, *sttPort, *ttsPort)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down")
}

func serve(name string, port int, handler http.Handler) {
	addr := fmt.Sprintf(":%d", port)
	log.Printf("starting %s on %s", name, addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("%s server failed: %v", name, err)
	}
}
