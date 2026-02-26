package errors_test

import (
	"errors"
	"fmt"
	"io"
	"testing"

	pkgerrors "github.com/AltairaLabs/PromptKit/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	cause := fmt.Errorf("connection refused")
	err := pkgerrors.New("sdk", "OpenConversation", cause)

	assert.Equal(t, "sdk", err.Component)
	assert.Equal(t, "OpenConversation", err.Operation)
	assert.Equal(t, 0, err.StatusCode)
	assert.Nil(t, err.Details)
	assert.Equal(t, cause, err.Cause)
}

func TestNew_NilCause(t *testing.T) {
	err := pkgerrors.New("runtime", "LoadPack", nil)

	assert.Equal(t, "runtime", err.Component)
	assert.Equal(t, "LoadPack", err.Operation)
	assert.Nil(t, err.Cause)
}

func TestError_BasicMessage(t *testing.T) {
	cause := fmt.Errorf("file not found")
	err := pkgerrors.New("arena", "LoadScenario", cause)

	assert.Equal(t, "[arena] LoadScenario: file not found", err.Error())
}

func TestError_NoCause(t *testing.T) {
	err := pkgerrors.New("sdk", "Initialize", nil)

	assert.Equal(t, "[sdk] Initialize", err.Error())
}

func TestError_WithStatusCode(t *testing.T) {
	cause := fmt.Errorf("unauthorized")
	err := pkgerrors.New("runtime", "CallProvider", cause).WithStatusCode(401)

	assert.Equal(t, "[runtime] CallProvider (status 401): unauthorized", err.Error())
}

func TestError_WithStatusCodeNoCause(t *testing.T) {
	err := pkgerrors.New("sdk", "Authenticate", nil).WithStatusCode(403)

	assert.Equal(t, "[sdk] Authenticate (status 403)", err.Error())
}

func TestWithStatusCode(t *testing.T) {
	err := pkgerrors.New("sdk", "Send", fmt.Errorf("timeout"))
	result := err.WithStatusCode(504)

	// Builder returns same pointer for chaining.
	assert.Same(t, err, result)
	assert.Equal(t, 504, err.StatusCode)
}

func TestWithDetails(t *testing.T) {
	details := map[string]any{
		"pack":     "customer-support",
		"provider": "openai",
		"retries":  3,
	}
	err := pkgerrors.New("sdk", "Open", fmt.Errorf("failed"))
	result := err.WithDetails(details)

	assert.Same(t, err, result)
	assert.Equal(t, details, err.Details)
}

func TestChainedBuilders(t *testing.T) {
	err := pkgerrors.New("runtime", "Execute", fmt.Errorf("bad request")).
		WithStatusCode(400).
		WithDetails(map[string]any{"input": "too long"})

	assert.Equal(t, 400, err.StatusCode)
	assert.Equal(t, map[string]any{"input": "too long"}, err.Details)
	assert.Equal(t, "[runtime] Execute (status 400): bad request", err.Error())
}

func TestUnwrap(t *testing.T) {
	cause := fmt.Errorf("root cause")
	err := pkgerrors.New("sdk", "Open", cause)

	assert.Equal(t, cause, err.Unwrap())
}

func TestUnwrap_NilCause(t *testing.T) {
	err := pkgerrors.New("sdk", "Open", nil)

	assert.Nil(t, err.Unwrap())
}

func TestErrorsIs(t *testing.T) {
	sentinel := fmt.Errorf("sentinel error")
	wrapped := fmt.Errorf("mid-layer: %w", sentinel)
	err := pkgerrors.New("sdk", "Process", wrapped)

	assert.True(t, errors.Is(err, sentinel))
	assert.True(t, errors.Is(err, wrapped))
}

func TestErrorsAs(t *testing.T) {
	cause := fmt.Errorf("something failed")
	err := pkgerrors.New("arena", "Evaluate", cause)

	// Wrap in another error layer to test errors.As unwrapping.
	outer := fmt.Errorf("outer: %w", err)

	var ctxErr *pkgerrors.ContextualError
	require.True(t, errors.As(outer, &ctxErr))
	assert.Equal(t, "arena", ctxErr.Component)
	assert.Equal(t, "Evaluate", ctxErr.Operation)
}

func TestErrorInterface(t *testing.T) {
	var err error = pkgerrors.New("sdk", "Open", nil)
	assert.NotNil(t, err)
	assert.Equal(t, "[sdk] Open", err.Error())
}

func TestNestedContextualErrors(t *testing.T) {
	inner := pkgerrors.New("runtime", "CallLLM", io.ErrUnexpectedEOF).WithStatusCode(500)
	outer := pkgerrors.New("sdk", "Send", inner).WithStatusCode(502)

	assert.Equal(t, "[sdk] Send (status 502): [runtime] CallLLM (status 500): unexpected EOF", outer.Error())

	// Unwrap chain works.
	assert.True(t, errors.Is(outer, io.ErrUnexpectedEOF))

	var innerErr *pkgerrors.ContextualError
	require.True(t, errors.As(outer, &innerErr))
	// errors.As finds the first match, which is outer itself.
	assert.Equal(t, "sdk", innerErr.Component)
}

func TestZeroStatusCodeOmitted(t *testing.T) {
	err := pkgerrors.New("sdk", "Open", fmt.Errorf("fail")).WithStatusCode(0)

	assert.Equal(t, "[sdk] Open: fail", err.Error())
}

func TestDetailsDoNotAffectErrorString(t *testing.T) {
	err := pkgerrors.New("sdk", "Open", nil).
		WithDetails(map[string]any{"key": "value"})

	// Details are metadata only; they should not appear in the error string.
	assert.Equal(t, "[sdk] Open", err.Error())
}
