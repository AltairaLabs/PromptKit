package sdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateSemanticVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		// Valid versions
		{
			name:    "valid semver - basic",
			version: "1.0.0",
			wantErr: false,
		},
		{
			name:    "valid semver - with v prefix",
			version: "v1.0.0",
			wantErr: false,
		},
		{
			name:    "valid semver - with prerelease",
			version: "1.0.0-alpha",
			wantErr: false,
		},
		{
			name:    "valid semver - with prerelease and build metadata",
			version: "1.0.0-alpha+build.123",
			wantErr: false,
		},
		{
			name:    "valid semver - with build metadata",
			version: "1.0.0+build.123",
			wantErr: false,
		},
		{
			name:    "valid semver - high version numbers",
			version: "10.20.30",
			wantErr: false,
		},
		{
			name:    "valid semver - complex prerelease",
			version: "1.0.0-beta.1",
			wantErr: false,
		},

		// Invalid versions
		{
			name:    "invalid - incomplete version (two parts)",
			version: "1.0",
			wantErr: true,
		},
		{
			name:    "invalid - incomplete version (one part)",
			version: "1",
			wantErr: true,
		},
		{
			name:    "invalid - incomplete version with v prefix",
			version: "v1.0",
			wantErr: true,
		},
		{
			name:    "invalid - empty string",
			version: "",
			wantErr: true,
		},
		{
			name:    "invalid - just v",
			version: "v",
			wantErr: true,
		},
		{
			name:    "invalid - non-numeric version",
			version: "latest",
			wantErr: true,
		},
		{
			name:    "invalid - arbitrary text",
			version: "version-1",
			wantErr: true,
		},
		{
			name:    "invalid - negative numbers",
			version: "-1.0.0",
			wantErr: true,
		},
		{
			name:    "invalid - spaces",
			version: "1 .0. 0",
			wantErr: true,
		},
		{
			name:    "invalid - leading zeros",
			version: "01.0.0",
			wantErr: true,
		},
		{
			name:    "invalid - special characters",
			version: "1.0.0!",
			wantErr: true,
		},
		{
			name:    "invalid - four part version",
			version: "1.0.0.0",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSemanticVersion(tt.version)
			if tt.wantErr {
				assert.Error(t, err, "Expected error for version: %s", tt.version)
			} else {
				assert.NoError(t, err, "Expected no error for version: %s", tt.version)
			}
		})
	}
}
