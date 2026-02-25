// Package main demonstrates hooks and guardrails with the PromptKit SDK.
//
// This example shows:
//   - Built-in guardrails: BannedWordsHook, LengthHook
//   - Custom ProviderHook: a PII detection hook
//   - Detecting hook denials for graceful handling
//   - Streaming with chunk-level guardrail enforcement
//
// Run with:
//
//	export OPENAI_API_KEY=your-key
//	go run .
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// ---------------------------------------------------------------------------
// Custom hook: PIIHook denies responses containing email or phone patterns.
// ---------------------------------------------------------------------------

// PIIHook is a ProviderHook and ChunkInterceptor that rejects responses
// containing personally identifiable information (email addresses or phone
// numbers).
type PIIHook struct {
	patterns []*regexp.Regexp
}

// Compile-time interface checks.
var (
	_ hooks.ProviderHook     = (*PIIHook)(nil)
	_ hooks.ChunkInterceptor = (*PIIHook)(nil)
)

// NewPIIHook creates a guardrail that rejects responses containing email
// addresses or phone numbers.
func NewPIIHook() *PIIHook {
	return &PIIHook{
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`),
			regexp.MustCompile(`\b\d{3}[-.]?\d{3}[-.]?\d{4}\b`),
		},
	}
}

func (h *PIIHook) Name() string { return "pii_filter" }

func (h *PIIHook) BeforeCall(_ context.Context, _ *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}

func (h *PIIHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, resp *hooks.ProviderResponse,
) hooks.Decision {
	return h.check(resp.Message.Content)
}

func (h *PIIHook) OnChunk(_ context.Context, chunk *providers.StreamChunk) hooks.Decision {
	return h.check(chunk.Content)
}

func (h *PIIHook) check(content string) hooks.Decision {
	for _, p := range h.patterns {
		if p.MatchString(content) {
			return hooks.Deny("response contains PII (email or phone number)")
		}
	}
	return hooks.Allow
}

// ---------------------------------------------------------------------------
// Helper: detect hook denial errors
// ---------------------------------------------------------------------------

// hookDenialReason extracts the denial reason from either a HookDeniedError
// (non-streaming AfterCall) or a ValidationAbortError (streaming chunk interceptor).
func hookDenialReason(err error) (string, bool) {
	var denied *hooks.HookDeniedError
	if errors.As(err, &denied) {
		return denied.Reason, true
	}
	var aborted *providers.ValidationAbortError
	if errors.As(err, &aborted) {
		return aborted.Reason, true
	}
	return "", false
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	// Open a conversation with multiple hooks registered.
	// Hooks execute in order; the first denial short-circuits.
	conv, err := sdk.Open("./hooks.pack.json", "chat",
		// Built-in: reject responses containing "password" or "secret"
		sdk.WithProviderHook(guardrails.NewBannedWordsHook([]string{"password", "secret"})),
		// Built-in: reject responses longer than 500 characters
		sdk.WithProviderHook(guardrails.NewLengthHook(500, 0)),
		// Custom: reject responses containing email/phone patterns
		sdk.WithProviderHook(NewPIIHook()),
	)
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv.Close()

	ctx := context.Background()

	// --- Example 1: Normal request (should succeed) ---
	fmt.Println("=== Example 1: Normal Request ===")
	fmt.Println()

	resp, err := conv.Send(ctx, "What is the capital of France?")
	if err != nil {
		log.Fatalf("Send failed: %v", err)
	}
	fmt.Println(resp.Text())

	// --- Example 2: Request likely to trigger the banned-words hook ---
	fmt.Println()
	fmt.Println("=== Example 2: Banned Words Hook ===")
	fmt.Println()

	resp, err = conv.Send(ctx, "What is a good default password for a database?")
	if err != nil {
		if reason, ok := hookDenialReason(err); ok {
			fmt.Printf("Blocked by guardrail: %s\n", reason)
		} else {
			log.Fatalf("Send failed: %v", err)
		}
	} else {
		fmt.Println(resp.Text())
	}

	// --- Example 3: Request likely to trigger the PII hook ---
	fmt.Println()
	fmt.Println("=== Example 3: PII Hook ===")
	fmt.Println()

	resp, err = conv.Send(ctx, "Make up a fake contact card with a name, email address, and phone number.")
	if err != nil {
		if reason, ok := hookDenialReason(err); ok {
			fmt.Printf("Blocked by guardrail: %s\n", reason)
		} else {
			log.Fatalf("Send failed: %v", err)
		}
	} else {
		fmt.Println(resp.Text())
	}

	// --- Example 4: Streaming with hooks ---
	fmt.Println()
	fmt.Println("=== Example 4: Streaming with Hooks ===")
	fmt.Println()

	fmt.Println("Streaming a short response (hooks check each chunk):")
	for chunk := range conv.Stream(ctx, "Name three countries in Europe.") {
		if chunk.Error != nil {
			if reason, ok := hookDenialReason(chunk.Error); ok {
				fmt.Printf("\n[Stream blocked by guardrail: %s]\n", reason)
			} else {
				log.Printf("Stream error: %v", chunk.Error)
			}
			break
		}
		if chunk.Type == sdk.ChunkDone {
			fmt.Println()
			break
		}
		fmt.Print(chunk.Text)
	}
}
