package providers

import (
	"regexp"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Wire names of the sampling parameters a provider learns its model rejects.
const (
	ParamTemperature = "temperature"
	ParamTopP        = "top_p"
)

// learnableParams are the parameters a rejection can withhold. Anything else
// an API names (max_tokens, tools, ...) is not ours to drop silently.
var learnableParams = map[string]bool{
	ParamTemperature: true, ParamTopP: true, ParamTopK: true,
	ParamFrequencyPenalty: true, ParamPresencePenalty: true,
}

// rejectionPatterns match the 400s in which an API names a parameter its model
// does not take, each captured from a live response:
//
//	OpenAI:    Unsupported parameter: 'top_p' is not supported with this model.
//	OpenAI:    Unsupported value: 'temperature' does not support 0 with this model.
//	Anthropic: `temperature` is deprecated for this model.
var rejectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`Unsupported (?:parameter|value): '([a-z_]+)'`),
	regexp.MustCompile("`([a-z_]+)` is deprecated for this model"),
}

// geminiPenaltyRejection is Gemini's 400 for either penalty, which names
// neither: "Penalty is not enabled for this model".
const geminiPenaltyRejection = "Penalty is not enabled"

// RejectedParams returns the sampling parameters err reports the API rejected
// for this model, or nil when err is not such a rejection.
func RejectedParams(err error) []string {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if !strings.Contains(msg, "400") {
		return nil
	}
	var out []string
	for _, re := range rejectionPatterns {
		for _, m := range re.FindAllStringSubmatch(msg, -1) {
			if learnableParams[m[1]] {
				out = append(out, m[1])
			}
		}
	}
	if strings.Contains(msg, geminiPenaltyRejection) {
		out = append(out, ParamFrequencyPenalty, ParamPresencePenalty)
	}
	return out
}

// rejectedParams is the set of parameters a provider's API has rejected.
type rejectedParams struct {
	mu  sync.RWMutex
	set map[string]bool
}

// ParamRejected reports whether the API has rejected param for this
// provider's model. A rejected parameter is not sent.
func (b *BaseProvider) ParamRejected(param string) bool {
	if b.rejected == nil {
		return false
	}
	b.rejected.mu.RLock()
	defer b.rejected.mu.RUnlock()
	return b.rejected.set[param]
}

// RejectedParamNames returns the parameters the API has rejected so far.
func (b *BaseProvider) RejectedParamNames() []string {
	if b.rejected == nil {
		return nil
	}
	b.rejected.mu.RLock()
	defer b.rejected.mu.RUnlock()
	out := make([]string, 0, len(b.rejected.set))
	for p := range b.rejected.set {
		out = append(out, p)
	}
	return out
}

// RetryRejectedParams runs call, and while it fails because the API rejected
// a sampling parameter that was still being sent, records the parameter as
// rejected (so the provider stops sending it, here and on later calls) and
// runs call again. Each retry follows a newly rejected parameter, so it ends
// within len(learnableParams) retries. OpenAI names one parameter per 400,
// which is why it loops.
func (b *BaseProvider) RetryRejectedParams(call func() error) error {
	for {
		sent := b.rejectedSnapshot()
		err := call()
		if !b.learnRejected(err, sent) {
			return err
		}
	}
}

// RetryCall runs call with req through b.RetryRejectedParams, after moving
// req's system-role messages to its System field (NormalizeMessages): the APIs
// reject "system" as a message role on some paths. Each provider's Predict and
// PredictStream are this around their single attempt.
func RetryCall[T any](b *BaseProvider, req PredictionRequest, call func(PredictionRequest) (T, error)) (T, error) {
	req.NormalizeMessages()
	var out T
	err := b.RetryRejectedParams(func() (err error) {
		out, err = call(req)
		return err
	})
	return out, err
}

// RetryToolCall is RetryCall for PredictWithTools, which also returns the
// tool calls.
func RetryToolCall(
	b *BaseProvider, req PredictionRequest,
	call func(PredictionRequest) (PredictionResponse, []types.MessageToolCall, error),
) (PredictionResponse, []types.MessageToolCall, error) {
	type result struct {
		resp  PredictionResponse
		calls []types.MessageToolCall
	}
	r, err := RetryCall(b, req, func(req PredictionRequest) (result, error) {
		resp, calls, err := call(req)
		return result{resp, calls}, err
	})
	return r.resp, r.calls, err
}

// rejectedSnapshot copies the rejected set as it stood when a request was built.
func (b *BaseProvider) rejectedSnapshot() map[string]bool {
	if b.rejected == nil {
		return nil
	}
	b.rejected.mu.RLock()
	defer b.rejected.mu.RUnlock()
	out := make(map[string]bool, len(b.rejected.set))
	for p := range b.rejected.set {
		out[p] = true
	}
	return out
}

// learnRejected records the parameters err reports rejected and reports
// whether any was still being sent when the failed request was built (absent
// from sent), which makes a retry worthwhile.
func (b *BaseProvider) learnRejected(err error, sent map[string]bool) bool {
	params := RejectedParams(err)
	if len(params) == 0 || b.rejected == nil {
		return false
	}
	retry := false
	b.rejected.mu.Lock()
	defer b.rejected.mu.Unlock()
	for _, p := range params {
		if sent[p] {
			continue
		}
		retry = true
		if !b.rejected.set[p] {
			b.rejected.set[p] = true
			logger.Warn("the model rejected a prompt parameter; it is no longer sent to this provider",
				"provider", b.id, "param", p)
		}
	}
	return retry
}
