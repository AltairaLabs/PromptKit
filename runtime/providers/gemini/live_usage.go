package gemini

import "github.com/AltairaLabs/PromptKit/runtime/v2/providers"

// Gemini Live usage, as observed live (gemini-2.5-flash-native-audio and
// gemini-3.8-live, a text turn, a tool-call turn and a two-tool chain):
//
//   - usageMetadata arrives only on turnComplete, never on a toolCall, a
//     model turn or a message of its own;
//   - each turnComplete carries the usage of the generation it ends, not a
//     running total: Gemini 3.x ends each tool-call generation with its own
//     turnComplete, Gemini 2.5 reports a tool chain and its reply with one.
//
// So usage is summed until the chunk that ends the turn (holdUsage,
// takeTurnCost), and a toolCall's usage is not counted (it would count
// twice). Usage that arrives once its turn has ended (a 3.x tool-call
// turnComplete, a usage-only message right after a turnComplete) is reported
// at once as a usage-only chunk: it belongs to the turn that just ended, and
// the stage reports it on that turn's provider call.

// usageOnly handles a message that carries usage and nothing else.
func (s *StreamSession) usageOnly(usage *UsageMetadata) error {
	if usage == nil {
		return nil
	}
	s.holdUsage(usage)
	if s.turnOpen {
		return nil
	}
	return s.sendChunk(&providers.StreamChunk{CostInfo: s.takeTurnCost()})
}
