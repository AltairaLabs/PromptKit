package gemini

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
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
	addSamplingConfig(generationConfig, config.Sampling)

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
		return "models/" + defaultLiveModel
	}
	if len(model) < 7 || model[:7] != "models/" {
		return "models/" + model
	}
	return model
}

// defaultLiveModel is the Live API model used when none is configured.
const defaultLiveModel = "gemini-3.8-live"

// addSamplingConfig adds the prompt's sampling parameters to a Live setup's
// generationConfig. The penalties are left out: Gemini answers them with
// "Penalty is not enabled" (CreateStreamSession reports them as not sent).
func addSamplingConfig(generationConfig map[string]interface{}, s *providers.StreamingSampling) {
	if s == nil {
		return
	}
	if s.TemperatureSet || s.Temperature != 0 {
		generationConfig["temperature"] = s.Temperature
	}
	if s.TopP != 0 {
		generationConfig["topP"] = s.TopP
	}
	if s.TopK != nil {
		generationConfig["topK"] = *s.TopK
	}
	if s.MaxTokens > 0 {
		generationConfig["maxOutputTokens"] = s.MaxTokens
	}
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
