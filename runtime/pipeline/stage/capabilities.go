// Package stage provides the reactive streams architecture for pipeline execution.
package stage

// contentTypeAny is a constant for "any" content type string representation.
const contentTypeAny = "any"

// ContentType describes the type of content a stage handles.
type ContentType int

const (
	// ContentTypeAny indicates the stage accepts any content type.
	ContentTypeAny ContentType = iota
	// ContentTypeText indicates text content.
	ContentTypeText
	// ContentTypeAudio indicates audio content.
	ContentTypeAudio
	// ContentTypeVideo indicates video content.
	ContentTypeVideo
	// ContentTypeImage indicates image content.
	ContentTypeImage
	// ContentTypeMessage indicates a complete message.
	ContentTypeMessage
	// ContentTypeToolCall indicates a tool invocation.
	ContentTypeToolCall
)

// String returns the string representation of the content type.
func (ct ContentType) String() string {
	switch ct {
	case ContentTypeAny:
		return contentTypeAny
	case ContentTypeText:
		return "text"
	case ContentTypeAudio:
		return "audio"
	case ContentTypeVideo:
		return "video"
	case ContentTypeImage:
		return "image"
	case ContentTypeMessage:
		return "message"
	case ContentTypeToolCall:
		return "tool_call"
	default:
		return unknownType
	}
}

// AudioCapability describes audio format requirements for a stage.
type AudioCapability struct {
	// Formats lists accepted audio formats. Empty slice means any format.
	Formats []AudioFormat
	// SampleRates lists accepted sample rates in Hz. Empty slice means any rate.
	SampleRates []int
	// Channels lists accepted channel counts. Empty slice means any.
	Channels []int
}

// AcceptsFormat returns true if this capability accepts the given format.
// Returns true if Formats is empty (accepts any).
func (ac *AudioCapability) AcceptsFormat(format AudioFormat) bool {
	if ac == nil || len(ac.Formats) == 0 {
		return true
	}
	for _, f := range ac.Formats {
		if f == format {
			return true
		}
	}
	return false
}

// AcceptsSampleRate returns true if this capability accepts the given sample rate.
// Returns true if SampleRates is empty (accepts any).
func (ac *AudioCapability) AcceptsSampleRate(rate int) bool {
	if ac == nil || len(ac.SampleRates) == 0 {
		return true
	}
	for _, r := range ac.SampleRates {
		if r == rate {
			return true
		}
	}
	return false
}

// AcceptsChannels returns true if this capability accepts the given channel count.
// Returns true if Channels is empty (accepts any).
func (ac *AudioCapability) AcceptsChannels(channels int) bool {
	if ac == nil || len(ac.Channels) == 0 {
		return true
	}
	for _, c := range ac.Channels {
		if c == channels {
			return true
		}
	}
	return false
}

// AcceptsAudio returns true if this capability accepts the given audio data.
func (ac *AudioCapability) AcceptsAudio(audio *AudioData) bool {
	if audio == nil {
		return true
	}
	return ac.AcceptsFormat(audio.Format) &&
		ac.AcceptsSampleRate(audio.SampleRate) &&
		ac.AcceptsChannels(audio.Channels)
}

// Capabilities describes what a stage accepts or produces.
type Capabilities struct {
	// ContentTypes lists the content types handled. Empty means any.
	ContentTypes []ContentType
	// Audio specifies audio-specific requirements. Nil means N/A or any.
	Audio *AudioCapability
}

// AcceptsContentType returns true if this capability accepts the given content type.
// Returns true if ContentTypes is empty (accepts any).
func (c *Capabilities) AcceptsContentType(ct ContentType) bool {
	if c == nil || len(c.ContentTypes) == 0 {
		return true
	}
	for _, t := range c.ContentTypes {
		if t == ContentTypeAny || t == ct {
			return true
		}
	}
	return false
}

// AcceptsElement returns true if this capability accepts the given stream element.
func (c *Capabilities) AcceptsElement(elem *StreamElement) bool {
	if c == nil || elem == nil {
		return true
	}

	// Determine content type of element
	ct := ContentTypeAny
	switch {
	case elem.Text != nil:
		ct = ContentTypeText
	case elem.Audio != nil:
		ct = ContentTypeAudio
	case elem.Video != nil:
		ct = ContentTypeVideo
	case elem.Image != nil:
		ct = ContentTypeImage
	case elem.Message != nil:
		ct = ContentTypeMessage
	case elem.ToolCall != nil:
		ct = ContentTypeToolCall
	}

	if !c.AcceptsContentType(ct) {
		return false
	}

	// Check audio-specific requirements
	if elem.Audio != nil && c.Audio != nil {
		return c.Audio.AcceptsAudio(elem.Audio)
	}

	return true
}

// FormatCapable is an optional interface that stages can implement
// to declare their input/output format requirements.
// Stages that don't implement this are treated as accepting/producing any format.
type FormatCapable interface {
	// InputCapabilities returns what formats/content types this stage accepts.
	InputCapabilities() Capabilities
	// OutputCapabilities returns what formats/content types this stage produces.
	OutputCapabilities() Capabilities
}

// AnyCapabilities returns capabilities that accept any content type.
func AnyCapabilities() Capabilities {
	return Capabilities{
		ContentTypes: []ContentType{ContentTypeAny},
	}
}

// TextCapabilities returns capabilities for text-only content.
func TextCapabilities() Capabilities {
	return Capabilities{
		ContentTypes: []ContentType{ContentTypeText},
	}
}

// AudioCapabilities returns capabilities for audio content with optional format constraints.
func AudioCapabilities(formats []AudioFormat, sampleRates, channels []int) Capabilities {
	return Capabilities{
		ContentTypes: []ContentType{ContentTypeAudio},
		Audio: &AudioCapability{
			Formats:     formats,
			SampleRates: sampleRates,
			Channels:    channels,
		},
	}
}

// MessageCapabilities returns capabilities for message content.
func MessageCapabilities() Capabilities {
	return Capabilities{
		ContentTypes: []ContentType{ContentTypeMessage},
	}
}
