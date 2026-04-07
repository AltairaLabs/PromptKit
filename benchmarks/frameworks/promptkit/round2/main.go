package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/websocket"
)

var (
	port   = flag.Int("port", 8090, "listen port")
	llmURL = flag.String("llm-url", "", "LLM endpoint URL")
	sttURL = flag.String("stt-url", "", "STT WebSocket URL")
	ttsURL = flag.String("tts-url", "", "TTS WebSocket URL")
)

var upgrader = websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}

func main() {
	flag.Parse()
	if *llmURL == "" {
		*llmURL = envOr("OPENAI_BASE_URL", "http://localhost:8081/v1")
	}
	if *sttURL == "" {
		*sttURL = envOr("STT_URL", "ws://localhost:8082/v1/listen")
	}
	if *ttsURL == "" {
		*ttsURL = envOr("TTS_URL", "ws://localhost:8083/tts/ws")
	}

	http.HandleFunc("/", handleVoiceSession)
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("promptkit-round2 listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func handleVoiceSession(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Phase 1: Collect audio from client until {"type":"end_audio"}.
	var audioFrames [][]byte
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if msgType == websocket.BinaryMessage {
			audioFrames = append(audioFrames, data)
			continue
		}
		// Text message — check for end signal.
		var cmd map[string]string
		if json.Unmarshal(data, &cmd) == nil && cmd["type"] == "end_audio" {
			break
		}
	}

	// Phase 2: Send audio to STT, get transcript.
	transcript, err := callSTT(audioFrames)
	if err != nil {
		log.Printf("STT error: %v", err)
		return
	}

	// Phase 3: Send transcript to LLM, get streaming response.
	llmText, err := callLLM(transcript)
	if err != nil {
		log.Printf("LLM error: %v", err)
		return
	}

	// Phase 4: Send LLM response to TTS, stream audio back to client.
	if err := callTTSAndRelay(conn, llmText); err != nil {
		log.Printf("TTS error: %v", err)
		return
	}

	// Signal completion to client.
	conn.WriteJSON(map[string]string{"type": "done"}) //nolint:errcheck
}

func callSTT(audioFrames [][]byte) (string, error) {
	conn, _, err := websocket.DefaultDialer.Dial(*sttURL, nil)
	if err != nil {
		return "", fmt.Errorf("dial STT: %w", err)
	}
	defer conn.Close()

	for _, frame := range audioFrames {
		if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			return "", fmt.Errorf("send audio: %w", err)
		}
	}
	if err := conn.WriteJSON(map[string]string{"type": "CloseStream"}); err != nil {
		return "", fmt.Errorf("send CloseStream: %w", err)
	}

	// Read until we get a final transcript.
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return "", fmt.Errorf("read STT: %w", err)
		}
		var evt struct {
			Type    string `json:"type"`
			IsFinal bool   `json:"is_final"`
			Channel struct {
				Alternatives []struct {
					Transcript string `json:"transcript"`
				} `json:"alternatives"`
			} `json:"channel"`
		}
		if json.Unmarshal(msg, &evt) != nil {
			continue
		}
		if evt.Type == "Results" && evt.IsFinal && len(evt.Channel.Alternatives) > 0 {
			return evt.Channel.Alternatives[0].Transcript, nil
		}
	}
}

func callLLM(prompt string) (string, error) {
	reqBody, err := json.Marshal(map[string]any{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"stream": true,
	})
	if err != nil {
		return "", fmt.Errorf("marshal LLM request: %w", err)
	}

	resp, err := http.Post(*llmURL+"/chat/completions", "application/json", strings.NewReader(string(reqBody)))
	if err != nil {
		return "", fmt.Errorf("POST LLM: %w", err)
	}
	defer resp.Body.Close()

	var sb strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(payload), &chunk) == nil && len(chunk.Choices) > 0 {
			sb.WriteString(chunk.Choices[0].Delta.Content)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan LLM response: %w", err)
	}
	return sb.String(), nil
}

func callTTSAndRelay(clientConn *websocket.Conn, text string) error {
	ttsConn, _, err := websocket.DefaultDialer.Dial(*ttsURL, nil)
	if err != nil {
		return fmt.Errorf("dial TTS: %w", err)
	}
	defer ttsConn.Close()

	// Send synthesis request using the simple protocol.
	if err := ttsConn.WriteJSON(map[string]any{"text": text, "voice_id": "benchmark"}); err != nil {
		return fmt.Errorf("send TTS request: %w", err)
	}

	// Relay audio chunks back to the client.
	for {
		msgType, data, err := ttsConn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read TTS: %w", err)
		}
		if msgType == websocket.BinaryMessage {
			if err := clientConn.WriteMessage(websocket.BinaryMessage, data); err != nil {
				return fmt.Errorf("relay audio: %w", err)
			}
			continue
		}
		// JSON message — check for done.
		var evt map[string]string
		if json.Unmarshal(data, &evt) == nil && evt["type"] == "done" {
			break
		}
	}
	return nil
}
