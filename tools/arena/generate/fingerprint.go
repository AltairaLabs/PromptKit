package generate

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

const fingerprintLength = 16

// Fingerprint produces a stable hash for a session based on its failure pattern
// and user message content. Sessions with the same fingerprint are considered
// duplicates for deduplication purposes.
func Fingerprint(session *SessionDetail) string {
	h := sha256.New()

	// Include sorted failure keys from conversation-level eval results.
	var keys []string
	for _, r := range session.EvalResults {
		if !r.Passed {
			keys = append(keys, failureKey(r))
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		_, _ = fmt.Fprintln(h, k)
	}

	// Include sorted failure keys from turn-level eval results.
	var turnKeys []string
	for idx, results := range session.TurnEvalResults {
		for _, r := range results {
			if !r.Passed {
				turnKeys = append(turnKeys, fmt.Sprintf("turn:%d:%s", idx, r.Type))
			}
		}
	}
	sort.Strings(turnKeys)
	for _, k := range turnKeys {
		_, _ = fmt.Fprintln(h, k)
	}

	// Include user message content for content-based fingerprinting.
	for i := range session.Messages {
		if session.Messages[i].Role == "user" {
			_, _ = fmt.Fprintln(h, session.Messages[i].GetContent())
		}
	}

	return fmt.Sprintf("%x", h.Sum(nil))[:fingerprintLength]
}

// DeduplicateSessions keeps the first session per unique fingerprint.
func DeduplicateSessions(sessions []*SessionDetail) []*SessionDetail {
	seen := make(map[string]bool)
	var result []*SessionDetail

	for _, s := range sessions {
		fp := Fingerprint(s)
		if seen[fp] {
			continue
		}
		seen[fp] = true
		result = append(result, s)
	}

	return result
}

// failureKey returns a stable string representation of a failed assertion result,
// composed of the assertion type and sorted detail keys.
func failureKey(r assertions.ConversationValidationResult) string {
	var parts []string
	parts = append(parts, r.Type)

	if r.Details != nil {
		var detailKeys []string
		for k := range r.Details {
			detailKeys = append(detailKeys, k)
		}
		sort.Strings(detailKeys)
		parts = append(parts, detailKeys...)
	}

	return strings.Join(parts, ":")
}
