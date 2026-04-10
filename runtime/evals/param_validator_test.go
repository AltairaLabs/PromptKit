package evals

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// stubParamValidator is a minimal handler implementing ParamValidator
// for interface-satisfaction assertions.
type stubParamValidator struct {
	err error
}

func (s *stubParamValidator) ValidateParams(_ map[string]any) error {
	return s.err
}

func TestParamValidatorInterface(t *testing.T) {
	// Compile-time check: the stub satisfies ParamValidator.
	var _ ParamValidator = (*stubParamValidator)(nil)

	s := &stubParamValidator{err: errors.New("bad params")}
	err := s.ValidateParams(map[string]any{})
	assert.EqualError(t, err, "bad params")

	ok := &stubParamValidator{}
	assert.NoError(t, ok.ValidateParams(nil))
}
