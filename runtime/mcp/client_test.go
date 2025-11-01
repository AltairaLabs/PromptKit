package mcp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultClientOptions(t *testing.T) {
	opts := DefaultClientOptions()

	assert.Equal(t, 30*time.Second, opts.RequestTimeout)
	assert.Equal(t, 10*time.Second, opts.InitTimeout)
	assert.Equal(t, 3, opts.MaxRetries)
	assert.Equal(t, 100*time.Millisecond, opts.RetryDelay)
	assert.True(t, opts.EnableGracefulDegradation)
}

func TestClientErrors(t *testing.T) {
	// Test that error constants are defined
	assert.NotNil(t, ErrClientNotInitialized)
	assert.NotNil(t, ErrClientClosed)
	assert.NotNil(t, ErrServerUnresponsive)
	assert.NotNil(t, ErrProcessDied)

	// Test error messages are meaningful
	assert.Contains(t, ErrClientNotInitialized.Error(), "not initialized")
	assert.Contains(t, ErrClientClosed.Error(), "closed")
	assert.Contains(t, ErrServerUnresponsive.Error(), "unresponsive")
	assert.Contains(t, ErrProcessDied.Error(), "died")
}

func TestNewStdioClient(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
		Args:    []string{"hello"},
	}

	client := NewStdioClient(config)

	assert.NotNil(t, client)
	assert.Equal(t, config.Name, client.config.Name)
	assert.Equal(t, config.Command, client.config.Command)
	assert.Equal(t, DefaultClientOptions(), client.options)
}

func TestNewStdioClientWithOptions(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}

	customOpts := ClientOptions{
		RequestTimeout:            5 * time.Second,
		InitTimeout:               2 * time.Second,
		MaxRetries:                5,
		RetryDelay:                50 * time.Millisecond,
		EnableGracefulDegradation: false,
	}

	client := NewStdioClientWithOptions(config, customOpts)

	assert.NotNil(t, client)
	assert.Equal(t, config.Name, client.config.Name)
	assert.Equal(t, customOpts.RequestTimeout, client.options.RequestTimeout)
	assert.Equal(t, customOpts.InitTimeout, client.options.InitTimeout)
	assert.Equal(t, customOpts.MaxRetries, client.options.MaxRetries)
	assert.Equal(t, customOpts.RetryDelay, client.options.RetryDelay)
	assert.False(t, client.options.EnableGracefulDegradation)
}
