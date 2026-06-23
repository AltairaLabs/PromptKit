package app

import (
	"errors"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

// EnsureEngine returns the cached engine from ctx, building it from ctx.Config
// if it has not been built yet. It also sets ctx.StateStore from the engine.
// Returns an error if no config has been loaded.
func (c *AppContext) EnsureEngine() (*engine.Engine, error) {
	if c.Engine != nil {
		return c.Engine, nil
	}
	if c.Config == nil {
		return nil, errors.New("no config loaded")
	}
	eng, err := engine.NewEngineFromConfig(c.Config)
	if err != nil {
		return nil, err
	}
	c.Engine = eng
	c.StateStore = eng.GetStateStore()
	return c.Engine, nil
}
