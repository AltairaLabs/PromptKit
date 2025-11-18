package policy

import (
	"fmt"
	"time"
)

// PolicyConfig defines a retention policy for media storage.
// Policies control how long media should be retained and when it should be deleted.
type PolicyConfig struct {
	// Name is a unique identifier for this policy (e.g., "delete-after-5min", "retain-30days")
	Name string `json:"name" yaml:"name"`

	// Description provides human-readable documentation for this policy
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Rules contains policy-specific configuration (e.g., retention duration)
	Rules map[string]interface{} `json:"rules,omitempty" yaml:"rules,omitempty"`
}

// PolicyMetadata stores policy information in .meta files alongside media.
// This metadata is used by the enforcement system to determine when to delete files.
type PolicyMetadata struct {
	// PolicyName identifies the policy applied to this media
	PolicyName string `json:"policy_name"`

	// ExpiresAt is when this media should be deleted (nil = never expires)
	ExpiresAt *time.Time `json:"expires_at,omitempty"`

	// CreatedAt is when the policy was applied
	CreatedAt time.Time `json:"created_at"`
}

// ParsePolicyName extracts policy type and parameters from a policy name.
// Supported formats:
//   - "delete-after-Xmin" - Delete after X minutes
//   - "retain-Xdays" - Retain for X days
//   - "retain-Xhours" - Retain for X hours
//
// Returns (policyType, duration, error)
func ParsePolicyName(name string) (string, time.Duration, error) {
	if name == "" {
		return "", 0, fmt.Errorf("empty policy name")
	}

	// Parse time-based policy names
	var duration time.Duration
	var policyType string

	// Try "delete-after-Xmin" format
	var minutes int
	n, _ := fmt.Sscanf(name, "delete-after-%dmin", &minutes)
	if n == 1 && fmt.Sprintf("delete-after-%dmin", minutes) == name {
		policyType = "delete-after"
		duration = time.Duration(minutes) * time.Minute
		return policyType, duration, nil
	}

	// Try "retain-Xhours" format
	var hours int
	n, _ = fmt.Sscanf(name, "retain-%dhours", &hours)
	if n == 1 && fmt.Sprintf("retain-%dhours", hours) == name {
		policyType = "retain"
		duration = time.Duration(hours) * time.Hour
		return policyType, duration, nil
	}

	// Try "retain-Xdays" format
	var days int
	n, _ = fmt.Sscanf(name, "retain-%ddays", &days)
	if n == 1 && fmt.Sprintf("retain-%ddays", days) == name {
		policyType = "retain"
		duration = time.Duration(days) * 24 * time.Hour
		return policyType, duration, nil
	}

	return "", 0, fmt.Errorf("unsupported policy name format: %s", name)
}

// Validate checks if a policy configuration is valid.
func (p *PolicyConfig) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("policy name cannot be empty")
	}

	// Try to parse the policy name to ensure it's valid
	_, _, err := ParsePolicyName(p.Name)
	if err != nil {
		return fmt.Errorf("invalid policy name: %w", err)
	}

	return nil
}
