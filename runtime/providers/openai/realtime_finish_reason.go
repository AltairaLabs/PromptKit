package openai

import "github.com/AltairaLabs/PromptKit/runtime/v2/types"

// Realtime response status values, and the status_details reason for a reply
// blocked by the content filter.
const (
	realtimeStatusCanceled      = "cancelled" //nolint:misspell // the documented wire value
	realtimeReasonContentFilter = "content_filter"
)

// realtimeFinishReason maps a Realtime response's status (and, for an
// incomplete response, its status_details.reason) onto the canonical finish
// reason vocabulary in runtime/types, which every other provider reports.
// A canceled response is a barge-in or client cancel, so it reports as interrupted;
// "failed" reports as "error", the value a failed stream carries elsewhere.
// Unknown values pass through verbatim so a new status is never swallowed.
func realtimeFinishReason(r *ResponseInfo) string {
	switch r.Status {
	case "completed":
		return types.FinishReasonStop
	case realtimeStatusCanceled:
		return types.FinishReasonInterrupted
	case "failed":
		return "error"
	case "incomplete":
		switch statusDetailsReason(r.StatusDetails) {
		case "max_output_tokens":
			return types.FinishReasonMaxOutputTokens
		case realtimeReasonContentFilter:
			return types.FinishReasonSafety
		}
	}
	return r.Status
}

// statusDetailsReason reads status_details.reason, "" when absent.
func statusDetailsReason(details interface{}) string {
	m, ok := details.(map[string]interface{})
	if !ok {
		return ""
	}
	reason, _ := m["reason"].(string)
	return reason
}
