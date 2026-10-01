package a2a

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// This file holds the version-specific wire shapes.
//
// The Go types in types.go are the protocol model. They marshal as A2A 1.0
// and unmarshal from 1.0, 0.3 and legacy PromptKit JSON alike, so decoding
// never needs to know the version. Encoding does: a 0.3 peer needs the "kind"
// discriminators, hyphenated states, lower-case roles, "final" on status
// events and kind-tagged file parts, and a 1.0 peer needs SendMessage and
// stream results wrapped in a oneof ({"task": ...}). The Wire* methods on
// ProtocolVersion produce the value to marshal for a given peer.

// WireTask returns task in version v's shape (the result of GetTask and
// CancelTask).
func (v ProtocolVersion) WireTask(task *Task) any {
	if v == ProtocolVersion03 {
		return toV03Task(task)
	}
	return task
}

// WireSendResult returns the SendMessage result for task in version v's shape:
// 1.0 wraps it as {"task": ...}, 0.3 sends the kind-tagged task bare.
func (v ProtocolVersion) WireSendResult(task *Task) any {
	if v == ProtocolVersion03 {
		return toV03Task(task)
	}
	return SendMessageResponse{Task: task}
}

// WireStreamEvent returns one streaming result in version v's shape. event is
// a *Task, *Message, *TaskStatusUpdateEvent or *TaskArtifactUpdateEvent.
// 1.0 wraps it in a StreamResponse; 0.3 sends it bare with its "kind" and, on
// a status update, "final" set when the state ends the stream.
func (v ProtocolVersion) WireStreamEvent(event any) (any, error) {
	if v == ProtocolVersion03 {
		switch e := event.(type) {
		case *Task:
			return toV03Task(e), nil
		case *Message:
			return toV03Message(e), nil
		case *TaskStatusUpdateEvent:
			return toV03StatusUpdate(e), nil
		case *TaskArtifactUpdateEvent:
			return toV03ArtifactUpdate(e), nil
		}
	} else {
		switch e := event.(type) {
		case *Task:
			return StreamResponse{Task: e}, nil
		case *Message:
			return StreamResponse{Message: e}, nil
		case *TaskStatusUpdateEvent:
			return StreamResponse{StatusUpdate: e}, nil
		case *TaskArtifactUpdateEvent:
			return StreamResponse{ArtifactUpdate: e}, nil
		}
	}
	return nil, fmt.Errorf("a2a: %T is not a stream event", event)
}

// WireSendParams returns SendMessage params in version v's shape (0.3 parts
// are kind-tagged).
func (v ProtocolVersion) WireSendParams(req *SendMessageRequest) any {
	if v != ProtocolVersion03 {
		return req
	}
	out := v03SendParams{Message: toV03Message(&req.Message), Metadata: req.Metadata}
	if c := req.Configuration; c != nil {
		out.Configuration = &v03SendConfig{
			AcceptedOutputModes: c.AcceptedOutputModes,
			HistoryLength:       c.HistoryLength,
			Blocking:            c.Blocking,
		}
	}
	return out
}

// WireAgentCard returns card in version v's shape.
//
// For 1.0 the card is returned as is. For 0.3 (and for a request that names
// no version, which the spec reads as 0.3) it is the 0.3 card — url,
// preferredTransport, protocolVersion, additionalInterfaces, 0.3-style
// security — with supportedInterfaces kept as well, so a 1.0 client reading
// the same document still finds its interface.
func (v ProtocolVersion) WireAgentCard(card *AgentCard) any {
	if v == ProtocolVersion03 {
		return toV03Card(card)
	}
	return card
}

// --- 0.3 shapes ---

// 0.3 "kind" discriminators.
const (
	kindTask           = "task"
	kindMessage        = "message"
	kindStatusUpdate   = "status-update"
	kindArtifactUpdate = "artifact-update"
	kindText           = "text"
	kindFile           = "file"
	kindData           = "data"
)

type v03File struct {
	Bytes    string `json:"bytes,omitempty"`
	URI      string `json:"uri,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Name     string `json:"name,omitempty"`
}

// into copies a 0.3 file into p's 1.0 fields.
func (f *v03File) into(p *Part) error {
	switch {
	case f.Bytes != "":
		raw, err := base64.StdEncoding.DecodeString(f.Bytes)
		if err != nil {
			return fmt.Errorf("a2a: file part bytes: %w", err)
		}
		p.Raw = raw
	case f.URI != "":
		uri := f.URI
		p.URL = &uri
	}
	if p.MediaType == "" {
		p.MediaType = f.MimeType
	}
	if p.Filename == "" {
		p.Filename = f.Name
	}
	return nil
}

type v03Part struct {
	Kind     string         `json:"kind"`
	Text     *string        `json:"text,omitempty"`
	File     *v03File       `json:"file,omitempty"`
	Data     any            `json:"data,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func toV03Part(p *Part) v03Part {
	out := v03Part{Metadata: p.Metadata}
	switch {
	case p.Raw != nil:
		out.Kind = kindFile
		out.File = &v03File{
			Bytes:    base64.StdEncoding.EncodeToString(p.Raw),
			MimeType: p.MediaType,
			Name:     p.Filename,
		}
	case p.URL != nil:
		out.Kind = kindFile
		out.File = &v03File{URI: *p.URL, MimeType: p.MediaType, Name: p.Filename}
	case p.dataValue() != nil:
		out.Kind = kindData
		out.Data = p.dataValue()
	default:
		out.Kind = kindText
		text := ""
		if p.Text != nil {
			text = *p.Text
		}
		out.Text = &text
	}
	return out
}

func toV03Parts(parts []Part) []v03Part {
	out := make([]v03Part, len(parts))
	for i := range parts {
		out[i] = toV03Part(&parts[i])
	}
	return out
}

type v03Message struct {
	Kind             string         `json:"kind"`
	MessageID        string         `json:"messageId"`
	ContextID        string         `json:"contextId,omitempty"`
	TaskID           string         `json:"taskId,omitempty"`
	Role             string         `json:"role"`
	Parts            []v03Part      `json:"parts"`
	ReferenceTaskIDs []string       `json:"referenceTaskIds,omitempty"`
	Extensions       []string       `json:"extensions,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}

func toV03Message(m *Message) *v03Message {
	if m == nil {
		return nil
	}
	return &v03Message{
		Kind:             kindMessage,
		MessageID:        m.MessageID,
		ContextID:        m.ContextID,
		TaskID:           m.TaskID,
		Role:             string(m.Role),
		Parts:            toV03Parts(m.Parts),
		ReferenceTaskIDs: m.ReferenceTaskIDs,
		Extensions:       m.Extensions,
		Metadata:         m.Metadata,
	}
}

type v03TaskStatus struct {
	State     string      `json:"state"`
	Message   *v03Message `json:"message,omitempty"`
	Timestamp *time.Time  `json:"timestamp,omitempty"`
}

func toV03Status(s *TaskStatus) v03TaskStatus {
	return v03TaskStatus{State: s.State.V03Name(), Message: toV03Message(s.Message), Timestamp: s.Timestamp}
}

type v03Artifact struct {
	ArtifactID  string         `json:"artifactId"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Parts       []v03Part      `json:"parts"`
	Extensions  []string       `json:"extensions,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

func toV03Artifact(a *Artifact) v03Artifact {
	return v03Artifact{
		ArtifactID:  a.ArtifactID,
		Name:        a.Name,
		Description: a.Description,
		Parts:       toV03Parts(a.Parts),
		Extensions:  a.Extensions,
		Metadata:    a.Metadata,
	}
}

type v03Task struct {
	Kind      string         `json:"kind"`
	ID        string         `json:"id"`
	ContextID string         `json:"contextId"`
	Status    v03TaskStatus  `json:"status"`
	Artifacts []v03Artifact  `json:"artifacts,omitempty"`
	History   []v03Message   `json:"history,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

func toV03Task(t *Task) *v03Task {
	out := &v03Task{
		Kind:      kindTask,
		ID:        t.ID,
		ContextID: t.ContextID,
		Status:    toV03Status(&t.Status),
		Metadata:  t.Metadata,
	}
	if t.Artifacts != nil {
		out.Artifacts = make([]v03Artifact, len(t.Artifacts))
		for i := range t.Artifacts {
			out.Artifacts[i] = toV03Artifact(&t.Artifacts[i])
		}
	}
	if t.History != nil {
		out.History = make([]v03Message, len(t.History))
		for i := range t.History {
			out.History[i] = *toV03Message(&t.History[i])
		}
	}
	return out
}

type v03StatusUpdate struct {
	Kind      string         `json:"kind"`
	TaskID    string         `json:"taskId"`
	ContextID string         `json:"contextId"`
	Status    v03TaskStatus  `json:"status"`
	Final     bool           `json:"final"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

func toV03StatusUpdate(e *TaskStatusUpdateEvent) v03StatusUpdate {
	return v03StatusUpdate{
		Kind:      kindStatusUpdate,
		TaskID:    e.TaskID,
		ContextID: e.ContextID,
		Status:    toV03Status(&e.Status),
		Final:     e.Status.State.IsTerminal() || e.Status.State.IsInterrupted(),
		Metadata:  e.Metadata,
	}
}

type v03ArtifactUpdate struct {
	Kind      string         `json:"kind"`
	TaskID    string         `json:"taskId"`
	ContextID string         `json:"contextId"`
	Artifact  v03Artifact    `json:"artifact"`
	Append    bool           `json:"append,omitempty"`
	LastChunk bool           `json:"lastChunk,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

func toV03ArtifactUpdate(e *TaskArtifactUpdateEvent) v03ArtifactUpdate {
	return v03ArtifactUpdate{
		Kind:      kindArtifactUpdate,
		TaskID:    e.TaskID,
		ContextID: e.ContextID,
		Artifact:  toV03Artifact(&e.Artifact),
		Append:    e.Append,
		LastChunk: e.LastChunk,
		Metadata:  e.Metadata,
	}
}

type v03SendConfig struct {
	AcceptedOutputModes []string `json:"acceptedOutputModes,omitempty"`
	HistoryLength       *int     `json:"historyLength,omitempty"`
	Blocking            bool     `json:"blocking,omitempty"`
}

type v03SendParams struct {
	Message       *v03Message    `json:"message"`
	Configuration *v03SendConfig `json:"configuration,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// v03Interface is 0.3's AgentInterface: "transport" rather than
// protocolBinding, and no version.
type v03Interface struct {
	URL       string `json:"url"`
	Transport string `json:"transport"`
}

type v03Capabilities struct {
	Streaming         bool             `json:"streaming,omitempty"`
	PushNotifications bool             `json:"pushNotifications,omitempty"`
	Extensions        []AgentExtension `json:"extensions,omitempty"`
}

type v03Skill struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Tags        []string              `json:"tags"`
	Examples    []string              `json:"examples,omitempty"`
	InputModes  []string              `json:"inputModes,omitempty"`
	OutputModes []string              `json:"outputModes,omitempty"`
	Security    []map[string][]string `json:"security,omitempty"`
}

// v03Card is the 0.3 card, carrying the 1.0 supportedInterfaces alongside.
type v03Card struct {
	ProtocolVersion                   string                       `json:"protocolVersion"`
	Name                              string                       `json:"name"`
	Description                       string                       `json:"description"`
	URL                               string                       `json:"url"`
	PreferredTransport                string                       `json:"preferredTransport"`
	AdditionalInterfaces              []v03Interface               `json:"additionalInterfaces,omitempty"`
	SupportedInterfaces               []AgentInterface             `json:"supportedInterfaces,omitempty"`
	IconURL                           string                       `json:"iconUrl,omitempty"`
	Provider                          *AgentProvider               `json:"provider,omitempty"`
	Version                           string                       `json:"version"`
	DocumentationURL                  string                       `json:"documentationUrl,omitempty"`
	Capabilities                      v03Capabilities              `json:"capabilities"`
	SecuritySchemes                   map[string]v03SecurityScheme `json:"securitySchemes,omitempty"`
	Security                          []map[string][]string        `json:"security,omitempty"`
	DefaultInputModes                 []string                     `json:"defaultInputModes"`
	DefaultOutputModes                []string                     `json:"defaultOutputModes"`
	Skills                            []v03Skill                   `json:"skills"`
	SupportsAuthenticatedExtendedCard bool                         `json:"supportsAuthenticatedExtendedCard,omitempty"`
}

func toV03Security(reqs []SecurityRequirement) []map[string][]string {
	if len(reqs) == 0 {
		return nil
	}
	out := make([]map[string][]string, len(reqs))
	for i, r := range reqs {
		m := make(map[string][]string, len(r.Schemes))
		for name, scopes := range r.Schemes {
			if scopes == nil {
				scopes = []string{}
			}
			m[name] = scopes
		}
		out[i] = m
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func toV03Card(card *AgentCard) *v03Card {
	out := &v03Card{
		ProtocolVersion:     v03CardProtocolVersion,
		Name:                card.Name,
		Description:         card.Description,
		PreferredTransport:  ProtocolBindingJSONRPC,
		SupportedInterfaces: card.SupportedInterfaces,
		IconURL:             card.IconURL,
		Provider:            card.Provider,
		Version:             card.Version,
		DocumentationURL:    card.DocumentationURL,
		Capabilities: v03Capabilities{
			Streaming:         card.Capabilities.Streaming,
			PushNotifications: card.Capabilities.PushNotifications,
			Extensions:        card.Capabilities.Extensions,
		},
		Security:                          toV03Security(card.SecurityRequirements),
		DefaultInputModes:                 nonNil(card.DefaultInputModes),
		DefaultOutputModes:                nonNil(card.DefaultOutputModes),
		Skills:                            make([]v03Skill, len(card.Skills)),
		SupportsAuthenticatedExtendedCard: card.Capabilities.ExtendedAgentCard,
	}
	// url is the preferred JSON-RPC interface; other transports become
	// additionalInterfaces (0.3 §5.6).
	for _, iface := range card.SupportedInterfaces {
		transport := iface.ProtocolBinding
		if strings.EqualFold(transport, ProtocolBindingJSONRPC) {
			transport = ProtocolBindingJSONRPC
			if out.URL == "" {
				out.URL = iface.URL
			}
		}
		out.AdditionalInterfaces = append(out.AdditionalInterfaces, v03Interface{URL: iface.URL, Transport: transport})
	}
	if len(card.SecuritySchemes) > 0 {
		out.SecuritySchemes = make(map[string]v03SecurityScheme, len(card.SecuritySchemes))
		for name, scheme := range card.SecuritySchemes {
			out.SecuritySchemes[name] = scheme.v03()
		}
	}
	for i := range card.Skills {
		sk := &card.Skills[i]
		out.Skills[i] = v03Skill{
			ID:          sk.ID,
			Name:        sk.Name,
			Description: sk.Description,
			Tags:        nonNil(sk.Tags),
			Examples:    sk.Examples,
			InputModes:  sk.InputModes,
			OutputModes: sk.OutputModes,
			Security:    toV03Security(sk.SecurityRequirements),
		}
	}
	return out
}

// UnmarshalJSON implements json.Unmarshaler. A 0.3 card declares its endpoint
// in url/preferredTransport/additionalInterfaces rather than
// supportedInterfaces, and its requirements in "security"; both are folded
// into the 1.0 fields when the card does not carry them itself.
func (card *AgentCard) UnmarshalJSON(data []byte) error {
	type plain AgentCard
	var wire struct {
		plain
		URL                  string                `json:"url"`
		PreferredTransport   string                `json:"preferredTransport"`
		ProtocolVersion      string                `json:"protocolVersion"`
		AdditionalInterfaces []v03Interface        `json:"additionalInterfaces"`
		Security             []SecurityRequirement `json:"security"`
		SupportsExtended     bool                  `json:"supportsAuthenticatedExtendedCard"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*card = AgentCard(wire.plain)
	if len(card.SupportedInterfaces) == 0 && wire.URL != "" {
		card.SupportedInterfaces = v03Interfaces(wire.URL, wire.PreferredTransport,
			wire.ProtocolVersion, wire.AdditionalInterfaces)
	}
	if len(card.SecurityRequirements) == 0 {
		card.SecurityRequirements = wire.Security
	}
	if wire.SupportsExtended {
		card.Capabilities.ExtendedAgentCard = true
	}
	return nil
}

// v03Interfaces converts a 0.3 card's endpoint declaration to 1.0 interfaces.
func v03Interfaces(url, transport, version string, additional []v03Interface) []AgentInterface {
	if transport == "" {
		transport = ProtocolBindingJSONRPC
	}
	v := string(ProtocolVersion03)
	if parsed, err := ParseProtocolVersion(version); err == nil && parsed != "" {
		v = string(parsed)
	}
	out := []AgentInterface{{URL: url, ProtocolBinding: transport, ProtocolVersion: v}}
	for _, a := range additional {
		if a.URL == url && strings.EqualFold(a.Transport, transport) {
			continue
		}
		out = append(out, AgentInterface{URL: a.URL, ProtocolBinding: a.Transport, ProtocolVersion: v})
	}
	return out
}
