// Package a2a implements the client side of the Agent-to-Agent (A2A) protocol.
//
// It provides the protocol types, a JSON-RPC Client for discovering and calling
// agents, an Executor that exposes A2A skills as tools, and a ToolBridge that
// builds tool descriptors from an agent card. The package speaks A2A 1.0 and
// 0.3: the types marshal as 1.0 and unmarshal either version's JSON, and
// ProtocolVersion produces the wire shape for a given peer.
//
// Types are derived from the A2A protocol specification (a2a.proto)
// and use camelCase JSON tags matching the JSON-RPC binding convention.
package a2a

import (
	"encoding/json"
	"fmt"
	"time"
)

// A2A 1.0 JSON-RPC method names (A2A 1.0 §5.3).
const (
	MethodV1SendMessage          = "SendMessage"
	MethodV1SendStreamingMessage = "SendStreamingMessage"
	MethodV1GetTask              = "GetTask"
	MethodV1CancelTask           = "CancelTask"
	MethodV1ListTasks            = "ListTasks"
	MethodV1SubscribeToTask      = "SubscribeToTask"
	MethodV1GetExtendedAgentCard = "GetExtendedAgentCard"

	MethodV1CreateTaskPushNotificationConfig = "CreateTaskPushNotificationConfig"
	MethodV1GetTaskPushNotificationConfig    = "GetTaskPushNotificationConfig"
	MethodV1ListTaskPushNotificationConfigs  = "ListTaskPushNotificationConfigs"
	MethodV1DeleteTaskPushNotificationConfig = "DeleteTaskPushNotificationConfig"
)

// The method names PromptKit used before it spoke A2A 1.0 and 0.3 by version.
// They keep their values: a server still answers every one of them, and code
// comparing against them keeps working.
//
// Deprecated: use the MethodV1* names for A2A 1.0 and MethodV03* for 0.3, or
// resolve a method with [LookupMethod].
const (
	MethodSendMessage          = "message/send"
	MethodSendStreamingMessage = "message/stream"
	MethodGetTask              = "tasks/get"
	MethodCancelTask           = "tasks/cancel"
	MethodListTasks            = "tasks/list"
	MethodTaskSubscribe        = "tasks/subscribe"
)

// A2A 0.3 JSON-RPC method names. A 0.3 server knows only these.
const (
	MethodV03SendMessage          = "message/send"
	MethodV03SendStreamingMessage = "message/stream"
	MethodV03GetTask              = "tasks/get"
	MethodV03CancelTask           = "tasks/cancel"
	MethodV03Resubscribe          = "tasks/resubscribe"
	MethodV03GetExtendedCard      = "agent/getAuthenticatedExtendedCard"

	MethodV03SetPushNotificationConfig    = "tasks/pushNotificationConfig/set"
	MethodV03GetPushNotificationConfig    = "tasks/pushNotificationConfig/get"
	MethodV03ListPushNotificationConfig   = "tasks/pushNotificationConfig/list"
	MethodV03DeletePushNotificationConfig = "tasks/pushNotificationConfig/delete"

	// MethodLegacyListTasks and MethodLegacySubscribe are the names PromptKit
	// servers before A2A 1.0 conformance answered to; they are in neither
	// 0.3 nor 1.0 and are accepted only so older PromptKit clients keep
	// working.
	MethodLegacyListTasks = MethodListTasks
	MethodLegacySubscribe = MethodTaskSubscribe
)

// A2A-specific JSON-RPC error codes (A2A 1.0 §5.4, identical in 0.3).
const (
	ErrCodeTaskNotFound                   = -32001
	ErrCodeTaskNotCancelable              = -32002
	ErrCodePushNotificationNotSupported   = -32003
	ErrCodeUnsupportedOperation           = -32004
	ErrCodeContentTypeNotSupported        = -32005
	ErrCodeInvalidAgentResponse           = -32006
	ErrCodeExtendedAgentCardNotConfigured = -32007
	ErrCodeExtensionSupportRequired       = -32008
	ErrCodeVersionNotSupported            = -32009
)

// Standard JSON-RPC 2.0 error codes.
const (
	ErrCodeParse          = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternal       = -32603
)

// TaskState represents the state of an A2A task.
//
// The Go values are PromptKit's own and never appear on the wire: a TaskState
// marshals to its A2A 1.0 name (TASK_STATE_COMPLETED, ...) and unmarshals from
// the 1.0, 0.3 (input-required) and legacy (input_required) spellings alike.
type TaskState string

// TaskState constants for the A2A protocol.
const (
	TaskStateSubmitted     TaskState = "submitted"
	TaskStateWorking       TaskState = "working"
	TaskStateCompleted     TaskState = "completed"
	TaskStateFailed        TaskState = "failed"
	TaskStateCanceled      TaskState = "canceled"
	TaskStateInputRequired TaskState = "input_required"
	TaskStateRejected      TaskState = "rejected"
	TaskStateAuthRequired  TaskState = "auth_required"
	// TaskStateUnknown is 1.0's TASK_STATE_UNSPECIFIED and 0.3's "unknown".
	TaskStateUnknown TaskState = "unknown"
)

// taskStateWire maps each state to its A2A 1.0 and 0.3 names.
var taskStateWire = map[TaskState][2]string{
	TaskStateSubmitted:     {"TASK_STATE_SUBMITTED", "submitted"},
	TaskStateWorking:       {"TASK_STATE_WORKING", "working"},
	TaskStateCompleted:     {"TASK_STATE_COMPLETED", "completed"},
	TaskStateFailed:        {"TASK_STATE_FAILED", "failed"},
	TaskStateCanceled:      {"TASK_STATE_CANCELED", "canceled"},
	TaskStateInputRequired: {"TASK_STATE_INPUT_REQUIRED", "input-required"},
	TaskStateRejected:      {"TASK_STATE_REJECTED", "rejected"},
	TaskStateAuthRequired:  {"TASK_STATE_AUTH_REQUIRED", "auth-required"},
	TaskStateUnknown:       {"TASK_STATE_UNSPECIFIED", "unknown"},
}

// taskStateByWire resolves every accepted spelling to its TaskState.
var taskStateByWire = func() map[string]TaskState {
	const spellings = 3 // 1.0, 0.3 and the Go value
	m := make(map[string]TaskState, len(taskStateWire)*spellings)
	for s, names := range taskStateWire {
		m[names[0]] = s
		m[names[1]] = s
		m[string(s)] = s
	}
	return m
}()

// V1Name returns the state's A2A 1.0 wire name, e.g. TASK_STATE_COMPLETED.
func (s TaskState) V1Name() string { return taskStateWire[s][0] }

// V03Name returns the state's A2A 0.3 wire name, e.g. input-required.
func (s TaskState) V03Name() string { return taskStateWire[s][1] }

// terminalTaskStates are the states from which no transition is possible.
var terminalTaskStates = map[TaskState]bool{
	TaskStateCompleted: true,
	TaskStateFailed:    true,
	TaskStateCanceled:  true,
	TaskStateRejected:  true,
}

// IsTerminal reports whether no further transitions are possible from s.
func (s TaskState) IsTerminal() bool { return terminalTaskStates[s] }

// IsInterrupted reports whether s pauses the task for the client
// (input-required or auth-required). A stream ends at an interrupted state.
func (s TaskState) IsInterrupted() bool {
	return s == TaskStateInputRequired || s == TaskStateAuthRequired
}

// ParseTaskState resolves a wire spelling (1.0, 0.3 or legacy PromptKit) to a
// TaskState.
func ParseTaskState(str string) (TaskState, error) {
	state, ok := taskStateByWire[str]
	if !ok {
		return "", fmt.Errorf("a2a: invalid task state: %q", str)
	}
	return state, nil
}

// MarshalJSON implements json.Marshaler, writing the A2A 1.0 name.
func (s TaskState) MarshalJSON() ([]byte, error) {
	names, ok := taskStateWire[s]
	if !ok {
		return nil, fmt.Errorf("a2a: invalid task state: %q", s)
	}
	return json.Marshal(names[0])
}

// UnmarshalJSON implements json.Unmarshaler, accepting any version's spelling.
// A spelling no version defines (a state a later version adds, say) decodes
// as TaskStateUnknown rather than failing the object it is in: A2A 1.0 §5.7
// asks implementations to ignore what they do not recognize. Use
// ParseTaskState to refuse it instead.
func (s *TaskState) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	state, err := ParseTaskState(str)
	if err != nil {
		state = TaskStateUnknown
	}
	*s = state
	return nil
}

// Role identifies the sender of a message.
//
// Like TaskState, a Role marshals to its A2A 1.0 name (ROLE_USER) and
// unmarshals from either version's spelling.
type Role string

// Role constants for message senders.
const (
	RoleUser  Role = "user"
	RoleAgent Role = "agent"
)

// A2A 1.0 wire names of the roles.
const (
	roleUserV1        = "ROLE_USER"
	roleAgentV1       = "ROLE_AGENT"
	roleUnspecifiedV1 = "ROLE_UNSPECIFIED"
)

// V1Name returns the role's A2A 1.0 wire name.
func (r Role) V1Name() string {
	switch r {
	case RoleUser:
		return roleUserV1
	case RoleAgent:
		return roleAgentV1
	default:
		return roleUnspecifiedV1
	}
}

// MarshalJSON implements json.Marshaler, writing the A2A 1.0 name.
func (r Role) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.V1Name())
}

// UnmarshalJSON implements json.Unmarshaler, accepting either version's
// spelling. An unrecognized role is kept verbatim.
func (r *Role) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	switch str {
	case roleUserV1:
		*r = RoleUser
	case roleAgentV1:
		*r = RoleAgent
	case roleUnspecifiedV1:
		*r = ""
	default:
		*r = Role(str)
	}
	return nil
}

// Part represents a piece of content within a message or artifact.
// Exactly one of Text, Raw, URL, or Data (or DataValue) should be set.
type Part struct {
	Text *string        `json:"text,omitempty"`
	Raw  []byte         `json:"raw,omitempty"`
	URL  *string        `json:"url,omitempty"`
	Data map[string]any `json:"data,omitempty"`
	// DataValue holds a data part whose value is not a JSON object: A2A 1.0
	// types data as any JSON value (an array, string, number or boolean as
	// well). An object is decoded into Data; anything else into DataValue.
	// When both are set, Data is sent.
	DataValue any `json:"-"`

	Metadata  map[string]any `json:"metadata,omitempty"`
	Filename  string         `json:"filename,omitempty"`
	MediaType string         `json:"mediaType,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler. Besides the A2A 1.0 shape it
// accepts 0.3's kind-tagged parts: {"kind":"text"}, {"kind":"data"} and
// {"kind":"file","file":{"bytes"|"uri",...}}.
func (p *Part) UnmarshalJSON(data []byte) error {
	type plain Part
	var wire struct {
		plain
		// Data shadows plain.Data: it may be any JSON value.
		Data json.RawMessage `json:"data"`
		Kind string          `json:"kind"`
		File *v03File        `json:"file"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*p = Part(wire.plain)
	if err := p.setData(wire.Data); err != nil {
		return err
	}
	if wire.File != nil {
		if err := wire.File.into(p); err != nil {
			return err
		}
	}
	return nil
}

// setData decodes a data part's value: an object into Data, any other
// value into DataValue. JSON null leaves both unset.
func (p *Part) setData(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("a2a: part data: %w", err)
	}
	if obj, ok := v.(map[string]any); ok {
		p.Data = obj
		return nil
	}
	p.DataValue = v
	return nil
}

// dataValue returns the data part's value, or nil when p is not a data part.
func (p *Part) dataValue() any {
	if p.Data != nil {
		return p.Data
	}
	return p.DataValue
}

// MarshalJSON implements json.Marshaler. It writes the A2A 1.0 shape, with
// "data" carrying Data (an empty object included) or DataValue.
func (p Part) MarshalJSON() ([]byte, error) {
	type plain Part
	return json.Marshal(struct {
		plain
		Data any `json:"data,omitempty"`
	}{plain: plain(p), Data: p.dataValue()})
}

// Message is a communication unit in the A2A protocol.
type Message struct {
	MessageID        string         `json:"messageId"`
	ContextID        string         `json:"contextId,omitempty"`
	TaskID           string         `json:"taskId,omitempty"`
	Role             Role           `json:"role"`
	Parts            []Part         `json:"parts"`
	ReferenceTaskIDs []string       `json:"referenceTaskIds,omitempty"`
	Extensions       []string       `json:"extensions,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}

// Artifact is a named output generated by an agent.
type Artifact struct {
	ArtifactID  string         `json:"artifactId"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Parts       []Part         `json:"parts"`
	Extensions  []string       `json:"extensions,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// TaskStatus describes the current status of a task.
type TaskStatus struct {
	State     TaskState  `json:"state"`
	Message   *Message   `json:"message,omitempty"`
	Timestamp *time.Time `json:"timestamp,omitempty"`
}

// MarshalJSON implements json.Marshaler. Timestamp is written as a ProtoJSON
// google.protobuf.Timestamp (A2A 1.0 TaskStatus.timestamp): RFC 3339 in UTC,
// with 0, 3, 6 or 9 fractional digits, whatever zone the time is held in.
func (s TaskStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		State     TaskState `json:"state"`
		Message   *Message  `json:"message,omitempty"`
		Timestamp string    `json:"timestamp,omitempty"`
	}{s.State, s.Message, formatTimestamp(s.Timestamp)})
}

// formatTimestamp writes t as a ProtoJSON google.protobuf.Timestamp: RFC 3339
// in UTC ("Z"), with 0, 3, 6 or 9 fractional digits. A nil t is "".
func formatTimestamp(t *time.Time) string {
	if t == nil {
		return ""
	}
	u := t.UTC()
	const base = "2006-01-02T15:04:05"
	switch ns := u.Nanosecond(); {
	case ns == 0:
		return u.Format(base + "Z")
	case ns%1_000_000 == 0:
		return u.Format(base + ".000Z")
	case ns%1_000 == 0:
		return u.Format(base + ".000000Z")
	default:
		return u.Format(base + ".000000000Z")
	}
}

// Task is the top-level unit of work in the A2A protocol.
type Task struct {
	ID        string         `json:"id"`
	ContextID string         `json:"contextId"`
	Status    TaskStatus     `json:"status"`
	Artifacts []Artifact     `json:"artifacts,omitempty"`
	History   []Message      `json:"history,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// --- Agent Discovery ---

// AgentCard describes an agent's capabilities and endpoints.
//
// A card always encodes the fields A2A 1.0 marks REQUIRED (name, description,
// supportedInterfaces, version, capabilities, defaultInputModes,
// defaultOutputModes and skills), as empty values when unset, so a strict
// client finds every one of them.
type AgentCard struct {
	Name                string            `json:"name"`
	Description         string            `json:"description"`
	Version             string            `json:"version"`
	Provider            *AgentProvider    `json:"provider,omitempty"`
	Capabilities        AgentCapabilities `json:"capabilities"`
	Skills              []AgentSkill      `json:"skills"`
	DefaultInputModes   []string          `json:"defaultInputModes"`
	DefaultOutputModes  []string          `json:"defaultOutputModes"`
	SupportedInterfaces []AgentInterface  `json:"supportedInterfaces"`
	IconURL             string            `json:"iconUrl,omitempty"`
	DocumentationURL    string            `json:"documentationUrl,omitempty"`

	// SecuritySchemes declares the authentication schemes the agent accepts,
	// keyed by a name SecurityRequirements refer to (A2A 1.0 §7.3).
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty"`
	// SecurityRequirements lists the scheme combinations a caller must
	// satisfy; any one requirement suffices.
	SecurityRequirements []SecurityRequirement `json:"securityRequirements,omitempty"`

	// Signatures are JSON Web Signatures computed over the card (A2A 1.0
	// §8.4). PromptKit carries them; it does not compute or verify them.
	Signatures []AgentCardSignature `json:"signatures,omitempty"`
}

// AgentCardSignature is a JWS signature of an AgentCard in RFC 7515 JSON
// form (A2A 1.0 §8.4.2).
type AgentCardSignature struct {
	// Protected is the base64url-encoded JWS protected header.
	Protected string `json:"protected"`
	// Signature is the base64url-encoded signature.
	Signature string `json:"signature"`
	// Header holds the unprotected JWS header values.
	Header map[string]any `json:"header,omitempty"`
}

// AgentSkill describes a specific skill an agent can perform. Description and
// tags are REQUIRED in A2A 1.0 and always encoded.
type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`

	SecurityRequirements []SecurityRequirement `json:"securityRequirements,omitempty"`
}

// AgentProvider identifies the organization behind an agent. Both fields
// are REQUIRED in A2A 1.0.
type AgentProvider struct {
	Organization string `json:"organization"`
	URL          string `json:"url"`
}

// AgentCapabilities describes what the agent supports.
type AgentCapabilities struct {
	Streaming         bool             `json:"streaming,omitempty"`
	PushNotifications bool             `json:"pushNotifications,omitempty"`
	ExtendedAgentCard bool             `json:"extendedAgentCard,omitempty"`
	Extensions        []AgentExtension `json:"extensions,omitempty"`
}

// AgentInterface describes a protocol endpoint. URL, ProtocolBinding and
// ProtocolVersion are REQUIRED in A2A 1.0 and always encoded.
type AgentInterface struct {
	URL string `json:"url"`
	// ProtocolBinding is the transport: [ProtocolBindingJSONRPC], "GRPC" or
	// "HTTP+JSON".
	ProtocolBinding string `json:"protocolBinding"`
	// ProtocolVersion is the A2A Major.Minor version served at URL.
	ProtocolVersion string `json:"protocolVersion"`
	Tenant          string `json:"tenant,omitempty"`
}

// AgentExtension describes an optional protocol extension.
type AgentExtension struct {
	URI         string `json:"uri"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	// Params is the extension's own configuration, passed through as is.
	Params map[string]any `json:"params,omitempty"`
}

// --- JSON-RPC Envelope ---

// JSONRPCRequest is a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse is a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError is a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// --- Method Params / Responses ---

// SendMessageRequest is the params for SendMessage and SendStreamingMessage
// (0.3: message/send, message/stream).
type SendMessageRequest struct {
	Tenant        string                    `json:"tenant,omitempty"`
	Message       Message                   `json:"message"`
	Configuration *SendMessageConfiguration `json:"configuration,omitempty"`
	Metadata      map[string]any            `json:"metadata,omitempty"`
}

// SendMessageConfiguration controls message handling.
//
// Blocking and ReturnImmediately are the same switch in two versions: A2A 0.3
// sends blocking (default false), 1.0 sends returnImmediately (default false,
// so 1.0 blocks by default). A server reads the one its caller's version
// defines; see [SendMessageConfiguration.WaitsForCompletion].
type SendMessageConfiguration struct {
	AcceptedOutputModes []string `json:"acceptedOutputModes,omitempty"`
	HistoryLength       *int     `json:"historyLength,omitempty"`
	// Blocking is A2A 0.3's switch.
	Blocking bool `json:"blocking,omitempty"`
	// ReturnImmediately is A2A 1.0's switch.
	ReturnImmediately bool `json:"returnImmediately,omitempty"`
	// TaskPushNotificationConfig asks the agent to push the task's updates to
	// a webhook (0.3: pushNotificationConfig). Only an agent whose card
	// declares capabilities.pushNotifications honors it; a PromptKit server
	// does not, and refuses a message that carries one with
	// PushNotificationNotSupportedError.
	TaskPushNotificationConfig *TaskPushNotificationConfig `json:"taskPushNotificationConfig,omitempty"`
}

// TaskPushNotificationConfig is where, and with what credentials, an agent
// pushes a task's updates (A2A 1.0 §4.3.1). In a SendMessage configuration
// TaskID is left empty.
type TaskPushNotificationConfig struct {
	Tenant string `json:"tenant,omitempty"`
	ID     string `json:"id,omitempty"`
	TaskID string `json:"taskId,omitempty"`
	// URL is the webhook the agent POSTs to. REQUIRED.
	URL string `json:"url"`
	// Token is a value unique to the task or session, sent with each
	// notification so the receiver can check it.
	Token          string              `json:"token,omitempty"`
	Authentication *AuthenticationInfo `json:"authentication,omitempty"`
}

// AuthenticationInfo is how an agent authenticates to a push webhook (A2A
// 1.0 §4.3.2).
type AuthenticationInfo struct {
	// Scheme is an HTTP authentication scheme (Bearer, Basic, ...). REQUIRED.
	Scheme string `json:"scheme"`
	// Credentials are the scheme's credentials, a token for Bearer.
	Credentials string `json:"credentials,omitempty"`
}

// WaitsForCompletion reports whether a SendMessage under version v should
// wait for the task to finish or be interrupted before answering. A nil
// configuration takes the version's default.
func (c *SendMessageConfiguration) WaitsForCompletion(v ProtocolVersion) bool {
	if v == ProtocolVersion03 {
		return c != nil && c.Blocking
	}
	return c == nil || !c.ReturnImmediately
}

// GetTaskRequest is the params for GetTask (0.3: tasks/get).
type GetTaskRequest struct {
	Tenant        string `json:"tenant,omitempty"`
	ID            string `json:"id"`
	HistoryLength *int   `json:"historyLength,omitempty"`
}

// CancelTaskRequest is the params for CancelTask (0.3: tasks/cancel).
type CancelTaskRequest struct {
	Tenant string `json:"tenant,omitempty"`
	ID     string `json:"id"`
}

// ListTasksRequest is the params for ListTasks (1.0 only).
type ListTasksRequest struct {
	Tenant               string     `json:"tenant,omitempty"`
	ContextID            string     `json:"contextId,omitempty"`
	Status               *TaskState `json:"status,omitempty"`
	PageSize             int        `json:"pageSize,omitempty"`
	PageToken            string     `json:"pageToken,omitempty"`
	HistoryLength        *int       `json:"historyLength,omitempty"`
	StatusTimestampAfter *time.Time `json:"statusTimestampAfter,omitempty"`
	// IncludeArtifacts asks for each task's artifacts; they are omitted by
	// default.
	IncludeArtifacts bool `json:"includeArtifacts,omitempty"`
}

// ListTasksResponse is the result for ListTasks. NextPageToken is empty on
// the last page.
type ListTasksResponse struct {
	Tasks         []Task `json:"tasks"`
	NextPageToken string `json:"nextPageToken"`
	PageSize      int    `json:"pageSize"`
	TotalSize     int    `json:"totalSize"`
}

// SubscribeTaskRequest is the params for SubscribeToTask (0.3:
// tasks/resubscribe).
type SubscribeTaskRequest struct {
	Tenant string `json:"tenant,omitempty"`
	ID     string `json:"id"`
}

// SendMessageResponse is the A2A 1.0 result of SendMessage: exactly one of
// Task or Message is set. (0.3 returns the Task or Message bare.)
type SendMessageResponse struct {
	Task    *Task    `json:"task,omitempty"`
	Message *Message `json:"message,omitempty"`
}

// StreamResponse is one A2A 1.0 streaming result: exactly one field is set.
// (0.3 sends each object bare, discriminated by its "kind".)
type StreamResponse struct {
	Task           *Task                    `json:"task,omitempty"`
	Message        *Message                 `json:"message,omitempty"`
	StatusUpdate   *TaskStatusUpdateEvent   `json:"statusUpdate,omitempty"`
	ArtifactUpdate *TaskArtifactUpdateEvent `json:"artifactUpdate,omitempty"`
}

// --- Streaming Events ---

// TaskStatusUpdateEvent is sent during streaming when a task's status changes.
type TaskStatusUpdateEvent struct {
	TaskID    string         `json:"taskId"`
	ContextID string         `json:"contextId"`
	Status    TaskStatus     `json:"status"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// TaskArtifactUpdateEvent is sent during streaming when an artifact is produced.
type TaskArtifactUpdateEvent struct {
	TaskID    string         `json:"taskId"`
	ContextID string         `json:"contextId"`
	Artifact  Artifact       `json:"artifact"`
	Append    bool           `json:"append,omitempty"`
	LastChunk bool           `json:"lastChunk,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}
