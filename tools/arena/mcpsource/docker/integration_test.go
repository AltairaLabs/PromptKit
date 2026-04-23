//go:build integration_docker

package docker

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Run with:
//
//	docker pull ghcr.io/altairalabs/codegen-sandbox:latest
//	go -C tools/arena test ./mcpsource/docker/... -tags=integration_docker -v -count=1
func TestSource_AgainstRealDocker(t *testing.T) {
	image := os.Getenv("PROMPTKIT_SANDBOX_IMAGE")
	if image == "" {
		image = "ghcr.io/altairalabs/codegen-sandbox:latest"
	}

	s := New()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, closer, err := s.Open(ctx, map[string]any{
		"image": image,
		"env":   map[string]any{"DEV_MODE": "1"},
	})
	require.NoError(t, err)
	defer func() { _ = closer.Close() }()

	assert.Contains(t, conn.URL, "http://localhost:")
}
