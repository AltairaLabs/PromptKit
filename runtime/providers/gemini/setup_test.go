package gemini

import (
	"reflect"
	"testing"
)

func TestGetResponseModalities(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty defaults to TEXT", nil, []string{"TEXT"}},
		{"empty slice defaults to TEXT", []string{}, []string{"TEXT"}},
		{"explicit AUDIO preserved", []string{"AUDIO"}, []string{"AUDIO"}},
		{"explicit TEXT preserved", []string{"TEXT"}, []string{"TEXT"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getResponseModalities(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getResponseModalities(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestValidateModalities(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		wantErr bool
	}{
		{"TEXT only ok", []string{"TEXT"}, false},
		{"AUDIO only ok", []string{"AUDIO"}, false},
		{"empty ok", nil, false},
		{"TEXT+AUDIO rejected", []string{"TEXT", "AUDIO"}, true},
		{"AUDIO+TEXT rejected", []string{"AUDIO", "TEXT"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateModalities(tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateModalities(%v) err=%v, wantErr=%v", tt.in, err, tt.wantErr)
			}
		})
	}
}

func TestGetModelPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty -> default", "", "models/gemini-2.0-flash-exp"},
		{"bare name gets prefixed", "gemini-2.0-flash", "models/gemini-2.0-flash"},
		{"already prefixed unchanged", "models/gemini-2.0-flash", "models/gemini-2.0-flash"},
		{"short name gets prefixed", "abc", "models/abc"},
		{"exactly models/ prefix preserved", "models/x", "models/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getModelPath(tt.in); got != tt.want {
				t.Errorf("getModelPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestBuildGenerationConfig(t *testing.T) {
	t.Run("TEXT has no speechConfig", func(t *testing.T) {
		cfg := buildGenerationConfig([]string{"TEXT"})
		if !reflect.DeepEqual(cfg["responseModalities"], []string{"TEXT"}) {
			t.Errorf("responseModalities = %v", cfg["responseModalities"])
		}
		if _, ok := cfg["speechConfig"]; ok {
			t.Error("expected no speechConfig for TEXT")
		}
	})

	t.Run("AUDIO adds Puck speechConfig", func(t *testing.T) {
		cfg := buildGenerationConfig([]string{"AUDIO"})
		speech, ok := cfg["speechConfig"].(map[string]interface{})
		if !ok {
			t.Fatal("expected speechConfig for AUDIO")
		}
		voiceCfg, ok := speech["voiceConfig"].(map[string]interface{})
		if !ok {
			t.Fatal("expected voiceConfig")
		}
		prebuilt, ok := voiceCfg["prebuiltVoiceConfig"].(map[string]interface{})
		if !ok {
			t.Fatal("expected prebuiltVoiceConfig")
		}
		if prebuilt["voiceName"] != "Puck" {
			t.Errorf("voiceName = %v, want Puck", prebuilt["voiceName"])
		}
	})
}

func TestAddTranscriptionConfig(t *testing.T) {
	t.Run("TEXT adds nothing", func(t *testing.T) {
		content := map[string]interface{}{}
		addTranscriptionConfig(content, []string{"TEXT"})
		if len(content) != 0 {
			t.Errorf("expected no transcription keys for TEXT, got %v", content)
		}
	})

	t.Run("AUDIO adds input+output transcription", func(t *testing.T) {
		content := map[string]interface{}{}
		addTranscriptionConfig(content, []string{"AUDIO"})
		if _, ok := content["outputAudioTranscription"]; !ok {
			t.Error("expected outputAudioTranscription key")
		}
		if _, ok := content["inputAudioTranscription"]; !ok {
			t.Error("expected inputAudioTranscription key")
		}
	})
}

func TestAddVADConfig(t *testing.T) {
	t.Run("nil VAD adds nothing", func(t *testing.T) {
		content := map[string]interface{}{}
		addVADConfig(content, nil)
		if len(content) != 0 {
			t.Errorf("expected no keys for nil VAD, got %v", content)
		}
	})

	t.Run("empty VAD (no fields) adds nothing", func(t *testing.T) {
		content := map[string]interface{}{}
		addVADConfig(content, &VADConfig{})
		if len(content) != 0 {
			t.Errorf("expected no realtimeInputConfig when VAD map is empty, got %v", content)
		}
	})

	t.Run("populated VAD nests under automaticActivityDetection", func(t *testing.T) {
		content := map[string]interface{}{}
		addVADConfig(content, &VADConfig{StartOfSpeechSensitivity: "HIGH"})
		ric, ok := content["realtimeInputConfig"].(map[string]interface{})
		if !ok {
			t.Fatal("expected realtimeInputConfig")
		}
		aad, ok := ric["automaticActivityDetection"].(map[string]interface{})
		if !ok {
			t.Fatal("expected automaticActivityDetection")
		}
		if aad["startOfSpeechSensitivity"] != "HIGH" {
			t.Errorf("startOfSpeechSensitivity = %v", aad["startOfSpeechSensitivity"])
		}
	})
}

func TestBuildVADConfigMap(t *testing.T) {
	t.Run("disabled short-circuits with only disabled flag", func(t *testing.T) {
		got := buildVADConfigMap(&VADConfig{
			Disabled:                 true,
			StartOfSpeechSensitivity: "HIGH", // must be ignored
			PrefixPaddingMs:          100,    // must be ignored
		})
		want := map[string]interface{}{"disabled": true}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("buildVADConfigMap disabled = %v, want %v", got, want)
		}
	})

	t.Run("all optional numeric/string fields mapped with exact keys", func(t *testing.T) {
		got := buildVADConfigMap(&VADConfig{
			StartOfSpeechSensitivity: "LOW",
			EndOfSpeechSensitivity:   "MEDIUM",
			PrefixPaddingMs:          200,
			SilenceThresholdMs:       500,
		})
		want := map[string]interface{}{
			"startOfSpeechSensitivity": "LOW",
			"endOfSpeechSensitivity":   "MEDIUM",
			"prefixPaddingMs":          200,
			// SilenceThresholdMs maps to Gemini's silenceDurationMs
			"silenceDurationMs": 500,
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("buildVADConfigMap = %v, want %v", got, want)
		}
	})

	t.Run("zero-value optionals omitted", func(t *testing.T) {
		got := buildVADConfigMap(&VADConfig{})
		if len(got) != 0 {
			t.Errorf("expected empty map for zero-value VAD, got %v", got)
		}
	})

	t.Run("only non-empty fields included", func(t *testing.T) {
		got := buildVADConfigMap(&VADConfig{EndOfSpeechSensitivity: "HIGH"})
		want := map[string]interface{}{"endOfSpeechSensitivity": "HIGH"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

func TestAddSystemInstruction(t *testing.T) {
	t.Run("empty adds nothing", func(t *testing.T) {
		content := map[string]interface{}{}
		addSystemInstruction(content, "")
		if len(content) != 0 {
			t.Errorf("expected no key for empty instruction, got %v", content)
		}
	})

	t.Run("non-empty nests parts[].text", func(t *testing.T) {
		content := map[string]interface{}{}
		addSystemInstruction(content, "be helpful")
		si, ok := content["systemInstruction"].(map[string]interface{})
		if !ok {
			t.Fatal("expected systemInstruction")
		}
		parts, ok := si["parts"].([]map[string]interface{})
		if !ok || len(parts) != 1 {
			t.Fatalf("expected 1 part, got %v", si["parts"])
		}
		if parts[0]["text"] != "be helpful" {
			t.Errorf("text = %v", parts[0]["text"])
		}
	})
}

func TestAddToolsConfig(t *testing.T) {
	t.Run("no tools adds nothing", func(t *testing.T) {
		content := map[string]interface{}{}
		addToolsConfig(content, nil)
		if len(content) != 0 {
			t.Errorf("expected no key for empty tools, got %v", content)
		}
	})

	t.Run("tools nest under functionDeclarations", func(t *testing.T) {
		content := map[string]interface{}{}
		addToolsConfig(content, []ToolDefinition{
			{Name: "a"},
			{Name: "b", Description: "does b"},
		})
		tools, ok := content["tools"].([]map[string]interface{})
		if !ok || len(tools) != 1 {
			t.Fatalf("expected 1 tools entry, got %v", content["tools"])
		}
		decls, ok := tools[0]["functionDeclarations"].([]map[string]interface{})
		if !ok || len(decls) != 2 {
			t.Fatalf("expected 2 function declarations, got %v", tools[0]["functionDeclarations"])
		}
		if decls[0]["name"] != "a" || decls[1]["name"] != "b" {
			t.Errorf("declaration names = %v, %v", decls[0]["name"], decls[1]["name"])
		}
	})
}

func TestBuildFunctionDeclaration(t *testing.T) {
	t.Run("name only", func(t *testing.T) {
		got := buildFunctionDeclaration(ToolDefinition{Name: "search"})
		if got["name"] != "search" {
			t.Errorf("name = %v", got["name"])
		}
		if _, ok := got["description"]; ok {
			t.Error("expected no description key")
		}
		if _, ok := got["parameters"]; ok {
			t.Error("expected no parameters key")
		}
	})

	t.Run("with description and parameters", func(t *testing.T) {
		params := map[string]interface{}{"type": "object"}
		got := buildFunctionDeclaration(ToolDefinition{
			Name:        "search",
			Description: "search the web",
			Parameters:  params,
		})
		if got["description"] != "search the web" {
			t.Errorf("description = %v", got["description"])
		}
		if !reflect.DeepEqual(got["parameters"], params) {
			t.Errorf("parameters = %v", got["parameters"])
		}
	})

	t.Run("empty parameters map omitted", func(t *testing.T) {
		got := buildFunctionDeclaration(ToolDefinition{
			Name:       "n",
			Parameters: map[string]interface{}{},
		})
		if _, ok := got["parameters"]; ok {
			t.Error("expected empty parameters map to be omitted")
		}
	})
}

func TestBuildSetupMessage(t *testing.T) {
	t.Run("minimal TEXT config", func(t *testing.T) {
		msg := buildSetupMessage(&StreamSessionConfig{Model: "gemini-2.0-flash"}, []string{"TEXT"})
		setup, ok := msg["setup"].(map[string]interface{})
		if !ok {
			t.Fatal("expected setup key")
		}
		if setup["model"] != "models/gemini-2.0-flash" {
			t.Errorf("model = %v", setup["model"])
		}
		if _, ok := setup["generationConfig"]; !ok {
			t.Error("expected generationConfig")
		}
		// TEXT should not add transcription/tools/system instruction
		for _, k := range []string{"outputAudioTranscription", "tools", "systemInstruction", "realtimeInputConfig"} {
			if _, ok := setup[k]; ok {
				t.Errorf("did not expect key %q for minimal TEXT config", k)
			}
		}
	})

	t.Run("full AUDIO config wires every section", func(t *testing.T) {
		msg := buildSetupMessage(&StreamSessionConfig{
			Model:             "gemini-live",
			SystemInstruction: "be nice",
			VAD:               &VADConfig{SilenceThresholdMs: 700},
			Tools:             []ToolDefinition{{Name: "t"}},
		}, []string{"AUDIO"})
		setup := msg["setup"].(map[string]interface{})
		for _, k := range []string{
			"model", "generationConfig", "outputAudioTranscription",
			"inputAudioTranscription", "systemInstruction", "tools", "realtimeInputConfig",
		} {
			if _, ok := setup[k]; !ok {
				t.Errorf("expected key %q in full AUDIO setup", k)
			}
		}
	})
}
