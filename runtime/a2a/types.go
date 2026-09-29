// Package a2a provides types for the Agent-to-Agent (A2A) protocol.
//
// Types are derived from the A2A protocol specification (a2a.proto)
// and use camelCase JSON tags matching the JSON-RPC binding convention.
package a2a

import (
	"encoding/json"
	"fmt"
	"time"
)

// A2A JSON-RPC method names, as A2A 1.0 (§5.3) names them.
const (
	MethodSendMessage          = "SendMessage"
	MethodSendStreamingMessage = "SendStreamingMessage"
	MethodGetTask              = "GetTask"
	MethodCancelTask           = "CancelTask"
	MethodListTasks            = "ListTasks"
	MethodSubscribeToTask      = "SubscribeToTask"
	MethodGetExtendedAgentCard = "GetExtendedAgentCard"

	MethodCreateTaskPushNotificationConfig = "CreateTaskPushNotificationConfig"
	MethodGetTaskPushNotificationConfig    = "GetTaskPushNotificationConfig"
	MethodListTaskPushNotificationConfigs  = "ListTaskPushNotificationConfigs"
	MethodDeleteTaskPushNotificationConfig = "DeleteTaskPushNotificationConfig"

	// MethodTaskSubscribe is the pre-1.0 name of [MethodSubscribeToTask].
	//
	// Deprecated: use MethodSubscribeToTask.
	MethodTaskSubscribe = MethodSubscribeToTask
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
	MethodLegacyListTasks = "tasks/list"
	MethodLegacySubscribe = "tasks/subscribe"
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
func (s *TaskState) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	state, err := ParseTaskState(str)
	if err != nil {
		return err
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
// Exactly one of Text, Raw, URL, or Data should be set.
type Part struct {
	Text *string        `json:"text,omitempty"`
	Raw  []byte         `json:"raw,omitempty"`
	URL  *string        `json:"url,omitempty"`
	Data map[string]any `json:"data,omitempty"`

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
		Kind string   `json:"kind"`
		File *v03File `json:"file"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*p = Part(wire.plain)
	if wire.File != nil {
		if err := wire.File.into(p); err != nil {
			return err
		}
	}
	return nil
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
type AgentCard struct {
	Name                string            `json:"name"`
	Description         string            `json:"description,omitempty"`
	Version             string            `json:"version,omitempty"`
	Provider            *AgentProvider    `json:"provider,omitempty"`
	Capabilities        AgentCapabilities `json:"capabilities,omitzero"`
	Skills              []AgentSkill      `json:"skills,omitempty"`
	DefaultInputModes   []string          `json:"defaultInputModes,omitempty"`
	DefaultOutputModes  []string          `json:"defaultOutputModes,omitempty"`
	SupportedInterfaces []AgentInterface  `json:"supportedInterfaces,omitempty"`
	IconURL             string            `json:"iconUrl,omitempty"`
	DocumentationURL    string            `json:"documentationUrl,omitempty"`

	// SecuritySchemes declares the authentication schemes the agent accepts,
	// keyed by a name SecurityRequirements refer to (A2A 1.0 §7.3).
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty"`
	// SecurityRequirements lists the scheme combinations a caller must
	// satisfy; any one requirement suffices.
	SecurityRequirements []SecurityRequirement `json:"securityRequirements,omitempty"`
}

// AgentSkill describes a specific skill an agent can perform.
type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`

	SecurityRequirements []SecurityRequirement `json:"securityRequirements,omitempty"`
}

// AgentProvider identifies the organization behind an agent.
type AgentProvider struct {
	Organization string `json:"organization"`
	URL          string `json:"url,omitempty"`
}

// AgentCapabilities describes what the agent supports.
type AgentCapabilities struct {
	Streaming         bool             `json:"streaming,omitempty"`
	PushNotifications bool             `json:"pushNotifications,omitempty"`
	ExtendedAgentCard bool             `json:"extendedAgentCard,omitempty"`
	Extensions        []AgentExtension `json:"extensions,omitempty"`
}

// AgentInterface describes a protocol endpoint.
type AgentInterface struct {
	URL string `json:"url"`
	// ProtocolBinding is the transport: [ProtocolBindingJSONRPC], "GRPC" or
	// "HTTP+JSON".
	ProtocolBinding string `json:"protocolBinding,omitempty"`
	// ProtocolVersion is the A2A Major.Minor version served at URL.
	ProtocolVersion string `json:"protocolVersion,omitempty"`
	Tenant          string `json:"tenant,omitempty"`
}

// AgentExtension describes an optional protocol extension.
type AgentExtension struct {
	URI         string         `json:"uri"`
	Description string         `json:"description,omitempty"`
	Required    bool           `json:"required,omitempty"`
	Params      map[string]any `json:"params,omitempty"`
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
	Tenant   string         `json:"tenant,omitempty"`
	ID       string         `json:"id"`
	Metadata map[string]any `json:"metadata,omitempty"`
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
