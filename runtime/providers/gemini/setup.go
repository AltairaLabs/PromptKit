package gemini

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// Wire-protocol field keys and modality values for the Gemini Live setup message.
const (
	modalityText = "TEXT"
	wireKeyParts = "parts"
	wireKeyText  = "text"
	wireKeyName  = "name"
	wireKeyModel = "model"
)

// getResponseModalities returns modalities with TEXT as default
func getResponseModalities(modalities []string) []string {
	if len(modalities) == 0 {
		return []string{modalityText}
	}
	return modalities
}

// validateModalities checks for invalid modality combinations
func validateModalities(modalities []string) error {
	if len(modalities) > 1 && sliceContains(modalities, modalityText) && sliceContains(modalities, "AUDIO") {
		return fmt.Errorf(
			"invalid response modalities: Gemini Live API does not support TEXT and AUDIO " +
				"simultaneously. Use either [\"TEXT\"] or [\"AUDIO\"], not both")
	}
	return nil
}

// buildSetupMessage constructs the initial setup message for Gemini Live API
func buildSetupMessage(config *StreamSessionConfig, modalities []string) map[string]interface{} {
	modelPath := getModelPath(config.Model)
	generationConfig := buildGenerationConfig(modalities)

	setupContent := map[string]interface{}{
		wireKeyModel:       modelPath,
		"generationConfig": generationConfig,
	}

	addTranscriptionConfig(setupContent, modalities)
	addVADConfig(setupContent, config.VAD)
	addSystemInstruction(setupContent, config.SystemInstruction)
	addToolsConfig(setupContent, config.Tools)

	return map[string]interface{}{
		"setup": setupContent,
	}
}

// getModelPath ensures model is in correct format: models/{model}
func getModelPath(model string) string {
	if model == "" {
		return "models/gemini-2.0-flash-exp"
	}
	if len(model) < 7 || model[:7] != "models/" {
		return "models/" + model
	}
	return model
}

// buildGenerationConfig creates the generation configuration
func buildGenerationConfig(modalities []string) map[string]interface{} {
	config := map[string]interface{}{
		"responseModalities": modalities,
	}

	if sliceContains(modalities, "AUDIO") {
		config["speechConfig"] = map[string]interface{}{
			"voiceConfig": map[string]interface{}{
				"prebuiltVoiceConfig": map[string]interface{}{
					"voiceName": "Puck",
				},
			},
		}
	}

	return config
}

// addTranscriptionConfig adds transcription settings for AUDIO mode
func addTranscriptionConfig(setupContent map[string]interface{}, modalities []string) {
	if sliceContains(modalities, "AUDIO") {
		setupContent["outputAudioTranscription"] = map[string]interface{}{}
		setupContent["inputAudioTranscription"] = map[string]interface{}{}
	}
}

// addVADConfig adds VAD configuration if provided
func addVADConfig(setupContent map[string]interface{}, vad *VADConfig) {
	if vad == nil {
		return
	}

	vadConfig := buildVADConfigMap(vad)
	if len(vadConfig) > 0 {
		setupContent["realtimeInputConfig"] = map[string]interface{}{
			"automaticActivityDetection": vadConfig,
		}
		logger.Debug("Gemini VAD config added to setup", "vadConfig", vadConfig)
	}
}

// buildVADConfigMap converts VADConfig to a map for the API
func buildVADConfigMap(vad *VADConfig) map[string]interface{} {
	vadConfig := map[string]interface{}{}

	if vad.Disabled {
		vadConfig["disabled"] = true
		return vadConfig
	}

	if vad.StartOfSpeechSensitivity != "" {
		vadConfig["startOfSpeechSensitivity"] = vad.StartOfSpeechSensitivity
	}
	if vad.EndOfSpeechSensitivity != "" {
		vadConfig["endOfSpeechSensitivity"] = vad.EndOfSpeechSensitivity
	}
	if vad.PrefixPaddingMs > 0 {
		vadConfig["prefixPaddingMs"] = vad.PrefixPaddingMs
	}
	if vad.SilenceThresholdMs > 0 {
		vadConfig["silenceDurationMs"] = vad.SilenceThresholdMs
	}

	return vadConfig
}

// addSystemInstruction adds system instruction if provided
func addSystemInstruction(setupContent map[string]interface{}, instruction string) {
	if instruction != "" {
		setupContent["systemInstruction"] = map[string]interface{}{
			wireKeyParts: []map[string]interface{}{
				{wireKeyText: instruction},
			},
		}
	}
}

// addToolsConfig adds tools configuration if provided
func addToolsConfig(setupContent map[string]interface{}, tools []ToolDefinition) {
	if len(tools) == 0 {
		return
	}

	functionDeclarations := make([]map[string]interface{}, len(tools))
	for i, tool := range tools {
		functionDeclarations[i] = buildFunctionDeclaration(tool)
	}

	setupContent["tools"] = []map[string]interface{}{
		{"functionDeclarations": functionDeclarations},
	}
	logger.Debug("Gemini tools added to setup", "tool_count", len(tools))
}

// buildFunctionDeclaration converts a ToolDefinition to API format
func buildFunctionDeclaration(tool ToolDefinition) map[string]interface{} {
	funcDecl := map[string]interface{}{
		wireKeyName: tool.Name,
	}
	if tool.Description != "" {
		funcDecl["description"] = tool.Description
	}
	if len(tool.Parameters) > 0 {
		funcDecl["parameters"] = tool.Parameters
	}
	return funcDecl
}

// isVADDisabled returns true if automatic VAD is disabled for this session.
func (s *StreamSession) isVADDisabled() bool {
	return s.config.VAD != nil && s.config.VAD.Disabled
}
