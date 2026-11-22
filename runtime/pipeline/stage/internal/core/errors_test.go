package core

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStageError_Error(t *testing.T) {
	err := &StageError{
		StageName: "my-stage",
		StageType: StageTypeTransform,
		Err:       assert.AnError,
	}

	errorMsg := err.Error()

	assert.Contains(t, errorMsg, "my-stage")
	assert.Contains(t, errorMsg, "transform")
}

func TestStageError_Unwrap(t *testing.T) {
	originalErr := assert.AnError
	stageErr := &StageError{
		StageName: "test",
		StageType: StageTypeGenerate,
		Err:       originalErr,
	}

	assert.Equal(t, originalErr, stageErr.Unwrap())
}

func TestNewStageError(t *testing.T) {
	err := NewStageError("my-stage", StageTypeSink, assert.AnError)

	assert.Equal(t, "my-stage", err.StageName)
	assert.Equal(t, StageTypeSink, err.StageType)
	assert.Equal(t, assert.AnError, err.Err)
}

func TestMaxRoundsError(t *testing.T) {
	err := error(&MaxRoundsError{Rounds: 7})
	assert.Equal(t, "provider stage: max rounds (7) exceeded", err.Error())
	assert.ErrorIs(t, err, ErrMaxRoundsExceeded)
	var mre *MaxRoundsError
	require.ErrorAs(t, fmt.Errorf("turn: %w", err), &mre)
	assert.Equal(t, 7, mre.Rounds)
}
