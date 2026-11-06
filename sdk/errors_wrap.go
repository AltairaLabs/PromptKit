package sdk

import (
	"errors"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// validatorTypeMetadataKey is the decision metadata key a guardrail uses to
// name its validator type.
const validatorTypeMetadataKey = "validator_type"

// chunkHookType names the hook point of a stream chunk denial.
const chunkHookType = "provider_chunk"

// packLoadError reports a failure to load or validate the pack at path.
func packLoadError(path string, err error) error {
	var pe *PackError
	if errors.As(err, &pe) {
		return err
	}
	return &PackError{Path: path, Cause: err}
}

// wrapTurnError gives a failed turn's error its SDK type: a hook or guardrail
// denial becomes a *ValidationError and a provider API or connection failure a
// *ProviderError, each wrapping the runtime error. Any other error is returned
// unchanged. fallbackProvider names the provider when the runtime error does
// not.
func wrapTurnError(err error, fallbackProvider string) error {
	if err == nil {
		return nil
	}
	var ve *ValidationError
	var pe *ProviderError
	if errors.As(err, &ve) || errors.As(err, &pe) {
		return err
	}
	if v := asValidationFailure(err); v != nil {
		return v
	}
	if p := asProviderFailure(err, fallbackProvider); p != nil {
		return p
	}
	return err
}

// asValidationFailure wraps a hook denial or a stream validation abort.
func asValidationFailure(err error) *ValidationError {
	var denied *hooks.HookDeniedError
	if errors.As(err, &denied) {
		validatorType := denied.HookType
		if t, ok := denied.Metadata[validatorTypeMetadataKey].(string); ok && t != "" {
			validatorType = t
		}
		return &ValidationError{
			ValidatorType: validatorType,
			Message:       denied.Reason,
			Details:       denied.Metadata,
			cause:         err,
		}
	}
	var aborted *providers.ValidationAbortError
	if errors.As(err, &aborted) {
		return &ValidationError{ValidatorType: chunkHookType, Message: aborted.Reason, cause: err}
	}
	return nil
}

// asProviderFailure wraps a provider HTTP error or transport failure.
func asProviderFailure(err error, fallbackProvider string) *ProviderError {
	var httpErr *providers.ProviderHTTPError
	if errors.As(err, &httpErr) {
		return &ProviderError{
			Provider:   firstNonEmpty(httpErr.Provider, fallbackProvider),
			StatusCode: httpErr.StatusCode,
			Message:    err.Error(),
			Cause:      err,
		}
	}
	var transportErr *providers.ProviderTransportError
	if errors.As(err, &transportErr) {
		return &ProviderError{
			Provider: firstNonEmpty(transportErr.Provider, fallbackProvider),
			Message:  err.Error(),
			Cause:    err,
		}
	}
	return nil
}

// firstNonEmpty returns a, or b when a is empty.
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// providerID names the conversation's provider for a ProviderError.
func (c *Conversation) providerID() string {
	if c.provider != nil {
		return c.provider.ID()
	}
	if c.config != nil {
		if p := c.config.getAgentProvider(); p != nil {
			return p.ID()
		}
	}
	return ""
}

// wrapTurnError is wrapTurnError naming this conversation's provider.
func (c *Conversation) wrapTurnError(err error) error {
	return wrapTurnError(err, c.providerID())
}

// toolResultErrorText is the text the model receives for a failed tool: the
// handler's own error. A *ToolError names the tool for the Go caller, and the
// model already knows which tool it called, so its Cause is reported alone.
func toolResultErrorText(err error) string {
	if te, ok := err.(*ToolError); ok && te.Cause != nil { //nolint:errorlint // only a top-level ToolError
		return te.Cause.Error()
	}
	return err.Error()
}
