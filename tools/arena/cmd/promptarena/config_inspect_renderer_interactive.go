package main

import (
	"encoding/json"
	"os"

	"github.com/AltairaLabs/PromptKit/tools/arena/inspect"
)

// outputJSON encodes inspection data as indented JSON to stdout.
func outputJSON(data *inspect.InspectionData) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}
