package mock

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/testutil"
	"github.com/AltairaLabs/PromptKit/runtime/a2a"
)

// testCard returns an AgentCard suitable for tests.
func testCard() *a2a.AgentCard {
	return &a2a.AgentCard{
		Name:        "test-agent",
		Description: "A mock agent for testing",
		Skills: []a2a.AgentSkill{
			{ID: "echo", Name: "Echo"},
			{ID: "fail", Name: "Fail"},
		},
	}
}

// sendRPC is a test helper that performs a raw JSON-RPC POST.
func sendRPC(t *testing.T, url, method string, params any) a2a.JSONRPCResponse {
	t.Helper()
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	body, err := json.Marshal(a2a.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  paramsJSON,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	resp, err := http.Post(url+"/a2a", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /a2a: %v", err)
	}
	defer resp.Body.Close()
	var rpcResp a2a.JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return rpcResp
}

// decodeTask unmarshals a Task from a JSONRPCResponse result.
func decodeTask(t *testing.T, resp a2a.JSONRPCResponse) a2a.Task {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("unexpected RPC error: %d %s", resp.Error.Code, resp.Error.Message)
	}
	var task a2a.Task
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		t.Fatalf("unmarshal task: %v", err)
	}
	return task
}

// sendMsg builds a SendMessageRequest with the given skill ID and text.
func sendMsg(skillID, text string) *a2a.SendMessageRequest {
	req := &a2a.SendMessageRequest{
		Message: a2a.Message{
			Role:  a2a.RoleUser,
			Parts: []a2a.Part{{Text: testutil.Ptr(text)}},
		},
	}
	if skillID != "" {
		req.Message.Metadata = map[string]any{"skillId": skillID}
	}
	return req
}

func TestMockServesAgentCard(t *testing.T) {
	m := NewA2AServer(testCard())
	m.Start()
	defer m.Close()

	resp, err := http.Get(m.URL() + "/.well-known/agent.json")
	if err != nil {
		t.Fatalf("GET agent.json: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var card a2a.AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if card.Name != "test-agent" {
		t.Errorf("name = %q, want %q", card.Name, "test-agent")
	}
	if len(card.Skills) != 2 {
		t.Errorf("skills = %d, want 2", len(card.Skills))
	}
}

func TestMockReturnsSkillResponse(t *testing.T) {
	m := NewA2AServer(testCard(),
		WithSkillResponse("echo", Response{
			Parts: []a2a.Part{{Text: testutil.Ptr("hello back")}},
		}),
	)
	m.Start()
	defer m.Close()

	resp := sendRPC(t, m.URL(), a2a.MethodSendMessage, sendMsg("echo", "hello"))
	task := decodeTask(t, resp)

	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", task.Status.State)
	}
	if len(task.Artifacts) == 0 || len(task.Artifacts[0].Parts) == 0 {
		t.Fatal("no artifacts returned")
	}
	if got := task.Artifacts[0].Parts[0].Text; got == nil || *got != "hello back" {
		t.Errorf("text = %v, want %q", got, "hello back")
	}
}

func TestMockMatchesInputContains(t *testing.T) {
	m := NewA2AServer(testCard(),
		WithInputMatcher("echo", func(msg a2a.Message) bool {
			return strings.Contains(messageText(&msg), "magic")
		}, Response{
			Parts: []a2a.Part{{Text: testutil.Ptr("found magic")}},
		}),
		WithSkillResponse("echo", Response{
			Parts: []a2a.Part{{Text: testutil.Ptr("default echo")}},
		}),
	)
	m.Start()
	defer m.Close()

	// Should match the input matcher.
	resp := sendRPC(t, m.URL(), a2a.MethodSendMessage, sendMsg("echo", "say the magic word"))
	task := decodeTask(t, resp)
	if got := *task.Artifacts[0].Parts[0].Text; got != "found magic" {
		t.Errorf("text = %q, want %q", got, "found magic")
	}

	// Should fall through to default.
	resp2 := sendRPC(t, m.URL(), a2a.MethodSendMessage, sendMsg("echo", "ordinary message"))
	task2 := decodeTask(t, resp2)
	if got := *task2.Artifacts[0].Parts[0].Text; got != "default echo" {
		t.Errorf("text = %q, want %q", got, "default echo")
	}
}

func TestMockMatchesInputRegex(t *testing.T) {
	mc := &MatchConfig{Regex: `\d{3}-\d{4}`}
	fn := matcherFromConfig(mc)

	msg := a2a.Message{Parts: []a2a.Part{{Text: testutil.Ptr("call 555-1234")}}}
	if !fn(msg) {
		t.Error("expected regex match")
	}

	msg2 := a2a.Message{Parts: []a2a.Part{{Text: testutil.Ptr("no numbers here")}}}
	if fn(msg2) {
		t.Error("expected no regex match")
	}
}

func TestMockReturnsError(t *testing.T) {
	m := NewA2AServer(testCard(),
		WithSkillError("fail", "something broke"),
	)
	m.Start()
	defer m.Close()

	resp := sendRPC(t, m.URL(), a2a.MethodSendMessage, sendMsg("fail", "do something"))
	task := decodeTask(t, resp)

	if task.Status.State != a2a.TaskStateFailed {
		t.Fatalf("state = %q, want failed", task.Status.State)
	}
	if task.Status.Message == nil || len(task.Status.Message.Parts) == 0 {
		t.Fatal("no error message in status")
	}
	if got := *task.Status.Message.Parts[0].Text; got != "something broke" {
		t.Errorf("error = %q, want %q", got, "something broke")
	}
}

func TestMockLatencyInjection(t *testing.T) {
	delay := 50 * time.Millisecond
	m := NewA2AServer(testCard(),
		WithLatency(delay),
		WithSkillResponse("echo", Response{
			Parts: []a2a.Part{{Text: testutil.Ptr("delayed")}},
		}),
	)
	m.Start()
	defer m.Close()

	start := time.Now()
	resp := sendRPC(t, m.URL(), a2a.MethodSendMessage, sendMsg("echo", "hi"))
	elapsed := time.Since(start)

	decodeTask(t, resp) // verify valid response

	if elapsed < delay {
		t.Errorf("elapsed %v < latency %v", elapsed, delay)
	}
}

func TestMockDefaultResponse(t *testing.T) {
	m := NewA2AServer(testCard(),
		WithSkillResponse("echo", Response{
			Parts: []a2a.Part{{Text: testutil.Ptr("default")}},
		}),
	)
	m.Start()
	defer m.Close()

	// Any message with skill "echo" should match.
	resp := sendRPC(t, m.URL(), a2a.MethodSendMessage, sendMsg("echo", "anything"))
	task := decodeTask(t, resp)

	if got := *task.Artifacts[0].Parts[0].Text; got != "default" {
		t.Errorf("text = %q, want %q", got, "default")
	}
}

func TestMockRuleOrdering(t *testing.T) {
	m := NewA2AServer(testCard(),
		WithInputMatcher("echo", func(msg a2a.Message) bool {
			return strings.Contains(messageText(&msg), "special")
		}, Response{
			Parts: []a2a.Part{{Text: testutil.Ptr("special response")}},
		}),
		WithSkillResponse("echo", Response{
			Parts: []a2a.Part{{Text: testutil.Ptr("catch-all")}},
		}),
	)
	m.Start()
	defer m.Close()

	// First rule should match.
	resp1 := sendRPC(t, m.URL(), a2a.MethodSendMessage, sendMsg("echo", "something special"))
	task1 := decodeTask(t, resp1)
	if got := *task1.Artifacts[0].Parts[0].Text; got != "special response" {
		t.Errorf("text = %q, want %q", got, "special response")
	}

	// Second rule (catch-all) should match.
	resp2 := sendRPC(t, m.URL(), a2a.MethodSendMessage, sendMsg("echo", "normal"))
	task2 := decodeTask(t, resp2)
	if got := *task2.Artifacts[0].Parts[0].Text; got != "catch-all" {
		t.Errorf("text = %q, want %q", got, "catch-all")
	}
}

func TestMockUnknownMethod(t *testing.T) {
	m := NewA2AServer(testCard())
	m.Start()
	defer m.Close()

	resp := sendRPC(t, m.URL(), "tasks/get", map[string]string{"id": "nope"})
	if resp.Error == nil {
		t.Fatal("expected error response")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("code = %d, want -32601", resp.Error.Code)
	}
}

func TestOptionsFromConfig(t *testing.T) {
	cfg := &AgentConfig{
		Name: "config-agent",
		Card: *testCard(),
		Responses: []RuleConfig{
			{
				Skill: "echo",
				Match: &MatchConfig{Contains: "hello"},
				Response: &ResponseConfig{
					Parts: []PartConfig{{Text: "hello response"}},
				},
			},
			{
				Skill: "echo",
				Response: &ResponseConfig{
					Parts: []PartConfig{{Text: "default"}},
				},
			},
			{
				Skill: "fail",
				Error: "config error",
			},
		},
	}

	opts := OptionsFromConfig(cfg)
	m := NewA2AServer(&cfg.Card, opts...)
	m.Start()
	defer m.Close()

	// Contains-match rule.
	resp := sendRPC(t, m.URL(), a2a.MethodSendMessage, sendMsg("echo", "hello world"))
	task := decodeTask(t, resp)
	if got := *task.Artifacts[0].Parts[0].Text; got != "hello response" {
		t.Errorf("text = %q, want %q", got, "hello response")
	}

	// Default rule.
	resp2 := sendRPC(t, m.URL(), a2a.MethodSendMessage, sendMsg("echo", "other"))
	task2 := decodeTask(t, resp2)
	if got := *task2.Artifacts[0].Parts[0].Text; got != "default" {
		t.Errorf("text = %q, want %q", got, "default")
	}

	// Error rule.
	resp3 := sendRPC(t, m.URL(), a2a.MethodSendMessage, sendMsg("fail", "x"))
	task3 := decodeTask(t, resp3)
	if task3.Status.State != a2a.TaskStateFailed {
		t.Fatalf("state = %q, want failed", task3.Status.State)
	}
}

func TestMockWithClient(t *testing.T) {
	m := NewA2AServer(testCard(),
		WithSkillResponse("echo", Response{
			Parts: []a2a.Part{{Text: testutil.Ptr("client test")}},
		}),
	)
	m.Start()
	defer m.Close()

	client := a2a.NewClient(m.URL())
	ctx := context.Background()

	// Discover.
	card, err := client.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if card.Name != "test-agent" {
		t.Errorf("card.Name = %q, want %q", card.Name, "test-agent")
	}

	// SendMessage.
	task, err := client.SendMessage(ctx, sendMsg("echo", "via client"))
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", task.Status.State)
	}
	if got := *task.Artifacts[0].Parts[0].Text; got != "client test" {
		t.Errorf("text = %q, want %q", got, "client test")
	}
}

func TestMockNoMatchingRule(t *testing.T) {
	m := NewA2AServer(testCard(),
		WithSkillResponse("other-skill", Response{
			Parts: []a2a.Part{{Text: testutil.Ptr("nope")}},
		}),
	)
	m.Start()
	defer m.Close()

	resp := sendRPC(t, m.URL(), a2a.MethodSendMessage, sendMsg("echo", "hi"))
	if resp.Error == nil {
		t.Fatal("expected error response")
	}
	if resp.Error.Code != -32000 {
		t.Errorf("code = %d, want -32000", resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "no matching rule") {
		t.Errorf("message = %q, want to contain 'no matching rule'", resp.Error.Message)
	}
}
