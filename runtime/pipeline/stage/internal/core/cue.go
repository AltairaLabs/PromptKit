package core

// CueKind says what a cue is for.
type CueKind string

// CueGuardrailMessage is a guardrail's message, spoken when it cuts a reply
// or blocks typed input.
const CueGuardrailMessage CueKind = "guardrail_message"

// Cue asks the cue stage (NewDuplexCueStage) to play the clip for Text.
type Cue struct {
	Kind CueKind
	// Text is what the clip says and the key it is looked up by.
	Text string
	// Preemptible cues give way to reply audio: a flush-on-cut
	// AudioPacingStage drops what is still queued of them when reply audio
	// arrives. Guardrail messages are not preemptible: a reply queues behind
	// them as behind another reply.
	Preemptible bool
}
