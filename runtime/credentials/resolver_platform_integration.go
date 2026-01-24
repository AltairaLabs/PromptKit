package credentials

import (
	"context"
	"fmt"
	"strings"
)

// resolvePlatformCredential creates credentials for cloud platforms.
func resolvePlatformCredential(ctx context.Context, cfg ResolverConfig) (Credential, error) {
	switch strings.ToLower(cfg.PlatformConfig.Type) {
	case platformBedrock:
		return NewAWSCredential(ctx, cfg.PlatformConfig.Region)
	case platformVertex:
		return NewGCPCredential(ctx, cfg.PlatformConfig.Project, cfg.PlatformConfig.Region)
	case platformAzure:
		return NewAzureCredential(ctx, cfg.PlatformConfig.Endpoint)
	default:
		return nil, fmt.Errorf("unsupported platform type: %s", cfg.PlatformConfig.Type)
	}
}
