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

func TestCheckHealth(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}

	t.Run("not initialized", func(t *testing.T) {
		client := NewStdioClient(config)
		err := client.checkHealth()
		assert.Equal(t, ErrClientNotInitialized, err)
	})

	t.Run("closed client", func(t *testing.T) {
		client := NewStdioClient(config)
		client.closed = true
		err := client.checkHealth()
		assert.Equal(t, ErrClientClosed, err)
	})

	t.Run("started but no process", func(t *testing.T) {
		client := NewStdioClient(config)
		client.started = true
		err := client.checkHealth()
		assert.Equal(t, ErrProcessDied, err)
	})
}

func TestIsAlive(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}

	t.Run("not started", func(t *testing.T) {
		client := NewStdioClient(config)
		assert.False(t, client.IsAlive())
	})

	t.Run("closed", func(t *testing.T) {
		client := NewStdioClient(config)
		client.started = true
		client.closed = true
		assert.False(t, client.IsAlive())
	})
}

func TestHandleNotification(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}
	client := NewStdioClient(config)

	// Test various notification types - these just log, so we're testing they don't panic
	t.Run("tools list changed", func(t *testing.T) {
		msg := &JSONRPCMessage{Method: "notifications/tools/list_changed"}
		client.handleNotification(msg) // Should not panic
	})

	t.Run("resources list changed", func(t *testing.T) {
		msg := &JSONRPCMessage{Method: "notifications/resources/list_changed"}
		client.handleNotification(msg) // Should not panic
	})

	t.Run("unknown notification", func(t *testing.T) {
		msg := &JSONRPCMessage{Method: "some/unknown/notification"}
		client.handleNotification(msg) // Should not panic
	})
}

func TestHandleMessage(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Command: "echo",
	}
	client := NewStdioClient(config)

	t.Run("invalid ID type", func(t *testing.T) {
		// ID is a string instead of a number - should log warning and return
		msg := &JSONRPCMessage{ID: "not-a-number"}
		client.handleMessage(msg) // Should not panic
	})

	t.Run("notification message", func(t *testing.T) {
		msg := &JSONRPCMessage{Method: "some/notification"}
		client.handleMessage(msg) // Should route to handleNotification
	})

	t.Run("response with valid ID but no pending request", func(t *testing.T) {
		msg := &JSONRPCMessage{ID: float64(123)}
		client.handleMessage(msg) // Should handle gracefully
	})
}
