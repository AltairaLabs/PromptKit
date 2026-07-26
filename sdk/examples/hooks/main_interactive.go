// Package main demonstrates hooks and guardrails with the PromptKit SDK.
//
// This example shows:
//   - Input guardrails that gate the user's message BEFORE the LLM call
//     (guardrails.Input, guardrails.InputFunc) — a hit spends zero tokens
//   - Output guardrails that gate the assistant's response AFTER the LLM call
//     (guardrails.Output)
//   - Pack-declared validators: the same input/output gating with zero Go
//     code, via hooks.pack.json's "validators" block
//   - The two ways a guardrail can respond, and how to tell them apart:
//   - Enforced — a graceful block. Send returns no error; the turn carries
//     a canned message and a validation record instead.
//   - Deny — a hard error. Send returns an error you catch with errors.As.
//   - Custom ProviderHook: a PII detection hook (the full-interface path,
//     used here to demonstrate Deny)
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
	"strings"

	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
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
// numbers). It implements the full hooks.ProviderHook interface — useful when
// a check needs both a synchronous AfterCall and a streaming OnChunk path
// sharing state, which the InputFunc/OutputFunc plain-function form doesn't
// cover. It answers with hooks.Deny: a hard error, not a graceful block — see
// Example 6 below and hookDenialReason.
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
// Helper: detect hook denials (the Deny / hard-error path)
// ---------------------------------------------------------------------------

// hookDenialReason extracts the denial reason from either a HookDeniedError
// (non-streaming AfterCall) or a ValidationAbortError (streaming chunk
// interceptor). Both only ever show up when a hook returns hooks.Deny — an
// Enforced guardrail never produces an error, so it never reaches this
// helper. See blockedByGuardrail for detecting the graceful-block path.
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
// Helper: detect a guardrail-blocked (Enforced) turn, without string-matching
// ---------------------------------------------------------------------------

// blockedByGuardrail reports whether resp is a turn a guardrail blocked
// gracefully (Enforced), and if so, which guardrail. It works by checking
// resp.Validations() for a failed entry, not by comparing resp.Text() against
// known blocked-message strings.
//
// This is the most reliable detection the public SDK offers today. The
// pipeline internally marks a blocked turn's *types.Message with
// FinishReason == types.FinishReasonSafety, but that doesn't reach the
// caller: sdk.Response has no FinishReason accessor, and Response.Message()
// returns a message rebuilt from the pipeline's narrow Response struct, which
// doesn't carry FinishReason either — so it's always empty here, blocked or
// not. See README.md for more on this gap.
//
// A caveat on Validations(): it's only populated when the firing guardrail's
// Decision.Metadata carries a "validator_type" key. guardrails.Input/Output
// (eval-backed) always set it. A bespoke guardrails.InputFunc/OutputFunc only
// does if its hooks.Enforced(...) call includes it explicitly — see the
// "no-ssn" guardrail below for the pattern.
func blockedByGuardrail(resp *sdk.Response) (validatorType string, blocked bool) {
	for _, v := range resp.Validations() {
		if !v.Passed {
			return v.ValidatorType, true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	// Guardrails are declared with sdk.WithGuardrail. Each guardrails.Spec
	// says which direction it gates:
	//
	//   - Input(...) / InputFunc(...)   gate the user's message BEFORE the
	//     LLM call. A hit costs zero tokens — no provider request is made.
	//   - Output(...) / OutputFunc(...) gate the assistant's response AFTER
	//     the LLM call.
	//
	// Whichever direction, a firing eval-backed or func-backed guardrail
	// returns hooks.Enforced: Send() returns no error, the assistant turn is
	// replaced with a canned message (the guardrails.WithMessage text, or
	// prompt.DefaultBlockedMessage if none was given), and the turn is
	// recorded on resp.Validations(). That's the graceful-block path — see
	// blockedByGuardrail.
	//
	// A hard error only happens when a hook returns hooks.Deny instead of
	// Enforced — see the custom PIIHook (Example 6) and hookDenialReason.
	//
	// hooks.pack.json also declares one input guardrail directly on the
	// "chat" prompt's "validators" block — no Go code needed for it at all
	// (Example 4).
	conv, err := sdk.Open("./hooks.pack.json", "chat",
		sdk.WithGuardrail(
			// --- Input guardrails: gate the user's message, before any LLM call ---

			// Eval-backed: "regex" with expect_match:false fails (blocks) when
			// the pattern IS found — a "banned phrase" check. This example
			// deliberately does NOT use "banned_words" here: that eval type
			// (content_excludes) and "contains_any" only ever scan
			// assistant-role messages, so as an input guardrail they'd compile
			// and register but silently never fire against the user's
			// message. Content-agnostic eval types — regex, length,
			// pii_leakage, contains, json_valid — all read the input
			// correctly instead.
			guardrails.Input("regex", map[string]any{
				"pattern":      `(?i)\bwire transfer\b`,
				"expect_match": false,
			}, guardrails.WithMessage("I can't help with transfers.")),

			// --- Output guardrails: gate the assistant's response, after the LLM call ---

			// Reject responses containing "password" or "secret". Like every
			// eval-backed guardrail, a hit here is Enforced (graceful), not a
			// hard error — see Example 5.
			guardrails.Output("banned_words", map[string]any{
				"words": []any{"password", "secret"},
			}, guardrails.WithMessage("I can't share credentials like that.")),
			// Reject responses longer than 500 characters
			guardrails.Output("length", map[string]any{
				"max_characters": 500,
			}),

			// Bespoke input check with no interface to implement: InputFunc
			// wraps a plain function that runs once per user turn (it's
			// skipped on tool-loop rounds, where the last message is a tool
			// result rather than new user input).
			guardrails.InputFunc("no-ssn", func(_ context.Context, in *hooks.InputRequest) hooks.Decision {
				if strings.Contains(strings.ToLower(in.UserInput), "social security number") {
					in.Replacement = "I can't help with that request."
					// Passing validator_type in metadata is what makes this
					// firing show up on resp.Validations() (see
					// blockedByGuardrail). Omit it and the block is still
					// graceful — no error, canned text — it just carries no
					// structured validation record for the caller to inspect.
					return hooks.Enforced("ssn requested", map[string]any{"validator_type": "no-ssn"})
				}
				return hooks.Allow
			}),
		),
		// Custom: reject responses containing email/phone patterns. This
		// implements the full hooks.ProviderHook interface and answers with
		// hooks.Deny — a hard error, not a graceful block (contrast Example 5).
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

	// --- Example 2: Input guardrail — graceful block (Enforced) ---
	fmt.Println()
	fmt.Println("=== Example 2: Input Guardrail — Graceful Block (Enforced) ===")
	fmt.Println()

	resp, err = conv.Send(ctx, "Can you help me set up a wire transfer?")
	if err != nil {
		// Not expected here: an input guardrail firing does NOT error.
		log.Fatalf("Send failed unexpectedly: %v", err)
	}
	fmt.Println("No error — the LLM was never called; zero tokens spent.")
	fmt.Printf("Response text (canned): %s\n", resp.Text())
	if name, blocked := blockedByGuardrail(resp); blocked {
		fmt.Printf("Blocked by guardrail: %s\n", name)
	}

	// --- Example 3: Bespoke input check (InputFunc) — graceful block ---
	fmt.Println()
	fmt.Println("=== Example 3: Bespoke Input Guardrail (InputFunc) ===")
	fmt.Println()

	resp, err = conv.Send(ctx, "What's my social security number on file?")
	if err != nil {
		log.Fatalf("Send failed unexpectedly: %v", err)
	}
	fmt.Printf("Response text (canned): %s\n", resp.Text())
	if name, blocked := blockedByGuardrail(resp); blocked {
		fmt.Printf("Blocked by guardrail: %s\n", name)
	}

	// --- Example 4: Pack-declared input validator — zero Go code ---
	fmt.Println()
	fmt.Println("=== Example 4: Pack-Declared Input Validator ===")
	fmt.Println()

	resp, err = conv.Send(ctx, "What's your bank's routing number?")
	if err != nil {
		log.Fatalf("Send failed unexpectedly: %v", err)
	}
	fmt.Printf("Response text (canned): %s\n", resp.Text())
	if name, blocked := blockedByGuardrail(resp); blocked {
		fmt.Printf("Blocked by guardrail declared in hooks.pack.json: %s\n", name)
	}

	// --- Example 5: Output guardrail — graceful block (Enforced) ---
	fmt.Println()
	fmt.Println("=== Example 5: Output Guardrail — Graceful Block (Enforced) ===")
	fmt.Println()

	resp, err = conv.Send(ctx, "What is a good default password for a database?")
	if err != nil {
		// Also not expected: eval-backed output guardrails enforce
		// gracefully too, same as input guardrails — they never Deny.
		log.Fatalf("Send failed unexpectedly: %v", err)
	}
	if name, blocked := blockedByGuardrail(resp); blocked {
		fmt.Printf("Response text (canned): %s\n", resp.Text())
		fmt.Printf("Blocked by guardrail: %s\n", name)
	} else {
		// The model didn't say "password" or "secret" this time — nothing to block.
		fmt.Println(resp.Text())
	}

	// --- Example 6: Custom PII hook — hard error (Deny) ---
	fmt.Println()
	fmt.Println("=== Example 6: Custom PII Hook — Hard Error (Deny) ===")
	fmt.Println()

	resp, err = conv.Send(ctx, "Make up a fake contact card with a name, email address, and phone number.")
	if err != nil {
		if reason, ok := hookDenialReason(err); ok {
			fmt.Printf("Send returned an error — blocked by guardrail: %s\n", reason)
		} else {
			log.Fatalf("Send failed: %v", err)
		}
	} else {
		fmt.Println(resp.Text())
	}

	// --- Example 7: Streaming with hooks ---
	fmt.Println()
	fmt.Println("=== Example 7: Streaming with Hooks ===")
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
