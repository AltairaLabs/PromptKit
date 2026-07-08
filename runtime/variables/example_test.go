package variables

import (
	"context"
	"fmt"
)

// staticProvider is a minimal, keyless Provider used only to demonstrate
// ChainProvider's override semantics without any network dependency.
type staticProvider struct {
	name string
	vars map[string]string
}

func (p *staticProvider) Name() string { return p.name }

func (p *staticProvider) Provide(context.Context) (map[string]string, error) {
	return p.vars, nil
}

// ExampleChain shows composing multiple providers: later providers in the
// chain override variables from earlier ones when keys conflict.
func ExampleChain() {
	base := &staticProvider{name: "base", vars: map[string]string{"greeting": "Hi", "tier": "free"}}
	override := &staticProvider{name: "override", vars: map[string]string{"tier": "premium"}}

	chain := Chain(base, override)

	vars, err := chain.Provide(context.Background())
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(vars["greeting"], vars["tier"])
	// Output: Hi premium
}
