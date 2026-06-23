package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/AltairaLabs/PromptKit/tools/arena/inspect"
)

// outputText renders inspection data as styled terminal output. The second
// parameter is unused (kept for backward compatibility with tests that call
// outputText(data, nil)).
func outputText(data *inspect.InspectionData, _ interface{}) error {
	fmt.Print(inspect.RenderText(data, inspect.RenderOptions{
		Verbose: inspectVerbose,
		Section: inspectSection,
		Stats:   inspectStats,
	}))
	return nil
}

// outputJSON encodes inspection data as indented JSON to stdout.
func outputJSON(data *inspect.InspectionData) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}
