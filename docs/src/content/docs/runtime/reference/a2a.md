---
title: A2A Reference
description: API reference for the runtime/a2a package — client, types, tool bridge, mock, streaming
sidebar:
  order: 7
---

API reference for the `runtime/a2a` package: client, protocol types, tool bridge, mock server, and streaming.

---

## Client

```go
import "github.com/AltairaLabs/PromptKit/runtime/a2a"
```

### NewClient

```go
func NewClient(baseURL string, opts ...ClientOption) *Client
```

Creates a new A2A client targeting `baseURL`. The URL should not include a trailing slash.

### Client Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `Discover` | `(ctx) (*AgentCard, error)` | Fetches the agent card from `/.well-known/agent.json`. Cached after first call. |
| `SendMessage` | `(ctx, *SendMessageRequest) (*Task, error)` | Sends a `message/send` JSON-RPC request. Returns the task. |
| `SendMessageStream` | `(ctx, *SendMessageRequest) (<-chan StreamEvent, error)` | Sends a `message/stream` request. Returns a channel of SSE events. |
| `GetTask` | `(ctx, taskID string) (*Task, error)` | Retrieves a task by ID via `tasks/get`. |
| `CancelTask` | `(ctx, taskID string) error` | Cancels a task via `tasks/cancel`. |
| `ListTasks` | `(ctx, *ListTasksRequest) ([]*Task, error)` | Lists tasks via `tasks/list`. |

### Client Options

| Option | Description |
|--------|-------------|
| `WithHTTPClient(hc *http.Client)` | Sets the underlying HTTP client. |
| `WithAuth(scheme, token string)` | Sets the `Authorization` header (`scheme token`) on all requests. |

### RPCError

```go
type RPCError struct {
    Code    int
    Message string
}
```

Returned by client methods when the server responds with a JSON-RPC error.

---

## Types

### AgentCard

```go
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
}
```

### AgentSkill

```go
type AgentSkill struct {
    ID          string   `json:"id"`
    Name        string   `json:"name"`
    Description string   `json:"description,omitempty"`
    Tags        []string `json:"tags,omitempty"`
    Examples    []string `json:"examples,omitempty"`
    InputModes  []string `json:"inputModes,omitempty"`
    OutputModes []string `json:"outputModes,omitempty"`
}
```

### AgentCapabilities

```go
type AgentCapabilities struct {
    Streaming         bool             `json:"streaming,omitempty"`
    PushNotifications bool             `json:"pushNotifications,omitempty"`
    ExtendedAgentCard bool             `json:"extendedAgentCard,omitempty"`
    Extensions        []AgentExtension `json:"extensions,omitempty"`
}
```

### Task

```go
type Task struct {
    ID        string         `json:"id"`
    ContextID string         `json:"contextId"`
    Status    TaskStatus     `json:"status"`
    Artifacts []Artifact     `json:"artifacts,omitempty"`
    History   []Message      `json:"history,omitempty"`
    Metadata  map[string]any `json:"metadata,omitempty"`
}
```

### TaskState

```go
type TaskState string

const (
    TaskStateSubmitted     TaskState = "submitted"
    TaskStateWorking       TaskState = "working"
    TaskStateCompleted     TaskState = "completed"
    TaskStateFailed        TaskState = "failed"
    TaskStateCanceled      TaskState = "canceled"
    TaskStateInputRequired TaskState = "input_required"
    TaskStateRejected      TaskState = "rejected"
    TaskStateAuthRequired  TaskState = "auth_required"
)
```

### TaskStatus

```go
type TaskStatus struct {
    State     TaskState  `json:"state"`
    Message   *Message   `json:"message,omitempty"`
    Timestamp *time.Time `json:"timestamp,omitempty"`
}
```

### Message

```go
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
```

### Part

```go
type Part struct {
    Text      *string        `json:"text,omitempty"`
    Raw       []byte         `json:"raw,omitempty"`
    URL       *string        `json:"url,omitempty"`
    Data      map[string]any `json:"data,omitempty"`
    Metadata  map[string]any `json:"metadata,omitempty"`
    Filename  string         `json:"filename,omitempty"`
    MediaType string         `json:"mediaType,omitempty"`
}
```

### Artifact

```go
type Artifact struct {
    ArtifactID  string         `json:"artifactId"`
    Name        string         `json:"name,omitempty"`
    Description string         `json:"description,omitempty"`
    Parts       []Part         `json:"parts"`
    Extensions  []string       `json:"extensions,omitempty"`
    Metadata    map[string]any `json:"metadata,omitempty"`
}
```

### SendMessageRequest

```go
type SendMessageRequest struct {
    Message       Message                   `json:"message"`
    Configuration *SendMessageConfiguration `json:"configuration,omitempty"`
    Metadata      map[string]any            `json:"metadata,omitempty"`
}
```

### SendMessageConfiguration

```go
type SendMessageConfiguration struct {
    AcceptedOutputModes []string `json:"acceptedOutputModes,omitempty"`
    HistoryLength       *int     `json:"historyLength,omitempty"`
    Blocking            bool     `json:"blocking,omitempty"`
}
```

### Role

```go
type Role string

const (
    RoleUser  Role = "user"
    RoleAgent Role = "agent"
)
```

---

## Streaming

### StreamEvent

```go
type StreamEvent struct {
    StatusUpdate   *TaskStatusUpdateEvent
    ArtifactUpdate *TaskArtifactUpdateEvent
}
```

Returned by `Client.SendMessageStream`. Exactly one field is non-nil per event.

### TaskStatusUpdateEvent

```go
type TaskStatusUpdateEvent struct {
    TaskID    string         `json:"taskId"`
    ContextID string         `json:"contextId"`
    Status    TaskStatus     `json:"status"`
    Metadata  map[string]any `json:"metadata,omitempty"`
}
```

### TaskArtifactUpdateEvent

```go
type TaskArtifactUpdateEvent struct {
    TaskID    string         `json:"taskId"`
    ContextID string         `json:"contextId"`
    Artifact  Artifact       `json:"artifact"`
    Append    bool           `json:"append,omitempty"`
    LastChunk bool           `json:"lastChunk,omitempty"`
    Metadata  map[string]any `json:"metadata,omitempty"`
}
```

### ReadSSE

```go
func ReadSSE(ctx context.Context, r io.Reader, ch chan<- StreamEvent)
```

Reads SSE events from `r` and sends parsed `StreamEvent` values to `ch`. Handles both raw event objects and JSON-RPC wrapped responses.

---

## ToolBridge

```go
import "github.com/AltairaLabs/PromptKit/runtime/a2a"
```

### NewToolBridge

```go
func NewToolBridge(client *Client) *ToolBridge
```

Creates a `ToolBridge` backed by the given A2A client.

### ToolBridge Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `RegisterAgent` | `(ctx) ([]*tools.ToolDescriptor, error)` | Discovers the agent and creates tool descriptors for each skill. |
| `GetToolDescriptors` | `() []*tools.ToolDescriptor` | Returns all accumulated tool descriptors. |

### Tool Name Format

```
a2a__{sanitized_agent_name}__{sanitized_skill_id}
```

Sanitization: lowercase, non-alphanumeric runs replaced with `_`, leading/trailing underscores trimmed.

---

## Mock Server

```go
import "github.com/AltairaLabs/PromptKit/runtime/a2a/mock"
```

### NewA2AServer

```go
func NewA2AServer(card *a2a.AgentCard, opts ...Option) *A2AServer
```

Creates a mock server. Call `Start()` to begin serving.

### A2AServer Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `Start` | `() (string, error)` | Starts the httptest server, returns its URL. |
| `Close` | `()` | Shuts down the server. |
| `URL` | `() string` | Returns the URL of the running server. Panics if not started. |

### Mock Options

| Option | Description |
|--------|-------------|
| `WithSkillResponse(skillID, Response)` | Returns the response for the given skill. |
| `WithSkillError(skillID, errMsg string)` | Returns a failed task for the given skill. |
| `WithLatency(d time.Duration)` | Adds delay before processing each request. |
| `WithInputMatcher(skillID, fn, Response)` | Matches skill + custom function. First match wins. |

### Mock Config Types

```go
type AgentConfig struct {
    Name      string
    Card      a2a.AgentCard
    Responses []RuleConfig
}

type RuleConfig struct {
    Skill    string
    Match    *MatchConfig
    Response *ResponseConfig
    Error    string
}

type MatchConfig struct {
    Contains string  // case-insensitive substring match
    Regex    string  // regex match
}

type ResponseConfig struct {
    Parts []PartConfig
}

type PartConfig struct {
    Text string
}
```

### OptionsFromConfig

```go
func OptionsFromConfig(cfg *AgentConfig) []Option
```

Converts an `AgentConfig` into a slice of `Option` values for `NewA2AServer`.

---

## Arena Config Types

```go
import "github.com/AltairaLabs/PromptKit/pkg/config"
```

These types are used in Arena's YAML configuration:

### A2AAgentConfig

```go
type A2AAgentConfig struct {
    Name      string            `yaml:"name"`
    Card      A2ACardConfig     `yaml:"card"`
    Responses []A2AResponseRule `yaml:"responses"`
}
```

### A2ACardConfig

```go
type A2ACardConfig struct {
    Name        string           `yaml:"name"`
    Description string           `yaml:"description"`
    Skills      []A2ASkillConfig `yaml:"skills"`
}
```

### A2ASkillConfig

```go
type A2ASkillConfig struct {
    ID          string   `yaml:"id"`
    Name        string   `yaml:"name"`
    Description string   `yaml:"description"`
    Tags        []string `yaml:"tags,omitempty"`
}
```

### A2AResponseRule

```go
type A2AResponseRule struct {
    Skill    string             `yaml:"skill"`
    Match    *A2AMatchConfig    `yaml:"match,omitempty"`
    Response *A2AResponseConfig `yaml:"response,omitempty"`
    Error    string             `yaml:"error,omitempty"`
}
```

### A2AMatchConfig

```go
type A2AMatchConfig struct {
    Contains string `yaml:"contains,omitempty"`
    Regex    string `yaml:"regex,omitempty"`
}
```

### A2AResponseConfig

```go
type A2AResponseConfig struct {
    Parts []A2APartConfig `yaml:"parts"`
}
```

---

## JSON-RPC Types

### JSONRPCRequest

```go
type JSONRPCRequest struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      any             `json:"id"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
}
```

### JSONRPCResponse

```go
type JSONRPCResponse struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      any             `json:"id"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *JSONRPCError   `json:"error,omitempty"`
}
```

### JSONRPCError

```go
type JSONRPCError struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
    Data    any    `json:"data,omitempty"`
}
```

---

## Method Constants

```go
const (
    MethodSendMessage          = "message/send"
    MethodSendStreamingMessage = "message/stream"
    MethodGetTask              = "tasks/get"
    MethodCancelTask           = "tasks/cancel"
    MethodListTasks            = "tasks/list"
    MethodTaskSubscribe        = "tasks/subscribe"
)
```

---

## See Also

- [A2A Concept](/concepts/a2a/) — protocol design and concepts
- [A2A Client Tutorial](/runtime/tutorials/07-a2a-client/) — hands-on walkthrough
- [Tool Bridge How-To](/runtime/how-to/use-a2a-tool-bridge/) — usage patterns
- [SDK A2A Reference](/sdk/reference/a2a-server/) — server, task store, opener API
