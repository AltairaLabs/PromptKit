package main

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// applyProviderOverrides rewrites each `from` provider in cfg.LoadedProviders
// with the spec of its `to` provider, in place, keeping the original ID/key.
// Both ids must be defined providers; a typo hard-errors rather than silently
// leaving the original provider in place.
func applyProviderOverrides(cfg *config.Config, pairs []string) error {
	for _, p := range pairs {
		from, to, err := parseOverridePair(p)
		if err != nil {
			return err
		}
		dst, ok := cfg.LoadedProviders[from]
		if !ok {
			return fmt.Errorf(
				"--override-provider %s=%s: unknown source provider %q (must be defined in spec.providers)",
				from, to, from)
		}
		src, ok := cfg.LoadedProviders[to]
		if !ok {
			return fmt.Errorf(
				"--override-provider %s=%s: unknown target provider %q (must be defined in spec.providers)",
				from, to, to)
		}
		overrideProviderSpec(dst, src)
	}
	return nil
}

// parseOverridePair splits a "from=to" string into its two non-empty halves.
func parseOverridePair(s string) (from, to string, err error) {
	from, to, found := strings.Cut(s, "=")
	if !found || from == "" || to == "" {
		return "", "", fmt.Errorf("invalid --override-provider %q: expected from=to", s)
	}
	return from, to, nil
}

// overrideProviderSpec copies src's spec into dst in place, keeping dst's ID.
func overrideProviderSpec(dst, src *config.Provider) {
	id := dst.ID
	*dst = *src
	dst.ID = id
}
