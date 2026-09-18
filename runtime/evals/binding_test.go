package evals

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

type fakeBinding struct {
	llm        providers.Provider
	classifier classify.Backend
	err        error
}

func (f fakeBinding) LLM(string) (providers.Provider, error) {
	return f.llm, f.err
}

func (f fakeBinding) Classifier(string) (classify.Backend, error) {
	return f.classifier, f.err
}

func TestProviderBinding_ContextRoundTrip(t *testing.T) {
	b := fakeBinding{}
	ctx := WithProviderBinding(context.Background(), b)

	got := BindingFromContext(ctx)

	require.NotNil(t, got, "the binding did not survive the context")
	assert.Equal(t, b, got)
}

func TestBindingFromContext_AbsentAndNil(t *testing.T) {
	assert.Nil(t, BindingFromContext(context.Background()),
		"a context with no binding must report none rather than an empty one")
	//nolint:staticcheck // deliberately passing a nil context: callers do, and it must not panic
	assert.Nil(t, BindingFromContext(nil))
}

// WithProviderBinding(nil) leaves the context alone rather than storing a typed
// nil, which would later read back as "there is a binding" and turn a missing
// one into a nil-pointer call.
func TestWithProviderBinding_NilIsNotStored(t *testing.T) {
	ctx := WithProviderBinding(context.Background(), nil)

	assert.Nil(t, BindingFromContext(ctx))
}

// DescribeUnresolved exists so the person reading the error knows whose problem
// it is. Each case has to say something different, or it is not doing its job.
func TestDescribeUnresolved(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		contains []string
	}{
		{
			name:     "no binding at all",
			err:      ErrNoBinding,
			contains: []string{"grader", "no provider binding"},
		},
		{
			name:     "host bound nothing",
			err:      ErrUnboundKey,
			contains: []string{"grader", "requires block", "supply a provider"},
		},
		{
			name:     "host bound the wrong kind",
			err:      ErrWrongKind,
			contains: []string{"grader", "cannot do what the check needs"},
		},
		{
			name:     "anything else is passed through",
			err:      errors.New("registry exploded"),
			contains: []string{"grader", "registry exploded"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := DescribeUnresolved("grader", tc.err)
			for _, want := range tc.contains {
				assert.Contains(t, got, want)
			}
		})
	}
}

// The three sentinels must stay distinguishable: callers branch on them to
// decide whether the fix is in the pack, in the host's wiring, or in what the
// host wired.
func TestBindingErrors_AreDistinct(t *testing.T) {
	assert.False(t, errors.Is(ErrUnboundKey, ErrWrongKind))
	assert.False(t, errors.Is(ErrWrongKind, ErrNoBinding))
	assert.False(t, errors.Is(ErrNoBinding, ErrUnboundKey))

	wrapped := errors.Join(ErrWrongKind, errors.New("context"))
	assert.True(t, errors.Is(wrapped, ErrWrongKind), "wrapping must not hide the sentinel")
}
