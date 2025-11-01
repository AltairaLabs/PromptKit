package engine

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// RunPlan defines the test execution plan
type RunPlan struct {
	Combinations []RunCombination
}

// RunCombination represents a single test execution
type RunCombination struct {
	Region     string
	ScenarioID string
	ProviderID string
}

// RunResult contains the complete results of a single test execution
type RunResult struct {
	RunID      string                  `json:"RunID"`
	PromptPack string                  `json:"PromptPack"`
	Region     string                  `json:"Region"`
	ScenarioID string                  `json:"ScenarioID"`
	ProviderID string                  `json:"ProviderID"`
	Params     map[string]interface{}  `json:"Params"`
	Messages   []types.Message         `json:"Messages"`
	Commit     map[string]interface{}  `json:"Commit"`
	Cost       types.CostInfo          `json:"Cost"`
	ToolStats  *types.ToolStats        `json:"ToolStats"`
	Violations []types.ValidationError `json:"Violations"`
	StartTime  time.Time               `json:"StartTime"`
	EndTime    time.Time               `json:"EndTime"`
	Duration   time.Duration           `json:"Duration"`
	Error      string                  `json:"Error"`
	SelfPlay   bool                    `json:"SelfPlay"`
	PersonaID  string                  `json:"PersonaID"`

	UserFeedback  *statestore.Feedback `json:"UserFeedback"`
	SessionTags   []string             `json:"SessionTags"`
	AssistantRole *SelfPlayRoleInfo    `json:"AssistantRole"`
	UserRole      *SelfPlayRoleInfo    `json:"UserRole"`
}

// SelfPlayRoleInfo contains provider information for self-play roles
type SelfPlayRoleInfo struct {
	Provider string
	Model    string
	Region   string
}
