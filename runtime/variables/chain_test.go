package variables

import (
	"context"
	"errors"
	"testing"
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

func (m *mockProvider) Provide(ctx context.Context) (map[string]string, error) {
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
			got, err := c.Provide(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("ChainProvider.Provide() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Errorf("ChainProvider.Provide() got %d vars, want %d", len(got), len(tt.want))
					return
				}

				for k, v := range tt.want {
					if got[k] != v {
						t.Errorf("ChainProvider.Provide()[%s] = %v, want %v", k, got[k], v)
					}
				}
			}
		})
	}
}

func TestChain_Add(t *testing.T) {
	c := Chain()
	p1 := &mockProvider{name: "p1", vars: map[string]string{"a": "1"}}
	p2 := &mockProvider{name: "p2", vars: map[string]string{"b": "2"}}

	c.Add(p1).Add(p2)

	providers := c.Providers()
	if len(providers) != 2 {
		t.Errorf("ChainProvider.Providers() got %d, want 2", len(providers))
	}
}

func TestChain_AllProvidersCalled(t *testing.T) {
	p1 := &mockProvider{name: "p1", vars: map[string]string{"a": "1"}}
	p2 := &mockProvider{name: "p2", vars: map[string]string{"b": "2"}}

	c := Chain(p1, p2)
	_, _ = c.Provide(context.Background())

	if !p1.called {
		t.Error("first provider was not called")
	}
	if !p2.called {
		t.Error("second provider was not called")
	}
}
