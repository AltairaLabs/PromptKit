// Command server is a minimal A2A server with Bearer token authentication.
// It implements two skills (echo, reverse) without requiring an LLM.
//
// Usage:
//
//	go run ./examples/a2a-auth-test/server
//
// The server listens on port 9877 and requires Bearer token "test-token-123".
// Agent card: http://localhost:9877/.well-known/agent.json
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	a2aserver "github.com/AltairaLabs/PromptKit/server/a2a"
)

const (
	serverPort = 9877
	authToken  = "test-token-123"
)

// bearerAuth implements a2aserver.Authenticator with a static Bearer token.
type bearerAuth struct {
	token string
}

func (b *bearerAuth) Authenticate(r *http.Request) error {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return fmt.Errorf("missing authorization header")
	}
	_, token, ok := strings.Cut(auth, " ")
	if !ok || token != b.token {
		return fmt.Errorf("invalid token")
	}
	return nil
}

// echoConversation implements a2aserver.Conversation.
// It extracts text from the incoming message and echoes it back.
type echoConversation struct{}

func (c *echoConversation) Send(_ context.Context, message any) (a2aserver.SendResult, error) {
	msg, ok := message.(*types.Message)
	if !ok {
		return &echoResult{text: "Error: unexpected message type"}, nil
	}

	var text string
	if len(msg.Parts) > 0 {
		for _, part := range msg.Parts {
			if part.Text != nil {
				text += *part.Text
			}
		}
	} else {
		text = msg.Content
	}

	if strings.Contains(strings.ToLower(text), "reverse") {
		// Extract the text after "reverse" and reverse it
		cleaned := strings.TrimSpace(text)
		runes := []rune(cleaned)
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
			runes[i], runes[j] = runes[j], runes[i]
		}
		return &echoResult{text: string(runes)}, nil
	}

	return &echoResult{text: fmt.Sprintf("Echo: %s", text)}, nil
}

func (c *echoConversation) Close() error { return nil }

// echoResult implements a2aserver.SendResult.
type echoResult struct {
	text string
}

func (r *echoResult) HasPendingTools() bool                                 { return false }
func (r *echoResult) HasPendingClientTools() bool                           { return false }
func (r *echoResult) PendingClientTools() []a2aserver.PendingClientToolInfo { return nil }
func (r *echoResult) Text() string                                          { return r.text }
func (r *echoResult) Parts() []types.ContentPart {
	return []types.ContentPart{{Text: &r.text}}
}

func main() {
	card := a2a.AgentCard{
		Name:               "Echo Agent",
		Description:        "A test agent that echoes messages and reverses strings",
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Skills: []a2a.AgentSkill{
			{
				ID:          "echo",
				Name:        "Echo",
				Description: "Echoes the input message back",
				Tags:        []string{"test", "echo"},
			},
			{
				ID:          "reverse",
				Name:        "Reverse String",
				Description: "Reverses the input string",
				Tags:        []string{"test", "reverse"},
			},
		},
	}

	opener := func(_ string) (a2aserver.Conversation, error) {
		return &echoConversation{}, nil
	}

	server := a2aserver.NewServer(opener,
		a2aserver.WithCard(&card),
		a2aserver.WithPort(serverPort),
		a2aserver.WithAuthenticator(&bearerAuth{token: authToken}),
	)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		os.Exit(0)
	}()

	fmt.Printf("A2A echo server listening on http://localhost:%d\n", serverPort)
	fmt.Printf("Agent card: http://localhost:%d/.well-known/agent.json\n", serverPort)
	fmt.Printf("Auth: Bearer %s\n", authToken)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
