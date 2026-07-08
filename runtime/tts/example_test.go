package tts

import "fmt"

// ExampleDefaultSynthesisConfig shows the sensible defaults returned for
// text-to-speech synthesis: alloy voice, MP3 output, normal speed and
// pitch. No network access or API key is required to build the config.
func ExampleDefaultSynthesisConfig() {
	config := DefaultSynthesisConfig()
	fmt.Printf("voice=%s format=%s speed=%.1f pitch=%.0f\n",
		config.Voice, config.Format.Name, config.Speed, config.Pitch)
	// Output: voice=alloy format=mp3 speed=1.0 pitch=0
}
