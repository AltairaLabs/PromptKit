package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRegistrar is a mock implementation of ToolRegistrar for testing.
type mockRegistrar struct {
	handlers map[string]func(args map[string]any) (any, error)
}

func (m *mockRegistrar) OnTool(name string, handler func(args map[string]any) (any, error)) {
	if m.handlers == nil {
		m.handlers = make(map[string]func(args map[string]any) (any, error))
	}
	m.handlers[name] = handler
}

func TestOnTyped(t *testing.T) {
	t.Run("basic struct", func(t *testing.T) {
		type Args struct {
			City    string `map:"city"`
			Country string `map:"country"`
		}

		reg := &mockRegistrar{}
		OnTyped(reg, "get_weather", func(args Args) (any, error) {
			return map[string]string{
				"location": args.City + ", " + args.Country,
			}, nil
		})

		require.Contains(t, reg.handlers, "get_weather")

		result, err := reg.handlers["get_weather"](map[string]any{
			"city":    "Tokyo",
			"country": "Japan",
		})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"location": "Tokyo, Japan"}, result)
	})

	t.Run("numeric conversion", func(t *testing.T) {
		type Args struct {
			Count int     `map:"count"`
			Price float64 `map:"price"`
		}

		reg := &mockRegistrar{}
		OnTyped(reg, "order", func(args Args) (any, error) {
			return args.Count * int(args.Price), nil
		})

		// JSON numbers come as float64
		result, err := reg.handlers["order"](map[string]any{
			"count": float64(5),
			"price": float64(10.5),
		})
		require.NoError(t, err)
		assert.Equal(t, 50, result) // 5 * 10
	})

	t.Run("missing fields use zero values", func(t *testing.T) {
		type Args struct {
			Required string `map:"required"`
			Optional string `map:"optional"`
		}

		reg := &mockRegistrar{}
		OnTyped(reg, "test", func(args Args) (any, error) {
			return args, nil
		})

		result, err := reg.handlers["test"](map[string]any{
			"required": "value",
		})
		require.NoError(t, err)
		assert.Equal(t, Args{Required: "value", Optional: ""}, result)
	})
}

func TestMapToStruct(t *testing.T) {
	t.Run("basic mapping", func(t *testing.T) {
		type Target struct {
			Name  string `map:"name"`
			Value int    `map:"value"`
		}

		m := map[string]any{
			"name":  "test",
			"value": float64(42),
		}

		var target Target
		err := mapToStruct(m, &target)
		require.NoError(t, err)
		assert.Equal(t, "test", target.Name)
		assert.Equal(t, 42, target.Value)
	})

	t.Run("nested struct", func(t *testing.T) {
		type Inner struct {
			X int `json:"x"`
			Y int `json:"y"`
		}
		type Outer struct {
			Inner Inner `map:"inner"`
		}

		m := map[string]any{
			"inner": map[string]any{
				"x": float64(1),
				"y": float64(2),
			},
		}

		var target Outer
		err := mapToStruct(m, &target)
		require.NoError(t, err)
		assert.Equal(t, 1, target.Inner.X)
		assert.Equal(t, 2, target.Inner.Y)
	})

	t.Run("non-pointer error", func(t *testing.T) {
		type Target struct{}
		err := mapToStruct(map[string]any{}, Target{})
		assert.Error(t, err)
	})
}

func TestHandlerAdapter(t *testing.T) {
	t.Run("execute handler", func(t *testing.T) {
		adapter := NewHandlerAdapter("test_tool", func(args map[string]any) (any, error) {
			return map[string]any{
				"input":  args["input"],
				"output": "processed",
			}, nil
		})

		assert.Equal(t, "test_tool", adapter.Name())

		result, err := adapter.Execute(context.Background(), &tools.ToolDescriptor{
			Name: "test_tool",
		}, json.RawMessage(`{"input": "hello"}`))

		require.NoError(t, err)

		var resultMap map[string]any
		err = json.Unmarshal(result, &resultMap)
		require.NoError(t, err)
		assert.Equal(t, "hello", resultMap["input"])
		assert.Equal(t, "processed", resultMap["output"])
	})

	t.Run("invalid JSON args", func(t *testing.T) {
		adapter := NewHandlerAdapter("test", func(args map[string]any) (any, error) {
			return nil, nil
		})

		_, err := adapter.Execute(context.Background(), &tools.ToolDescriptor{}, json.RawMessage(`invalid`))
		assert.Error(t, err)
	})

	t.Run("handler error", func(t *testing.T) {
		adapter := NewHandlerAdapter("test", func(_ map[string]any) (any, error) {
			return nil, assert.AnError
		})

		_, err := adapter.Execute(context.Background(), &tools.ToolDescriptor{}, json.RawMessage(`{}`))
		assert.Error(t, err)
	})
}

func TestMapToStructEdgeCases(t *testing.T) {
	t.Run("nil pointer error", func(t *testing.T) {
		var target *struct{}
		err := mapToStruct(map[string]any{}, target)
		assert.Error(t, err)
	})

	t.Run("non-struct pointer error", func(t *testing.T) {
		var target string
		err := mapToStruct(map[string]any{}, &target)
		assert.Error(t, err)
	})

	t.Run("uses field name when no tag", func(t *testing.T) {
		type Target struct {
			Name string // no tag, uses "Name" as key
		}
		m := map[string]any{"Name": "test"}
		var target Target
		err := mapToStruct(m, &target)
		require.NoError(t, err)
		assert.Equal(t, "test", target.Name)
	})

	t.Run("uint conversion", func(t *testing.T) {
		type Target struct {
			Count uint `map:"count"`
		}
		m := map[string]any{"count": float64(42)}
		var target Target
		err := mapToStruct(m, &target)
		require.NoError(t, err)
		assert.Equal(t, uint(42), target.Count)
	})

	t.Run("float32 conversion", func(t *testing.T) {
		type Target struct {
			Value float32 `map:"value"`
		}
		m := map[string]any{"value": float64(3.14)}
		var target Target
		err := mapToStruct(m, &target)
		require.NoError(t, err)
		assert.InDelta(t, float32(3.14), target.Value, 0.001)
	})

	t.Run("nil value skipped", func(t *testing.T) {
		type Target struct {
			Name string `map:"name"`
		}
		m := map[string]any{"name": nil}
		target := Target{Name: "original"}
		err := mapToStruct(m, &target)
		require.NoError(t, err)
		assert.Equal(t, "original", target.Name) // unchanged
	})

	t.Run("slice conversion via JSON", func(t *testing.T) {
		type Target struct {
			Items []string `map:"items"`
		}
		m := map[string]any{"items": []any{"a", "b", "c"}}
		var target Target
		err := mapToStruct(m, &target)
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c"}, target.Items)
	})
}
