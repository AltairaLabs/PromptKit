package gemini

import (
	"errors"
	"testing"
)

func TestGeminiAPIError_Error(t *testing.T) {
	err := &GeminiAPIError{
		Code:    400,
		Message: "Invalid audio format",
		Status:  "INVALID_ARGUMENT",
	}

	expected := "gemini api error (code 400, status INVALID_ARGUMENT): Invalid audio format"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestGeminiAPIError_IsRetryable(t *testing.T) {
	tests := []struct {
		name    string
		code    int
		want    bool
	}{
		{"rate limit", 429, true},
		{"internal server error", 500, true},
		{"service unavailable", 503, true},
		{"bad request", 400, false},
		{"unauthorized", 401, false},
		{"not found", 404, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &GeminiAPIError{Code: tt.code}
			if got := err.IsRetryable(); got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGeminiAPIError_IsAuthError(t *testing.T) {
	tests := []struct {
		name string
		code int
		want bool
	}{
		{"unauthorized", 401, true},
		{"forbidden", 403, false},
		{"bad request", 400, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &GeminiAPIError{Code: tt.code}
			if got := err.IsAuthError(); got != tt.want {
				t.Errorf("IsAuthError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGeminiAPIError_IsPolicyViolation(t *testing.T) {
	tests := []struct {
		name   string
		code   int
		status string
		want   bool
	}{
		{"policy violation", 400, "POLICY_VIOLATION", true},
		{"invalid argument", 400, "INVALID_ARGUMENT", false},
		{"unauthorized", 401, "UNAUTHENTICATED", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &GeminiAPIError{Code: tt.code, Status: tt.status}
			if got := err.IsPolicyViolation(); got != tt.want {
				t.Errorf("IsPolicyViolation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name    string
		apiErr  *GeminiAPIError
		wantErr error
		wantNil bool
	}{
		{
			name:    "nil error",
			apiErr:  nil,
			wantNil: true,
		},
		{
			name: "policy violation",
			apiErr: &GeminiAPIError{
				Code:    400,
				Message: "Content blocked",
				Status:  "POLICY_VIOLATION",
			},
			wantErr: ErrPolicyViolation,
		},
		{
			name: "invalid request",
			apiErr: &GeminiAPIError{
				Code:    400,
				Message: "Bad format",
				Status:  "INVALID_ARGUMENT",
			},
			wantErr: ErrInvalidRequest,
		},
		{
			name: "authentication failed",
			apiErr: &GeminiAPIError{
				Code:    401,
				Message: "Invalid API key",
				Status:  "UNAUTHENTICATED",
			},
			wantErr: ErrAuthenticationFailed,
		},
		{
			name: "rate limit",
			apiErr: &GeminiAPIError{
				Code:    429,
				Message: "Too many requests",
				Status:  "RESOURCE_EXHAUSTED",
			},
			wantErr: ErrRateLimitExceeded,
		},
		{
			name: "service unavailable",
			apiErr: &GeminiAPIError{
				Code:    503,
				Message: "Service down",
				Status:  "UNAVAILABLE",
			},
			wantErr: ErrServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ClassifyError(tt.apiErr)

			if tt.wantNil {
				if err != nil {
					t.Errorf("expected nil error, got %v", err)
				}
				return
			}

			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("expected error to wrap %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestDetermineRecoveryStrategy(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want RecoveryStrategy
	}{
		{
			name: "nil error",
			err:  nil,
			want: RecoveryRetry,
		},
		{
			name: "authentication error",
			err:  ErrAuthenticationFailed,
			want: RecoveryFailFast,
		},
		{
			name: "policy violation",
			err:  ErrPolicyViolation,
			want: RecoveryFailFast,
		},
		{
			name: "rate limit",
			err:  ErrRateLimitExceeded,
			want: RecoveryWaitAndRetry,
		},
		{
			name: "service unavailable",
			err:  ErrServiceUnavailable,
			want: RecoveryWaitAndRetry,
		},
		{
			name: "retryable API error",
			err:  &GeminiAPIError{Code: 429},
			want: RecoveryWaitAndRetry,
		},
		{
			name: "auth API error",
			err:  &GeminiAPIError{Code: 401},
			want: RecoveryFailFast,
		},
		{
			name: "policy API error",
			err:  &GeminiAPIError{Code: 400, Status: "POLICY_VIOLATION"},
			want: RecoveryFailFast,
		},
		{
			name: "unknown error",
			err:  errors.New("unknown"),
			want: RecoveryGracefulDegradation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineRecoveryStrategy(tt.err)
			if got != tt.want {
				t.Errorf("DetermineRecoveryStrategy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPromptFeedback_IsBlocked(t *testing.T) {
	tests := []struct {
		name     string
		feedback PromptFeedback
		want     bool
	}{
		{
			name:     "not blocked",
			feedback: PromptFeedback{BlockReason: ""},
			want:     false,
		},
		{
			name:     "blocked",
			feedback: PromptFeedback{BlockReason: "SAFETY"},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.feedback.IsBlocked(); got != tt.want {
				t.Errorf("IsBlocked() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPromptFeedback_GetBlockReason(t *testing.T) {
	tests := []struct {
		name     string
		feedback PromptFeedback
		want     string
	}{
		{
			name:     "not blocked",
			feedback: PromptFeedback{BlockReason: ""},
			want:     "none",
		},
		{
			name:     "safety block",
			feedback: PromptFeedback{BlockReason: "SAFETY"},
			want:     "SAFETY",
		},
		{
			name:     "other block",
			feedback: PromptFeedback{BlockReason: "OTHER"},
			want:     "OTHER",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.feedback.GetBlockReason(); got != tt.want {
				t.Errorf("GetBlockReason() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRecoveryStrategyNames(t *testing.T) {
	// Just verify the constants exist and can be compared
	strategies := []RecoveryStrategy{
		RecoveryRetry,
		RecoveryFailFast,
		RecoveryGracefulDegradation,
		RecoveryWaitAndRetry,
	}

	for i, s1 := range strategies {
		for j, s2 := range strategies {
			equal := (s1 == s2)
			shouldEqual := (i == j)
			if equal != shouldEqual {
				t.Errorf("strategies[%d] == strategies[%d] = %v, want %v", i, j, equal, shouldEqual)
			}
		}
	}
}
