package policy_test

import (
"testing"
"time"

"github.com/AltairaLabs/PromptKit/runtime/storage/policy"
"github.com/stretchr/testify/assert"
"github.com/stretchr/testify/require"
)

func TestParsePolicyName(t *testing.T) {
	tests := []struct {
		name         string
		policyName   string
		wantType     string
		wantDuration time.Duration
		wantError    bool
	}{
		{
			name:         "delete after 5 minutes",
			policyName:   "delete-after-5min",
			wantType:     "delete-after",
			wantDuration: 5 * time.Minute,
			wantError:    false,
		},
		{
			name:         "delete after 60 minutes",
			policyName:   "delete-after-60min",
			wantType:     "delete-after",
			wantDuration: 60 * time.Minute,
			wantError:    false,
		},
		{
			name:         "retain 30 days",
			policyName:   "retain-30days",
			wantType:     "retain",
			wantDuration: 30 * 24 * time.Hour,
			wantError:    false,
		},
		{
			name:         "retain 1 day",
			policyName:   "retain-1days",
			wantType:     "retain",
			wantDuration: 24 * time.Hour,
			wantError:    false,
		},
		{
			name:         "retain 2 hours",
			policyName:   "retain-2hours",
			wantType:     "retain",
			wantDuration: 2 * time.Hour,
			wantError:    false,
		},
		{
			name:         "empty policy name",
			policyName:   "",
			wantType:     "",
			wantDuration: 0,
			wantError:    true,
		},
		{
			name:         "invalid format",
			policyName:   "keep-forever",
			wantType:     "",
			wantDuration: 0,
			wantError:    true,
		},
		{
			name:         "malformed duration",
			policyName:   "delete-after-XYZmin",
			wantType:     "",
			wantDuration: 0,
			wantError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
gotType, gotDuration, err := policy.ParsePolicyName(tt.policyName)

if tt.wantError {
assert.Error(t, err)
return
}

require.NoError(t, err)
assert.Equal(t, tt.wantType, gotType)
assert.Equal(t, tt.wantDuration, gotDuration)
})
	}
}

func TestPolicyConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		config    policy.PolicyConfig
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid delete-after policy",
			config: policy.PolicyConfig{
				Name:        "delete-after-5min",
				Description: "Delete after 5 minutes",
			},
			wantError: false,
		},
		{
			name: "valid retain policy",
			config: policy.PolicyConfig{
				Name:        "retain-30days",
				Description: "Retain for 30 days",
			},
			wantError: false,
		},
		{
			name: "empty name",
			config: policy.PolicyConfig{
				Name: "",
			},
			wantError: true,
			errorMsg:  "policy name cannot be empty",
		},
		{
			name: "invalid name format",
			config: policy.PolicyConfig{
				Name: "invalid-format",
			},
			wantError: true,
			errorMsg:  "invalid policy name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
err := tt.config.Validate()

			if tt.wantError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
				return
			}

			assert.NoError(t, err)
		})
	}
}

func TestPolicyMetadata(t *testing.T) {
	t.Run("creates policy metadata", func(t *testing.T) {
now := time.Now()
		expiresAt := now.Add(5 * time.Minute)

		metadata := policy.PolicyMetadata{
			PolicyName: "delete-after-5min",
			ExpiresAt:  &expiresAt,
			CreatedAt:  now,
		}

		assert.Equal(t, "delete-after-5min", metadata.PolicyName)
		assert.NotNil(t, metadata.ExpiresAt)
		assert.Equal(t, expiresAt, *metadata.ExpiresAt)
		assert.Equal(t, now, metadata.CreatedAt)
	})

	t.Run("creates policy metadata without expiration", func(t *testing.T) {
now := time.Now()

		metadata := policy.PolicyMetadata{
			PolicyName: "retain-forever",
			ExpiresAt:  nil,
			CreatedAt:  now,
		}

		assert.Equal(t, "retain-forever", metadata.PolicyName)
		assert.Nil(t, metadata.ExpiresAt)
		assert.Equal(t, now, metadata.CreatedAt)
	})
}
