// Package stage provides the reactive streams architecture for pipeline execution.
package stage

import (
	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// ValidateCapabilities checks format compatibility between connected stages.
// It logs warnings for potential mismatches but does not return errors,
// as format compatibility can often only be fully determined at runtime.
//
// This function is called during pipeline building to provide early feedback
// about potential issues.
func ValidateCapabilities(stages []Stage, edges map[string][]string) {
	// Build stage lookup map
	stageMap := make(map[string]Stage)
	for _, s := range stages {
		stageMap[s.Name()] = s
	}

	// Check each edge for format compatibility
	for fromName, toNames := range edges {
		fromStage := stageMap[fromName]
		if fromStage == nil {
			continue
		}

		for _, toName := range toNames {
			toStage := stageMap[toName]
			if toStage == nil {
				continue
			}

			checkStageCompatibility(fromStage, toStage)
		}
	}
}

// checkStageCompatibility checks if two connected stages have compatible formats.
func checkStageCompatibility(from, to Stage) {
	// Get capabilities if stages implement FormatCapable
	fromCap, fromOK := from.(FormatCapable)
	toCap, toOK := to.(FormatCapable)

	// If neither implements FormatCapable, assume compatible
	if !fromOK && !toOK {
		return
	}

	// Get output capabilities from source stage
	var outputCaps Capabilities
	if fromOK {
		outputCaps = fromCap.OutputCapabilities()
	} else {
		outputCaps = AnyCapabilities()
	}

	// Get input capabilities from destination stage
	var inputCaps Capabilities
	if toOK {
		inputCaps = toCap.InputCapabilities()
	} else {
		inputCaps = AnyCapabilities()
	}

	// Check content type compatibility
	if !contentTypesCompatible(outputCaps.ContentTypes, inputCaps.ContentTypes) {
		logger.Warn("Pipeline format mismatch: content type incompatibility",
			"from_stage", from.Name(),
			"to_stage", to.Name(),
			"from_produces", contentTypesToStrings(outputCaps.ContentTypes),
			"to_accepts", contentTypesToStrings(inputCaps.ContentTypes),
		)
	}

	// Check audio format compatibility if both have audio capabilities
	if outputCaps.Audio != nil && inputCaps.Audio != nil {
		checkAudioCompatibility(from.Name(), to.Name(), outputCaps.Audio, inputCaps.Audio)
	}
}

// contentTypesCompatible checks if output content types are compatible with input.
func contentTypesCompatible(output, input []ContentType) bool {
	// Empty means "any" - always compatible
	if len(output) == 0 || len(input) == 0 {
		return true
	}

	// Check if any output type is accepted by input
	for _, outType := range output {
		if outType == ContentTypeAny {
			return true
		}
		for _, inType := range input {
			if inType == ContentTypeAny || outType == inType {
				return true
			}
		}
	}

	return false
}

// checkAudioCompatibility checks if audio capabilities are compatible.
func checkAudioCompatibility(fromName, toName string, output, input *AudioCapability) {
	// Check format compatibility
	if !audioFormatsOverlap(output.Formats, input.Formats) {
		logger.Warn("Pipeline format mismatch: audio format incompatibility",
			"from_stage", fromName,
			"to_stage", toName,
			"from_produces", audioFormatsToStrings(output.Formats),
			"to_accepts", audioFormatsToStrings(input.Formats),
		)
	}

	// Check sample rate compatibility
	if !intsOverlap(output.SampleRates, input.SampleRates) {
		logger.Warn("Pipeline format mismatch: sample rate incompatibility",
			"from_stage", fromName,
			"to_stage", toName,
			"from_produces", output.SampleRates,
			"to_accepts", input.SampleRates,
		)
	}

	// Check channel compatibility
	if !intsOverlap(output.Channels, input.Channels) {
		logger.Warn("Pipeline format mismatch: channel count incompatibility",
			"from_stage", fromName,
			"to_stage", toName,
			"from_produces", output.Channels,
			"to_accepts", input.Channels,
		)
	}
}

// audioFormatsOverlap returns true if the two format slices have any overlap.
// Returns true if either slice is empty (meaning "any").
func audioFormatsOverlap(a, b []AudioFormat) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	for _, af := range a {
		for _, bf := range b {
			if af == bf {
				return true
			}
		}
	}
	return false
}

// intsOverlap returns true if the two int slices have any overlap.
// Returns true if either slice is empty (meaning "any").
func intsOverlap(a, b []int) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	for _, ai := range a {
		for _, bi := range b {
			if ai == bi {
				return true
			}
		}
	}
	return false
}

// contentTypesToStrings converts content types to string slice for logging.
func contentTypesToStrings(types []ContentType) []string {
	result := make([]string, len(types))
	for i, t := range types {
		result[i] = t.String()
	}
	return result
}

// audioFormatsToStrings converts audio formats to string slice for logging.
func audioFormatsToStrings(formats []AudioFormat) []string {
	result := make([]string, len(formats))
	for i, f := range formats {
		result[i] = f.String()
	}
	return result
}

// DescribeCapabilities returns a human-readable description of a stage's capabilities.
// Useful for debugging and logging.
func DescribeCapabilities(stage Stage) string {
	fc, ok := stage.(FormatCapable)
	if !ok {
		return stage.Name() + ": no format capabilities declared (accepts any)"
	}

	input := fc.InputCapabilities()
	output := fc.OutputCapabilities()

	return stage.Name() + ": " +
		"accepts=" + describeCapability(input) +
		" produces=" + describeCapability(output)
}

func describeCapability(caps Capabilities) string {
	if len(caps.ContentTypes) == 0 {
		return contentTypeAny
	}

	result := joinContentTypes(caps.ContentTypes)
	if caps.Audio != nil {
		result += "(" + describeAudioCapability(caps.Audio) + ")"
	}
	return result
}

func joinContentTypes(types []ContentType) string {
	strs := make([]string, len(types))
	for i, ct := range types {
		strs[i] = ct.String()
	}
	return joinStrings(strs, "|")
}

func describeAudioCapability(audio *AudioCapability) string {
	if audio == nil {
		return ""
	}
	result := ""
	if len(audio.Formats) > 0 {
		result = joinStrings(audioFormatsToStrings(audio.Formats), "|")
	}
	return result
}

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}
