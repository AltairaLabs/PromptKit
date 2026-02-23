package main

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/tools/arena/generate"
)

func newGenerateTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "generate", RunE: runGenerate}
	cmd.Flags().String("source", "", "")
	cmd.Flags().String("from-recordings", "", "")
	cmd.Flags().String("filter-eval-type", "", "")
	cmd.Flags().Bool("filter-passed", false, "")
	cmd.Flags().String("pack", "", "")
	cmd.Flags().String("output", ".", "")
	cmd.Flags().Bool("dedup", true, "")
	return cmd
}

func TestResolveAdapter_FromRecordings(t *testing.T) {
	cmd := newGenerateTestCmd()
	require.NoError(t, cmd.Flags().Set("from-recordings", "*.recording.json"))

	adapter, err := resolveAdapter(cmd)
	require.NoError(t, err)
	assert.Equal(t, "recordings", adapter.Name())
}

func TestResolveAdapter_FromSource(t *testing.T) {
	// Register a test adapter in the global registry.
	generateRegistry.Register(&testSourceAdapter{name: "test-source"})

	cmd := newGenerateTestCmd()
	require.NoError(t, cmd.Flags().Set("source", "test-source"))

	adapter, err := resolveAdapter(cmd)
	require.NoError(t, err)
	assert.Equal(t, "test-source", adapter.Name())
}

func TestResolveAdapter_NeitherFlag(t *testing.T) {
	cmd := newGenerateTestCmd()
	_, err := resolveAdapter(cmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "specify either --source or --from-recordings")
}

func TestBuildListOptions_Default(t *testing.T) {
	cmd := newGenerateTestCmd()
	opts, err := buildListOptions(cmd)
	require.NoError(t, err)
	assert.Nil(t, opts.FilterPassed, "FilterPassed should be nil when flag not changed")
	assert.Empty(t, opts.FilterEvalType)
}

func TestBuildListOptions_WithFilterPassed(t *testing.T) {
	cmd := newGenerateTestCmd()
	require.NoError(t, cmd.Flags().Set("filter-passed", "false"))

	opts, err := buildListOptions(cmd)
	require.NoError(t, err)
	require.NotNil(t, opts.FilterPassed)
	assert.False(t, *opts.FilterPassed)
}

func TestBuildListOptions_WithFilterEvalType(t *testing.T) {
	cmd := newGenerateTestCmd()
	require.NoError(t, cmd.Flags().Set("filter-eval-type", "content_matches"))

	opts, err := buildListOptions(cmd)
	require.NoError(t, err)
	assert.Equal(t, "content_matches", opts.FilterEvalType)
}

// testSourceAdapter is a minimal test double for the generate command tests.
type testSourceAdapter struct {
	name string
}

func (a *testSourceAdapter) Name() string { return a.name }

func (a *testSourceAdapter) List(
	_ context.Context,
	_ generate.ListOptions,
) ([]generate.SessionSummary, error) {
	return nil, nil
}

func (a *testSourceAdapter) Get(
	_ context.Context,
	_ string,
) (*generate.SessionDetail, error) {
	return nil, nil
}
