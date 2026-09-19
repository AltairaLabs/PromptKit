// Package main demonstrates the topic_policy guardrail: confining a
// conversation to a declared subject scope, decided by a topic classifier
// rather than by the model being governed.
//
// The point of the feature, and what this example makes visible, is that a
// denied turn never reaches the agent. The off-topic answer is not generated
// and then suppressed — it is never generated. The counting provider below
// reports how many turns actually reached the model.
//
// Run it:
//
//	go run .
//
// No API keys needed. By default the classifier is the deterministic
// keywordClassifier in this file, so the example runs offline and always
// produces the same output.
//
// To run the same policy against a real topic-control endpoint:
//
//	TOPIC_CONTROL_BASE_URL=http://localhost:8000/v1 \
//	TOPIC_CONTROL_MODEL=nvidia/llama-3.1-nemoguard-8b-topic-control \
//	go run .
//
// which swaps in the nvidia-topic-control backend — the same wiring a
// deployment uses, expressed as a provider file:
//
//	id: topic-control
//	role: inference
//	type: nvidia-topic-control
//	base_url: http://topic-control:8000/v1
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers" // register built-in eval handlers
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

const packPath = "./support.pack.json"

// ---------------------------------------------------------------------------
// keywordClassifier — the offline stand-in.
//
// A real topic classifier is a model that judges a message against the policy
// prose. This one matches substrings, which is enough to drive the guardrail
// deterministically in an example but is NOT a topic classifier: it cannot
// resolve "what about that one?" against the history, and it has no opinion on
// anything it has no keyword for. Point the example at a real endpoint (see
// the package comment) to see the actual behavior.
// ---------------------------------------------------------------------------

type keywordClassifier struct{}

var (
	inScopeKeywords   = []string{"omnia", "promptkit", "license", "licensing", "support", "deploy"}
	smallTalkKeywords = []string{"hello", "hi ", "thanks", "thank you", "good morning"}
)

// The error is always nil — this classifier cannot fail. The signature is
// classify.TopicClassifier's, and a real backend returns transport errors here.
//
//nolint:unparam // interface conformance
func (keywordClassifier) ClassifyTopic(
	_ context.Context, req classify.TopicRequest,
) (classify.TopicResult, error) {
	msg := strings.ToLower(req.Message)
	for _, kw := range append(inScopeKeywords, smallTalkKeywords...) {
		if strings.Contains(msg, kw) {
			return classify.TopicResult{Decision: classify.TopicAllow, Raw: "on-topic"}, nil
		}
	}
	return classify.TopicResult{Decision: classify.TopicDeny, Raw: "off-topic"}, nil
}

// ---------------------------------------------------------------------------
// countingProvider — records how many turns reached the agent.
// ---------------------------------------------------------------------------

type countingProvider struct {
	providers.Provider
	mu    sync.Mutex
	calls int
}

func (p *countingProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.record()
	return p.Provider.Predict(ctx, req)
}

func (p *countingProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	p.record()
	return p.Provider.PredictStream(ctx, req)
}

func (p *countingProvider) record() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
}

func (p *countingProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	fmt.Println("=== SDK topic_policy Example ===")

	provider := &countingProvider{
		Provider: mock.NewProviderWithRepository("mock", "mock-model", false,
			mock.NewInMemoryMockRepository("Here's what I can tell you about that.")),
	}

	opts := []sdk.Option{sdk.WithProvider(provider)}

	// The guardrail needs a topic classifier bound, or it denies every turn:
	// on_error defaults to deny, and "nothing configured at all" is an error.
	// That is deliberate — a safety control that silently does not run is the
	// failure it exists to prevent.
	if baseURL := os.Getenv("TOPIC_CONTROL_BASE_URL"); baseURL != "" {
		fmt.Printf("Classifier: nvidia-topic-control at %s\n\n", baseURL)
		opts = append(opts, sdk.WithInferenceProvider(sdk.ProviderSpec{
			ID:      "topic-control",
			Type:    "nvidia-topic-control",
			BaseURL: baseURL,
			Model:   os.Getenv("TOPIC_CONTROL_MODEL"),
		}))
	} else {
		fmt.Println("Classifier: built-in keyword stand-in (set TOPIC_CONTROL_BASE_URL for a real one)")
		fmt.Println()
		opts = append(opts, sdk.WithClassifier("topic-control", keywordClassifier{}))
	}

	conv, err := sdk.Open(packPath, "support", opts...)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer conv.Close()

	ctx := context.Background()
	for _, turn := range []struct {
		label   string
		message string
	}{
		{"in scope", "Can Omnia run on OpenShift?"},
		{"small talk", "Thanks, that helps!"},
		{"out of scope", "Who should I vote for?"},
		{"out of scope", "What should I take for a headache?"},
	} {
		before := provider.callCount()

		resp, sendErr := conv.Send(ctx, turn.message)
		if sendErr != nil {
			log.Printf("  send error: %v", sendErr)
			continue
		}

		// An enforced guardrail is not an error. The turn continues with the
		// validator's message substituted for the model's reply, and the
		// provider call never happened.
		verdict := "ALLOWED"
		if provider.callCount() == before {
			verdict = "DENIED "
		}
		fmt.Printf("[%s] %-12s %s\n", verdict, "("+turn.label+")", turn.message)
		fmt.Printf("           -> %s\n\n", resp.Text())
	}

	fmt.Printf("Turns that reached the agent: %d of 4\n", provider.callCount())
}
