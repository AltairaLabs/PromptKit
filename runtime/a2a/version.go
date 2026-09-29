package a2a

import (
	"fmt"
	"strings"
)

// ProtocolVersion is an A2A protocol version, as Major.Minor (A2A 1.0 §3.6).
type ProtocolVersion string

// Supported protocol versions.
const (
	ProtocolVersion10 ProtocolVersion = "1.0"
	ProtocolVersion03 ProtocolVersion = "0.3"
)

// HeaderVersion is the service parameter carrying the protocol version a
// request speaks. A request without it is 0.3 (A2A 1.0 §3.6.2).
const HeaderVersion = "A2A-Version"

// ProtocolBindingJSONRPC is the AgentInterface binding name of the JSON-RPC
// transport (0.3 calls the same field "transport").
const ProtocolBindingJSONRPC = "JSONRPC"

// v03CardProtocolVersion is what a 0.3 agent card declares in protocolVersion.
const v03CardProtocolVersion = "0.3.0"

// ParseProtocolVersion resolves an A2A-Version value to a supported version.
// Patch numbers are ignored, as the spec requires. An empty value returns
// ("", nil): the caller decides what an unversioned request means.
func ParseProtocolVersion(s string) (ProtocolVersion, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	// Major.Minor decides; a patch number is ignored.
	if major, rest, ok := strings.Cut(s, "."); ok {
		minor, _, _ := strings.Cut(rest, ".")
		switch v := ProtocolVersion(major + "." + minor); v {
		case ProtocolVersion10, ProtocolVersion03:
			return v, nil
		}
	}
	return "", fmt.Errorf("a2a: protocol version %q is not supported (supported: %s, %s)",
		s, ProtocolVersion10, ProtocolVersion03)
}

// Operation is a protocol operation, independent of any version's name for it.
type Operation int

// Protocol operations.
const (
	OpUnknown Operation = iota
	OpSendMessage
	OpSendStreamingMessage
	OpGetTask
	OpCancelTask
	OpListTasks
	OpSubscribeToTask
	OpPushNotificationConfig
	OpGetExtendedAgentCard
)

// methodTable maps every method name a PromptKit server answers to its
// operation and the version whose name it is.
var methodTable = map[string]struct {
	op Operation
	v  ProtocolVersion
}{
	MethodV1SendMessage:                      {OpSendMessage, ProtocolVersion10},
	MethodV1SendStreamingMessage:             {OpSendStreamingMessage, ProtocolVersion10},
	MethodV1GetTask:                          {OpGetTask, ProtocolVersion10},
	MethodV1CancelTask:                       {OpCancelTask, ProtocolVersion10},
	MethodV1ListTasks:                        {OpListTasks, ProtocolVersion10},
	MethodV1SubscribeToTask:                  {OpSubscribeToTask, ProtocolVersion10},
	MethodV1GetExtendedAgentCard:             {OpGetExtendedAgentCard, ProtocolVersion10},
	MethodV1CreateTaskPushNotificationConfig: {OpPushNotificationConfig, ProtocolVersion10},
	MethodV1GetTaskPushNotificationConfig:    {OpPushNotificationConfig, ProtocolVersion10},
	MethodV1ListTaskPushNotificationConfigs:  {OpPushNotificationConfig, ProtocolVersion10},
	MethodV1DeleteTaskPushNotificationConfig: {OpPushNotificationConfig, ProtocolVersion10},

	MethodV03SendMessage:                  {OpSendMessage, ProtocolVersion03},
	MethodV03SendStreamingMessage:         {OpSendStreamingMessage, ProtocolVersion03},
	MethodV03GetTask:                      {OpGetTask, ProtocolVersion03},
	MethodV03CancelTask:                   {OpCancelTask, ProtocolVersion03},
	MethodV03Resubscribe:                  {OpSubscribeToTask, ProtocolVersion03},
	MethodV03GetExtendedCard:              {OpGetExtendedAgentCard, ProtocolVersion03},
	MethodV03SetPushNotificationConfig:    {OpPushNotificationConfig, ProtocolVersion03},
	MethodV03GetPushNotificationConfig:    {OpPushNotificationConfig, ProtocolVersion03},
	MethodV03ListPushNotificationConfig:   {OpPushNotificationConfig, ProtocolVersion03},
	MethodV03DeletePushNotificationConfig: {OpPushNotificationConfig, ProtocolVersion03},

	MethodLegacyListTasks: {OpListTasks, ProtocolVersion03},
	MethodLegacySubscribe: {OpSubscribeToTask, ProtocolVersion03},
}

// LookupMethod resolves a JSON-RPC method name to its operation and the
// version that names it that way. ok is false for an unknown method.
func LookupMethod(method string) (op Operation, v ProtocolVersion, ok bool) {
	e, ok := methodTable[method]
	return e.op, e.v, ok
}

// methodNames is each version's method name for each operation. Push
// notification config has several methods and no single name.
var methodNames = map[ProtocolVersion]map[Operation]string{
	ProtocolVersion10: {
		OpSendMessage:          MethodV1SendMessage,
		OpSendStreamingMessage: MethodV1SendStreamingMessage,
		OpGetTask:              MethodV1GetTask,
		OpCancelTask:           MethodV1CancelTask,
		OpListTasks:            MethodV1ListTasks,
		OpSubscribeToTask:      MethodV1SubscribeToTask,
		OpGetExtendedAgentCard: MethodV1GetExtendedAgentCard,
	},
	ProtocolVersion03: {
		OpSendMessage:          MethodV03SendMessage,
		OpSendStreamingMessage: MethodV03SendStreamingMessage,
		OpGetTask:              MethodV03GetTask,
		OpCancelTask:           MethodV03CancelTask,
		OpListTasks:            MethodLegacyListTasks,
		OpSubscribeToTask:      MethodV03Resubscribe,
		OpGetExtendedAgentCard: MethodV03GetExtendedCard,
	},
}

// Method returns version v's JSON-RPC method name for op (1.0's for any
// version other than 0.3). 0.3 has no ListTasks; the legacy PromptKit name is
// returned for it.
func (v ProtocolVersion) Method(op Operation) string {
	if v != ProtocolVersion03 {
		v = ProtocolVersion10
	}
	return methodNames[v][op]
}

// declaredVersion returns the version the card's JSON-RPC interfaces call
// for, and whether they declare one at all.
func (card *AgentCard) declaredVersion() (ProtocolVersion, bool) {
	if card == nil {
		return "", false
	}
	for _, iface := range card.SupportedInterfaces {
		if strings.EqualFold(iface.ProtocolBinding, ProtocolBindingJSONRPC) {
			return card.PreferredVersion(), true
		}
	}
	return "", false
}

// PreferredVersion picks the protocol version to speak to the agent that
// published card: 1.0 when any JSON-RPC interface declares it (or the card
// declares nothing), otherwise 0.3.
func (card *AgentCard) PreferredVersion() ProtocolVersion {
	if card == nil || len(card.SupportedInterfaces) == 0 {
		return ProtocolVersion10
	}
	sawJSONRPC := false
	for _, iface := range card.SupportedInterfaces {
		if !strings.EqualFold(iface.ProtocolBinding, ProtocolBindingJSONRPC) {
			continue
		}
		sawJSONRPC = true
		v, err := ParseProtocolVersion(iface.ProtocolVersion)
		if err == nil && v == ProtocolVersion10 {
			return ProtocolVersion10
		}
	}
	if sawJSONRPC {
		return ProtocolVersion03
	}
	return ProtocolVersion10
}
