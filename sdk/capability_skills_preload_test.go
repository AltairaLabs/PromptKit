package sdk

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/skills"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
)

func preloadInlineSources(names ...string) []skills.SkillSource {
	srcs := make([]skills.SkillSource, 0, len(names))
	for _, n := range names {
		srcs = append(srcs, skills.SkillSource{
			Name:         n,
			Description:  n + " skill",
			Instructions: "do " + n,
			Preload:      true,
		})
	}
	return srcs
}

// initSkillsCapability initializes the capability and materializes one
// conversation's ActiveSet, which is where preloading now happens: Init only
// records which skills preload, because activating them at Init put them in a
// set every conversation shared (#2011).
func initSkillsCapability(t *testing.T, c *SkillsCapability) {
	t.Helper()
	if err := c.Init(CapabilityContext{Pack: &pack.Pack{}, PromptName: "chat"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	c.NewActiveSet()
}

func TestSkillsCapability_PreloadBlockedByMaxActive_IsLogged(t *testing.T) {
	var buf bytes.Buffer
	logger.SetLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { logger.SetLogger(nil) })

	// Four skills want preloading; only two slots exist. The two that lose
	// are not recoverable on first use either — the limit is still full.
	cap := NewSkillsCapability(
		preloadInlineSources("alpha", "bravo", "charlie", "delta"),
		WithMaxActiveSkills(2),
	)
	initSkillsCapability(t, cap)

	out := buf.String()
	if !strings.Contains(out, "preload") {
		t.Fatalf("blocked preloads were not reported at warn: %q", out)
	}
	for _, name := range []string{"charlie", "delta"} {
		if !strings.Contains(out, name) {
			t.Errorf("skill %q was dropped from the preload set without being named: %q", name, out)
		}
	}
}

func TestSkillsCapability_PreloadWithinMaxActive_IsQuiet(t *testing.T) {
	var buf bytes.Buffer
	logger.SetLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { logger.SetLogger(nil) })

	cap := NewSkillsCapability(
		preloadInlineSources("alpha", "bravo"),
		WithMaxActiveSkills(4),
	)
	initSkillsCapability(t, cap)

	if out := buf.String(); out != "" {
		t.Errorf("preloads that all succeeded should log nothing at warn, got %q", out)
	}
}
