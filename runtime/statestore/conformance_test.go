package statestore_test

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore/file"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// fullStore is every interface the three shipped stores implement. The SDK
// type-asserts these, so a case that behaves differently on one store is a
// behavior change for whoever swaps stores.
type fullStore interface {
	statestore.Store
	statestore.BulkWriter
	statestore.MessageReader
	statestore.MessageAppender
	statestore.MetadataAccessor
	statestore.SummaryAccessor
	statestore.ListAccessor
	statestore.MessageLog
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, opts statestore.ListOptions) ([]string, error)
}

// conformanceStores builds a fresh instance of every shipped store.
func conformanceStores(t *testing.T) map[string]fullStore {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	fs, err := file.NewStore(file.Options{Root: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = fs.Close() })

	mem := statestore.NewMemoryStore()
	t.Cleanup(mem.Close)

	return map[string]fullStore{
		"memory": mem,
		"redis":  statestore.NewRedisStore(client),
		"file":   fs,
	}
}

// eachStore runs fn against every shipped store as its own subtest.
func eachStore(t *testing.T, fn func(t *testing.T, s fullStore)) {
	t.Helper()
	for name, s := range conformanceStores(t) {
		t.Run(name, func(t *testing.T) { fn(t, s) })
	}
}

func msg(role, content string) types.Message { return types.Message{Role: role, Content: content} }

// texts reduces messages to "role:content" so stores that stamp timestamps or
// other bookkeeping still compare equal on what the caller wrote.
func texts(msgs []types.Message) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Role+":"+m.Content)
	}
	return out
}

func TestStoreConformance_LoadErrors(t *testing.T) {
	eachStore(t, func(t *testing.T, s fullStore) {
		ctx := context.Background()
		_, err := s.Load(ctx, "missing")
		assert.ErrorIs(t, err, statestore.ErrNotFound)
		_, err = s.Load(ctx, "")
		assert.ErrorIs(t, err, statestore.ErrInvalidID)
	})
}

func TestStoreConformance_SaveLoadRoundTrip(t *testing.T) {
	eachStore(t, func(t *testing.T, s fullStore) {
		ctx := context.Background()
		require.NoError(t, s.Save(ctx, &statestore.ConversationState{
			ID:           "conv",
			UserID:       "user-1",
			SystemPrompt: "be brief",
			Messages:     []types.Message{msg("user", "hi"), msg("assistant", "hello")},
			Metadata:     map[string]interface{}{"k": "v"},
			Summaries:    []statestore.Summary{{StartTurn: 0, EndTurn: 1, Content: "greeting"}},
			TokenCount:   42,
		}))

		got, err := s.Load(ctx, "conv")
		require.NoError(t, err)
		assert.Equal(t, "conv", got.ID)
		assert.Equal(t, "user-1", got.UserID)
		assert.Equal(t, "be brief", got.SystemPrompt)
		assert.Equal(t, []string{"user:hi", "assistant:hello"}, texts(got.Messages))
		assert.Equal(t, map[string]interface{}{"k": "v"}, got.Metadata)
		require.Len(t, got.Summaries, 1)
		assert.Equal(t, "greeting", got.Summaries[0].Content)
		assert.Equal(t, 1, got.Summaries[0].EndTurn)
		assert.Equal(t, 42, got.TokenCount)
	})
}

func TestStoreConformance_SaveOverwrites(t *testing.T) {
	eachStore(t, func(t *testing.T, s fullStore) {
		ctx := context.Background()
		require.NoError(t, s.Save(ctx, &statestore.ConversationState{
			ID: "conv", Messages: []types.Message{msg("user", "a"), msg("user", "b")},
			Metadata: map[string]interface{}{"old": "x"},
		}))
		require.NoError(t, s.Save(ctx, &statestore.ConversationState{
			ID: "conv", Messages: []types.Message{msg("user", "c")},
			Metadata: map[string]interface{}{"new": "y"},
		}))
		got, err := s.Load(ctx, "conv")
		require.NoError(t, err)
		assert.Equal(t, []string{"user:c"}, texts(got.Messages))
		assert.Equal(t, map[string]interface{}{"new": "y"}, got.Metadata)
		n, err := s.MessageCount(ctx, "conv")
		require.NoError(t, err)
		assert.Equal(t, 1, n)
	})
}

func TestStoreConformance_Fork(t *testing.T) {
	eachStore(t, func(t *testing.T, s fullStore) {
		ctx := context.Background()
		assert.ErrorIs(t, s.Fork(ctx, "missing", "copy"), statestore.ErrNotFound)

		require.NoError(t, s.Save(ctx, &statestore.ConversationState{
			ID: "src", Messages: []types.Message{msg("user", "hi")},
			Metadata: map[string]interface{}{"k": "v"},
		}))
		require.NoError(t, s.Fork(ctx, "src", "copy"))
		require.NoError(t, s.AppendMessages(ctx, "copy", []types.Message{msg("assistant", "forked")}))

		copied, err := s.Load(ctx, "copy")
		require.NoError(t, err)
		assert.Equal(t, "copy", copied.ID)
		assert.Equal(t, []string{"user:hi", "assistant:forked"}, texts(copied.Messages))
		assert.Equal(t, map[string]interface{}{"k": "v"}, copied.Metadata)

		src, err := s.Load(ctx, "src")
		require.NoError(t, err)
		assert.Equal(t, []string{"user:hi"}, texts(src.Messages), "fork must leave the source unchanged")
	})
}

func TestStoreConformance_Messages(t *testing.T) {
	eachStore(t, func(t *testing.T, s fullStore) {
		ctx := context.Background()
		_, err := s.LoadRecentMessages(ctx, "missing", 2)
		assert.ErrorIs(t, err, statestore.ErrNotFound)
		_, err = s.MessageCount(ctx, "missing")
		assert.ErrorIs(t, err, statestore.ErrNotFound)

		// AppendMessages auto-creates the conversation.
		require.NoError(t, s.AppendMessages(ctx, "conv", []types.Message{msg("user", "1"), msg("assistant", "2")}))
		require.NoError(t, s.AppendMessages(ctx, "conv", []types.Message{msg("user", "3")}))

		n, err := s.MessageCount(ctx, "conv")
		require.NoError(t, err)
		assert.Equal(t, 3, n)

		recent, err := s.LoadRecentMessages(ctx, "conv", 2)
		require.NoError(t, err)
		assert.Equal(t, []string{"assistant:2", "user:3"}, texts(recent))

		all, err := s.LoadRecentMessages(ctx, "conv", 10)
		require.NoError(t, err)
		assert.Equal(t, []string{"user:1", "assistant:2", "user:3"}, texts(all))
	})
}

func TestStoreConformance_Metadata(t *testing.T) {
	eachStore(t, func(t *testing.T, s fullStore) {
		ctx := context.Background()
		_, err := s.LoadMetadata(ctx, "missing")
		assert.ErrorIs(t, err, statestore.ErrNotFound)

		// MergeMetadata auto-creates, overwrites existing keys, keeps others,
		// and leaves messages alone.
		require.NoError(t, s.MergeMetadata(ctx, "conv", map[string]interface{}{"a": "1", "b": "2"}))
		require.NoError(t, s.AppendMessages(ctx, "conv", []types.Message{msg("user", "hi")}))
		require.NoError(t, s.MergeMetadata(ctx, "conv", map[string]interface{}{"b": "3", "c": "4"}))

		md, err := s.LoadMetadata(ctx, "conv")
		require.NoError(t, err)
		assert.Equal(t, map[string]interface{}{"a": "1", "b": "3", "c": "4"}, md)

		md["a"] = "mutated"
		again, err := s.LoadMetadata(ctx, "conv")
		require.NoError(t, err)
		assert.Equal(t, "1", again["a"], "LoadMetadata must return a copy")

		got, err := s.Load(ctx, "conv")
		require.NoError(t, err)
		assert.Equal(t, []string{"user:hi"}, texts(got.Messages))
	})
}

func TestStoreConformance_Summaries(t *testing.T) {
	eachStore(t, func(t *testing.T, s fullStore) {
		ctx := context.Background()
		require.NoError(t, s.AppendMessages(ctx, "conv", []types.Message{msg("user", "hi")}))
		none, err := s.LoadSummaries(ctx, "conv")
		require.NoError(t, err)
		assert.Nil(t, none, "no summaries is nil, not an error")

		require.NoError(t, s.SaveSummary(ctx, "conv", statestore.Summary{Content: "first", EndTurn: 1}))
		require.NoError(t, s.SaveSummary(ctx, "conv", statestore.Summary{Content: "second", EndTurn: 2}))
		// SaveSummary auto-creates.
		require.NoError(t, s.SaveSummary(ctx, "fresh", statestore.Summary{Content: "only"}))

		got, err := s.LoadSummaries(ctx, "conv")
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, []string{"first", "second"}, []string{got[0].Content, got[1].Content})
		assert.Equal(t, 2, got[1].EndTurn)

		fresh, err := s.LoadSummaries(ctx, "fresh")
		require.NoError(t, err)
		require.Len(t, fresh, 1)
		assert.Equal(t, "only", fresh[0].Content)
	})
}

func TestStoreConformance_Lists(t *testing.T) {
	eachStore(t, func(t *testing.T, s fullStore) {
		ctx := context.Background()
		_, err := s.LoadList(ctx, "missing", "l")
		assert.ErrorIs(t, err, statestore.ErrNotFound)
		_, err = s.ListLen(ctx, "missing", "l")
		assert.ErrorIs(t, err, statestore.ErrNotFound)

		require.NoError(t, s.AppendList(ctx, "conv", "history", [][]byte{[]byte(`"a"`), []byte(`"b"`)}))
		require.NoError(t, s.AppendList(ctx, "conv", "history", [][]byte{[]byte(`"c"`)}))

		items, err := s.LoadList(ctx, "conv", "history")
		require.NoError(t, err)
		assert.Equal(t, [][]byte{[]byte(`"a"`), []byte(`"b"`), []byte(`"c"`)}, items)
		n, err := s.ListLen(ctx, "conv", "history")
		require.NoError(t, err)
		assert.Equal(t, 3, n)

		unwritten, err := s.LoadList(ctx, "conv", "other")
		require.NoError(t, err)
		assert.Nil(t, unwritten, "an unwritten list on an existing conversation is nil, not an error")
		n, err = s.ListLen(ctx, "conv", "other")
		require.NoError(t, err)
		assert.Zero(t, n)
	})
}

func TestStoreConformance_MessageLog(t *testing.T) {
	eachStore(t, func(t *testing.T, s fullStore) {
		ctx := context.Background()
		none, err := s.LogLoad(ctx, "missing", 0)
		require.NoError(t, err)
		assert.Empty(t, none)
		n, err := s.LogLen(ctx, "missing")
		require.NoError(t, err)
		assert.Zero(t, n)

		total, err := s.LogAppend(ctx, "conv", 0, []types.Message{msg("user", "1"), msg("assistant", "2")})
		require.NoError(t, err)
		assert.Equal(t, 2, total)
		// A retry from seq 1 skips the message already persisted.
		total, err = s.LogAppend(ctx, "conv", 1, []types.Message{msg("assistant", "2"), msg("user", "3")})
		require.NoError(t, err)
		assert.Equal(t, 3, total)

		all, err := s.LogLoad(ctx, "conv", 0)
		require.NoError(t, err)
		assert.Equal(t, []string{"user:1", "assistant:2", "user:3"}, texts(all))
		recent, err := s.LogLoad(ctx, "conv", 1)
		require.NoError(t, err)
		assert.Equal(t, []string{"user:3"}, texts(recent))
	})
}

func TestStoreConformance_DeleteAndList(t *testing.T) {
	eachStore(t, func(t *testing.T, s fullStore) {
		ctx := context.Background()
		for _, id := range []string{"a", "b", "c"} {
			require.NoError(t, s.Save(ctx, &statestore.ConversationState{ID: id, UserID: "u1"}))
		}
		require.NoError(t, s.Save(ctx, &statestore.ConversationState{ID: "other", UserID: "u2"}))

		ids, err := s.List(ctx, statestore.ListOptions{UserID: "u1"})
		require.NoError(t, err)
		sort.Strings(ids)
		assert.Equal(t, []string{"a", "b", "c"}, ids)

		require.NoError(t, s.Delete(ctx, "b"))
		_, err = s.Load(ctx, "b")
		assert.ErrorIs(t, err, statestore.ErrNotFound)

		ids, err = s.List(ctx, statestore.ListOptions{UserID: "u1"})
		require.NoError(t, err)
		sort.Strings(ids)
		assert.Equal(t, []string{"a", "c"}, ids)
	})
}

func TestStoreConformance_DeleteMissing(t *testing.T) {
	eachStore(t, func(t *testing.T, s fullStore) {
		assert.ErrorIs(t, s.Delete(context.Background(), "missing"), statestore.ErrNotFound)
	})
}

// TestStoreConformance_TypedWritesAutoCreate pins the documented contract
// that each typed write auto-creates the conversation: afterwards it loads,
// and lists can be read from it.
func TestStoreConformance_TypedWritesAutoCreate(t *testing.T) {
	writes := map[string]func(ctx context.Context, s fullStore, id string) error{
		"AppendMessages": func(ctx context.Context, s fullStore, id string) error {
			return s.AppendMessages(ctx, id, []types.Message{msg("user", "hi")})
		},
		"MergeMetadata": func(ctx context.Context, s fullStore, id string) error {
			return s.MergeMetadata(ctx, id, map[string]interface{}{"k": "v"})
		},
		"SaveSummary": func(ctx context.Context, s fullStore, id string) error {
			return s.SaveSummary(ctx, id, statestore.Summary{Content: "sum"})
		},
		"AppendList": func(ctx context.Context, s fullStore, id string) error {
			return s.AppendList(ctx, id, "history", [][]byte{[]byte(`"x"`)})
		},
	}
	eachStore(t, func(t *testing.T, s fullStore) {
		for name, write := range writes {
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				id := "auto-" + name
				require.NoError(t, write(ctx, s, id))

				got, err := s.Load(ctx, id)
				require.NoError(t, err)
				assert.Equal(t, id, got.ID)
				items, err := s.LoadList(ctx, id, "unwritten")
				require.NoError(t, err)
				assert.Nil(t, items)
				n, err := s.ListLen(ctx, id, "unwritten")
				require.NoError(t, err)
				assert.Zero(t, n)
			})
		}
	})
}

var conformanceTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// fillValue sets every exported, serialized field of v to a non-zero value,
// so a store that drops or mangles a field shows up as a difference.
func fillValue(v reflect.Value, depth int) {
	if depth > 6 {
		return
	}
	switch v.Kind() {
	case reflect.Ptr:
		v.Set(reflect.New(v.Type().Elem()))
		fillValue(v.Elem(), depth+1)
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			v.Set(reflect.ValueOf(conformanceTime))
			return
		}
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if f.IsExported() && f.Tag.Get("json") != "-" {
				fillValue(v.Field(i), depth+1)
			}
		}
	case reflect.String:
		v.SetString("s")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(7)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(7)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1.5)
	case reflect.Slice:
		if v.Type() == reflect.TypeOf(json.RawMessage{}) {
			v.Set(reflect.ValueOf(json.RawMessage(`{"k":"v"}`)))
			return
		}
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fillValue(s.Index(0), depth+1)
		v.Set(s)
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		key := reflect.New(v.Type().Key()).Elem()
		fillValue(key, depth+1)
		val := reflect.New(v.Type().Elem()).Elem()
		fillValue(val, depth+1)
		m.SetMapIndex(key, val)
		v.Set(m)
	case reflect.Interface:
		if v.NumMethod() == 0 {
			v.Set(reflect.ValueOf("v"))
		}
	}
}

// TestStoreConformance_MessagesKeepEveryField pins that a message comes back
// from every store, through each write path, with every field it was given.
func TestStoreConformance_MessagesKeepEveryField(t *testing.T) {
	var full types.Message
	fillValue(reflect.ValueOf(&full).Elem(), 0)
	// A tool-result message serializes its content once, inside ToolResult,
	// so it is its own case rather than a field of the filled message.
	full.ToolResult = nil
	toolMsg := types.Message{
		Role: "tool", Content: "72F and sunny", Timestamp: conformanceTime,
		ToolResult: &types.MessageToolResult{
			ID: "call-1", Name: "weather", LatencyMs: 12,
			Parts: []types.ContentPart{types.NewTextPart("72F and sunny")},
		},
	}
	want := []types.Message{full, toolMsg}

	eachStore(t, func(t *testing.T, s fullStore) {
		ctx := context.Background()
		require.NoError(t, s.Save(ctx, &statestore.ConversationState{ID: "saved", Messages: want}))
		got, err := s.Load(ctx, "saved")
		require.NoError(t, err)
		assert.Equal(t, want, got.Messages, "Save then Load")

		require.NoError(t, s.AppendMessages(ctx, "appended", want))
		recent, err := s.LoadRecentMessages(ctx, "appended", 2)
		require.NoError(t, err)
		assert.Equal(t, want, recent, "AppendMessages then LoadRecentMessages")
	})
}
