package evals

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

type fakeBinding struct {
	llm        providers.Provider
	classifier inference.Provider
	err        error
}

func (f fakeBinding) LLM(string) (providers.Provider, error) {
	return f.llm, f.err
}

func (f fakeBinding) Classifier(string) (any, error) {
	return f.classifier, f.err
}

func TestProviderBinding_ContextRoundTrip(t *testing.T) {
	b := fakeBinding{}
	ctx := WithProviderBinding(context.Background(), b)

	got := BindingFromContext(ctx)

	require.NotNil(t, got, "the binding did not survive the context")
	assert.Equal(t, b, got)
}

// Absent, explicitly-nil and present must be distinguishable, and the middle
// one matters most: storing a nil binding would read back as "there is one" and
// turn a missing binding into a nil call at the first check that needs it.
func TestBindingFromContext_AbsentNilAndPresent(t *testing.T) {
	real := fakeBinding{llm: mock.NewProvider("grader", "mock-model", false)}

	present := BindingFromContext(WithProviderBinding(context.Background(), real))
	require.NotNil(t, present)
	resolved, err := present.LLM("grader")
	require.NoError(t, err)
	assert.Equal(t, "grader", resolved.ID(),
		"the binding that came back out resolves what the one going in resolved")

	assert.Nil(t, BindingFromContext(context.Background()),
		"a context with no binding must report none rather than an empty one")
	assert.Nil(t, BindingFromContext(WithProviderBinding(context.Background(), nil)),
		"a nil binding must not be stored as a present one")
	//nolint:staticcheck // deliberately passing a nil context: callers do, and it must not panic
	assert.Nil(t, BindingFromContext(nil))
}

// Storing nil must also leave an existing binding alone rather than shadowing
// it with something unusable.
func TestWithProviderBinding_NilLeavesAnExistingBindingInPlace(t *testing.T) {
	ctx := WithProviderBinding(context.Background(),
		fakeBinding{llm: mock.NewProvider("grader", "mock-model", false)})

	got := BindingFromContext(WithProviderBinding(ctx, nil))

	require.NotNil(t, got, "the earlier binding was shadowed by a nil one")
	resolved, err := got.LLM("grader")
	require.NoError(t, err)
	assert.Equal(t, "grader", resolved.ID())
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
