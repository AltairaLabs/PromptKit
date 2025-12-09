package variables

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
)

// mockProvider is a test helper that returns predefined values
type mockProvider struct {
	name   string
	vars   map[string]string
	err    error
	called bool
}

func (m *mockProvider) Name() string {
	return m.name
}

func (m *mockProvider) Provide(ctx context.Context, state *statestore.ConversationState) (map[string]string, error) {
	m.called = true
	return m.vars, m.err
}

func TestChain_Name(t *testing.T) {
	c := Chain()
	if got := c.Name(); got != "chain" {
		t.Errorf("ChainProvider.Name() = %v, want %v", got, "chain")
	}
}

func TestChain_Provide(t *testing.T) {
	tests := []struct {
		name      string
		providers []Provider
		want      map[string]string
		wantErr   bool
	}{
		{
			name:      "empty chain returns empty map",
			providers: nil,
			want:      map[string]string{},
		},
		{
			name: "single provider",
			providers: []Provider{
				&mockProvider{
					name: "mock1",
					vars: map[string]string{"key1": "value1"},
				},
			},
			want: map[string]string{"key1": "value1"},
		},
		{
			name: "multiple providers merge",
			providers: []Provider{
				&mockProvider{
					name: "mock1",
					vars: map[string]string{"key1": "value1"},
				},
				&mockProvider{
					name: "mock2",
					vars: map[string]string{"key2": "value2"},
				},
			},
			want: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name: "later provider overrides earlier",
			providers: []Provider{
				&mockProvider{
					name: "mock1",
					vars: map[string]string{"key": "first"},
				},
				&mockProvider{
					name: "mock2",
					vars: map[string]string{"key": "second"},
				},
			},
			want: map[string]string{"key": "second"},
		},
		{
			name: "provider error stops chain",
			providers: []Provider{
				&mockProvider{
					name: "mock1",
					vars: map[string]string{"key1": "value1"},
				},
				&mockProvider{
					name: "failing",
					err:  errors.New("provider error"),
				},
			},
			wantErr: true,
		},
		{
			name: "provider returning nil is ok",
			providers: []Provider{
				&mockProvider{
					name: "mock1",
					vars: map[string]string{"key1": "value1"},
				},
				&mockProvider{
					name: "mock2",
					vars: nil,
				},
			},
			want: map[string]string{"key1": "value1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Chain(tt.providers...)
			got, err := c.Provide(context.Background(), nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("ChainProvider.Provide() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("ChainProvider.Provide() got %d vars, want %d", len(got), len(tt.want))
				return
			}

			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("ChainProvider.Provide()[%s] = %v, want %v", k, got[k], v)
				}
			}
		})
	}
}

func TestChain_Add(t *testing.T) {
	c := Chain()
	if len(c.Providers()) != 0 {
		t.Errorf("new chain should have 0 providers, got %d", len(c.Providers()))
	}

	mock1 := &mockProvider{name: "mock1", vars: map[string]string{"key1": "value1"}}
	mock2 := &mockProvider{name: "mock2", vars: map[string]string{"key2": "value2"}}

	c.Add(mock1).Add(mock2)

	if len(c.Providers()) != 2 {
		t.Errorf("chain should have 2 providers, got %d", len(c.Providers()))
	}

	// Verify order is preserved
	got, _ := c.Provide(context.Background(), nil)
	if got["key1"] != "value1" || got["key2"] != "value2" {
		t.Errorf("chain.Add() should preserve order")
	}
}

func TestChain_ErrorMessage(t *testing.T) {
	c := Chain(
		&mockProvider{name: "failing", err: errors.New("test error")},
	)

	_, err := c.Provide(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}

	// Error message should include provider name
	if !errors.Is(err, err) || err.Error() != "provider failing failed: test error" {
		t.Errorf("error message should include provider name, got: %v", err)
	}
}
