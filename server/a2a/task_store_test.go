package a2aserver

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func taskStoreTextPtr(s string) *string { return &s }

func TestInMemoryTaskStore_Create(t *testing.T) {
	store := NewInMemoryTaskStore()

	task, err := store.Create("task-1", "ctx-1")
	require.NoError(t, err)

	assert.Equal(t, "task-1", task.ID)
	assert.Equal(t, "ctx-1", task.ContextID)
	assert.Equal(t, a2a.TaskStateSubmitted, task.Status.State)
	require.NotNil(t, task.Status.Timestamp)
	assert.False(t, task.Status.Timestamp.IsZero())
}

func TestInMemoryTaskStore_CreateDuplicate(t *testing.T) {
	store := NewInMemoryTaskStore()

	_, err := store.Create("task-1", "ctx-1")
	require.NoError(t, err)

	_, err = store.Create("task-1", "ctx-1")
	assert.ErrorIs(t, err, ErrTaskAlreadyExists)
}

func TestInMemoryTaskStore_GetNotFound(t *testing.T) {
	store := NewInMemoryTaskStore()

	_, err := store.Get("nonexistent")
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

func TestInMemoryTaskStore_Get(t *testing.T) {
	store := NewInMemoryTaskStore()
	_, err := store.Create("task-1", "ctx-1")
	require.NoError(t, err)

	task, err := store.Get("task-1")
	require.NoError(t, err)
	assert.Equal(t, "task-1", task.ID)
	assert.Equal(t, a2a.TaskStateSubmitted, task.Status.State)
}

func TestInMemoryTaskStore_ValidTransitions(t *testing.T) {
	tests := []struct {
		name string
		from a2a.TaskState
		to   a2a.TaskState
	}{
		{"submitted→working", a2a.TaskStateSubmitted, a2a.TaskStateWorking},
		{"working→completed", a2a.TaskStateWorking, a2a.TaskStateCompleted},
		{"working→failed", a2a.TaskStateWorking, a2a.TaskStateFailed},
		{"working→canceled", a2a.TaskStateWorking, a2a.TaskStateCanceled},
		{"working→input_required", a2a.TaskStateWorking, a2a.TaskStateInputRequired},
		{"working→auth_required", a2a.TaskStateWorking, a2a.TaskStateAuthRequired},
		{"working→rejected", a2a.TaskStateWorking, a2a.TaskStateRejected},
		{"input_required→working", a2a.TaskStateInputRequired, a2a.TaskStateWorking},
		{"input_required→canceled", a2a.TaskStateInputRequired, a2a.TaskStateCanceled},
		{"auth_required→working", a2a.TaskStateAuthRequired, a2a.TaskStateWorking},
		{"auth_required→canceled", a2a.TaskStateAuthRequired, a2a.TaskStateCanceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewInMemoryTaskStore()
			_, err := store.Create("t", "c")
			require.NoError(t, err)

			switch tt.from {
			case a2a.TaskStateSubmitted:
			case a2a.TaskStateWorking:
				require.NoError(t, store.SetState("t", a2a.TaskStateWorking, nil))
			case a2a.TaskStateInputRequired:
				require.NoError(t, store.SetState("t", a2a.TaskStateWorking, nil))
				require.NoError(t, store.SetState("t", a2a.TaskStateInputRequired, nil))
			case a2a.TaskStateAuthRequired:
				require.NoError(t, store.SetState("t", a2a.TaskStateWorking, nil))
				require.NoError(t, store.SetState("t", a2a.TaskStateAuthRequired, nil))
			}

			err = store.SetState("t", tt.to, nil)
			assert.NoError(t, err)

			task, err := store.Get("t")
			require.NoError(t, err)
			assert.Equal(t, tt.to, task.Status.State)
		})
	}
}

func TestInMemoryTaskStore_InvalidTransitions(t *testing.T) {
	tests := []struct {
		name string
		from a2a.TaskState
		to   a2a.TaskState
	}{
		{"submitted→completed", a2a.TaskStateSubmitted, a2a.TaskStateCompleted},
		{"submitted→failed", a2a.TaskStateSubmitted, a2a.TaskStateFailed},
		{"submitted→input_required", a2a.TaskStateSubmitted, a2a.TaskStateInputRequired},
		{"working→submitted", a2a.TaskStateWorking, a2a.TaskStateSubmitted},
		{"input_required→completed", a2a.TaskStateInputRequired, a2a.TaskStateCompleted},
		{"auth_required→completed", a2a.TaskStateAuthRequired, a2a.TaskStateCompleted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewInMemoryTaskStore()
			_, err := store.Create("t", "c")
			require.NoError(t, err)

			switch tt.from {
			case a2a.TaskStateSubmitted:
			case a2a.TaskStateWorking:
				require.NoError(t, store.SetState("t", a2a.TaskStateWorking, nil))
			case a2a.TaskStateInputRequired:
				require.NoError(t, store.SetState("t", a2a.TaskStateWorking, nil))
				require.NoError(t, store.SetState("t", a2a.TaskStateInputRequired, nil))
			case a2a.TaskStateAuthRequired:
				require.NoError(t, store.SetState("t", a2a.TaskStateWorking, nil))
				require.NoError(t, store.SetState("t", a2a.TaskStateAuthRequired, nil))
			}

			err = store.SetState("t", tt.to, nil)
			assert.Error(t, err)
			assert.True(t, errors.Is(err, ErrInvalidTransition))
		})
	}
}

func TestInMemoryTaskStore_TerminalStateTransitions(t *testing.T) {
	terminals := []a2a.TaskState{
		a2a.TaskStateCompleted,
		a2a.TaskStateFailed,
		a2a.TaskStateCanceled,
		a2a.TaskStateRejected,
	}

	for _, terminal := range terminals {
		t.Run(string(terminal), func(t *testing.T) {
			store := NewInMemoryTaskStore()
			_, err := store.Create("t", "c")
			require.NoError(t, err)
			require.NoError(t, store.SetState("t", a2a.TaskStateWorking, nil))
			require.NoError(t, store.SetState("t", terminal, nil))

			err = store.SetState("t", a2a.TaskStateWorking, nil)
			assert.Error(t, err)
			assert.True(t, errors.Is(err, ErrTaskTerminal))
		})
	}
}

func TestInMemoryTaskStore_SetStateWithMessage(t *testing.T) {
	store := NewInMemoryTaskStore()
	_, err := store.Create("t", "c")
	require.NoError(t, err)

	msg := &a2a.Message{
		MessageID: "status-1",
		Role:      a2a.RoleAgent,
		Parts:     []a2a.Part{{Text: taskStoreTextPtr("Working on it")}},
	}

	require.NoError(t, store.SetState("t", a2a.TaskStateWorking, msg))

	task, err := store.Get("t")
	require.NoError(t, err)
	require.NotNil(t, task.Status.Message)
	assert.Equal(t, "status-1", task.Status.Message.MessageID)
	assert.Equal(t, "Working on it", *task.Status.Message.Parts[0].Text)
}

func TestInMemoryTaskStore_SetStateNotFound(t *testing.T) {
	store := NewInMemoryTaskStore()
	err := store.SetState("nonexistent", a2a.TaskStateWorking, nil)
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

func TestInMemoryTaskStore_Cancel(t *testing.T) {
	cancellable := []struct {
		name string
		from a2a.TaskState
	}{
		{"from submitted", a2a.TaskStateSubmitted},
		{"from working", a2a.TaskStateWorking},
		{"from input_required", a2a.TaskStateInputRequired},
		{"from auth_required", a2a.TaskStateAuthRequired},
	}

	for _, tt := range cancellable {
		t.Run(tt.name, func(t *testing.T) {
			store := NewInMemoryTaskStore()
			_, err := store.Create("t", "c")
			require.NoError(t, err)

			switch tt.from {
			case a2a.TaskStateSubmitted:
			case a2a.TaskStateWorking:
				require.NoError(t, store.SetState("t", a2a.TaskStateWorking, nil))
			case a2a.TaskStateInputRequired:
				require.NoError(t, store.SetState("t", a2a.TaskStateWorking, nil))
				require.NoError(t, store.SetState("t", a2a.TaskStateInputRequired, nil))
			case a2a.TaskStateAuthRequired:
				require.NoError(t, store.SetState("t", a2a.TaskStateWorking, nil))
				require.NoError(t, store.SetState("t", a2a.TaskStateAuthRequired, nil))
			}

			err = store.Cancel("t")
			assert.NoError(t, err)

			task, err := store.Get("t")
			require.NoError(t, err)
			assert.Equal(t, a2a.TaskStateCanceled, task.Status.State)
		})
	}
}

func TestInMemoryTaskStore_CancelTerminal(t *testing.T) {
	terminals := []a2a.TaskState{a2a.TaskStateCompleted, a2a.TaskStateFailed}

	for _, terminal := range terminals {
		t.Run(string(terminal), func(t *testing.T) {
			store := NewInMemoryTaskStore()
			_, err := store.Create("t", "c")
			require.NoError(t, err)
			require.NoError(t, store.SetState("t", a2a.TaskStateWorking, nil))
			require.NoError(t, store.SetState("t", terminal, nil))

			err = store.Cancel("t")
			assert.Error(t, err)
			assert.True(t, errors.Is(err, ErrTaskTerminal))
		})
	}
}

func TestInMemoryTaskStore_CancelNotFound(t *testing.T) {
	store := NewInMemoryTaskStore()
	err := store.Cancel("nonexistent")
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

func TestInMemoryTaskStore_AddArtifacts(t *testing.T) {
	store := NewInMemoryTaskStore()
	_, err := store.Create("t", "c")
	require.NoError(t, err)

	err = store.AddArtifacts("t", []a2a.Artifact{
		{ArtifactID: "a1", Parts: []a2a.Part{{Text: taskStoreTextPtr("first")}}},
	})
	require.NoError(t, err)

	err = store.AddArtifacts("t", []a2a.Artifact{
		{ArtifactID: "a2", Parts: []a2a.Part{{Text: taskStoreTextPtr("second")}}},
	})
	require.NoError(t, err)

	task, err := store.Get("t")
	require.NoError(t, err)
	require.Len(t, task.Artifacts, 2)
	assert.Equal(t, "a1", task.Artifacts[0].ArtifactID)
	assert.Equal(t, "a2", task.Artifacts[1].ArtifactID)
}

func TestInMemoryTaskStore_AddArtifactsNotFound(t *testing.T) {
	store := NewInMemoryTaskStore()
	err := store.AddArtifacts("nonexistent", []a2a.Artifact{{ArtifactID: "a1"}})
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

func TestInMemoryTaskStore_List(t *testing.T) {
	store := NewInMemoryTaskStore()

	for i := range 5 {
		_, err := store.Create(fmt.Sprintf("t%d", i), "ctx-1")
		require.NoError(t, err)
	}
	for i := range 8 {
		if i < 5 {
			continue
		}
		_, err := store.Create(fmt.Sprintf("t%d", i), "ctx-2")
		require.NoError(t, err)
	}

	t.Run("filter by context", func(t *testing.T) {
		tasks, err := store.List("ctx-1", 0, 0)
		require.NoError(t, err)
		assert.Len(t, tasks, 5)
		for _, task := range tasks {
			assert.Equal(t, "ctx-1", task.ContextID)
		}
	})

	t.Run("all tasks", func(t *testing.T) {
		tasks, err := store.List("", 0, 0)
		require.NoError(t, err)
		assert.Len(t, tasks, 8)
	})

	t.Run("with limit", func(t *testing.T) {
		tasks, err := store.List("ctx-1", 2, 0)
		require.NoError(t, err)
		assert.Len(t, tasks, 2)
	})

	t.Run("with offset", func(t *testing.T) {
		tasks, err := store.List("ctx-1", 0, 3)
		require.NoError(t, err)
		assert.Len(t, tasks, 2)
	})

	t.Run("offset beyond range", func(t *testing.T) {
		tasks, err := store.List("ctx-1", 0, 100)
		require.NoError(t, err)
		assert.Nil(t, tasks)
	})
}

func TestInMemoryTaskStore_Concurrent(t *testing.T) {
	store := NewInMemoryTaskStore()
	var wg sync.WaitGroup
	n := 100

	for i := range n {
		wg.Go(func() {
			_, _ = store.Create(fmt.Sprintf("t%d", i), "ctx")
		})
	}
	wg.Wait()

	tasks, err := store.List("ctx", 0, 0)
	require.NoError(t, err)
	assert.Len(t, tasks, n)

	for i := range n {
		wg.Go(func() {
			_ = store.SetState(fmt.Sprintf("t%d", i), a2a.TaskStateWorking, nil)
		})
	}
	wg.Wait()

	for i := range n {
		task, err := store.Get(fmt.Sprintf("t%d", i))
		require.NoError(t, err)
		assert.Equal(t, a2a.TaskStateWorking, task.Status.State)
	}
}

// setTaskTimestamp directly sets the timestamp on the live task in the store.
// This is needed because Get() returns deep copies.
func setTaskTimestamp(store *InMemoryTaskStore, taskID string, ts time.Time) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if task, ok := store.tasks[taskID]; ok {
		task.Status.Timestamp = &ts
	}
}

func TestInMemoryTaskStore_EvictTerminal(t *testing.T) {
	store := NewInMemoryTaskStore()

	_, err := store.Create("old-completed", "ctx")
	require.NoError(t, err)
	require.NoError(t, store.SetState("old-completed", a2a.TaskStateWorking, nil))
	require.NoError(t, store.SetState("old-completed", a2a.TaskStateCompleted, nil))

	old := time.Now().Add(-2 * time.Hour)
	setTaskTimestamp(store, "old-completed", old)

	_, err = store.Create("new-completed", "ctx")
	require.NoError(t, err)
	require.NoError(t, store.SetState("new-completed", a2a.TaskStateWorking, nil))
	require.NoError(t, store.SetState("new-completed", a2a.TaskStateCompleted, nil))

	_, err = store.Create("old-working", "ctx")
	require.NoError(t, err)
	require.NoError(t, store.SetState("old-working", a2a.TaskStateWorking, nil))
	setTaskTimestamp(store, "old-working", old)

	_, err = store.Create("old-failed", "ctx")
	require.NoError(t, err)
	require.NoError(t, store.SetState("old-failed", a2a.TaskStateWorking, nil))
	require.NoError(t, store.SetState("old-failed", a2a.TaskStateFailed, nil))
	setTaskTimestamp(store, "old-failed", old)

	cutoff := time.Now().Add(-1 * time.Hour)
	evicted := store.EvictTerminal(cutoff)

	assert.Len(t, evicted, 2)
	assert.Contains(t, evicted, "old-completed")
	assert.Contains(t, evicted, "old-failed")

	_, err = store.Get("old-completed")
	assert.ErrorIs(t, err, ErrTaskNotFound)
	_, err = store.Get("old-failed")
	assert.ErrorIs(t, err, ErrTaskNotFound)

	_, err = store.Get("new-completed")
	assert.NoError(t, err)
	_, err = store.Get("old-working")
	assert.NoError(t, err)
}

func TestInMemoryTaskStore_GetReturnsDeepCopy(t *testing.T) {
	store := NewInMemoryTaskStore()
	_, err := store.Create("t", "c")
	require.NoError(t, err)

	msg := &a2a.Message{
		Role:  a2a.RoleAgent,
		Parts: []a2a.Part{{Text: taskStoreTextPtr("hello")}},
	}
	require.NoError(t, store.SetState("t", a2a.TaskStateWorking, msg))

	task1, err := store.Get("t")
	require.NoError(t, err)
	task2, err := store.Get("t")
	require.NoError(t, err)

	// Mutating the returned copy must not affect the stored task.
	*task1.Status.Message.Parts[0].Text = "mutated"
	assert.Equal(t, "hello", *task2.Status.Message.Parts[0].Text,
		"Get must return independent deep copies")

	// Verify the pointers are different.
	assert.NotSame(t, task1, task2)
	assert.NotSame(t, task1.Status.Timestamp, task2.Status.Timestamp)
}

func TestInMemoryTaskStore_ListReturnsDeepCopies(t *testing.T) {
	store := NewInMemoryTaskStore()
	_, err := store.Create("t1", "ctx")
	require.NoError(t, err)

	tasks, err := store.List("ctx", 0, 0)
	require.NoError(t, err)
	require.Len(t, tasks, 1)

	// Mutating the returned task must not affect the store.
	tasks[0].ContextID = "mutated"
	task, err := store.Get("t1")
	require.NoError(t, err)
	assert.Equal(t, "ctx", task.ContextID)
}

func TestInMemoryTaskStore_ListDeterministicOrder(t *testing.T) {
	store := NewInMemoryTaskStore()
	base := time.Now().UTC()
	// Status time decides the order (most recent first); ties fall back to ID.
	stamps := map[string]time.Duration{"c": 3, "a": 1, "b": 1, "e": 5, "d": 4}
	for id, offset := range stamps {
		_, err := store.Create(id, "ctx")
		require.NoError(t, err)
		setTaskTimestamp(store, id, base.Add(offset*time.Second))
	}

	tasks, err := store.List("ctx", 0, 0)
	require.NoError(t, err)
	ids := make([]string, len(tasks))
	for i, task := range tasks {
		ids[i] = task.ID
	}
	assert.Equal(t, []string{"e", "d", "c", "a", "b"}, ids)
}

func TestInMemoryTaskStore_Query(t *testing.T) {
	store := NewInMemoryTaskStore()
	base := time.Now().UTC()
	for i, id := range []string{"t1", "t2", "t3", "t4"} {
		_, err := store.Create(id, "ctx")
		require.NoError(t, err)
		setTaskTimestamp(store, id, base.Add(time.Duration(i)*time.Second))
	}
	_, err := store.Create("other", "ctx-2")
	require.NoError(t, err)
	require.NoError(t, store.SetState("t2", a2a.TaskStateWorking, nil))
	setTaskTimestamp(store, "t2", base.Add(10*time.Second))

	page, err := store.Query(TaskQuery{ContextID: "ctx", Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, 4, page.Total)
	require.Len(t, page.Tasks, 2)

	working := a2a.TaskStateWorking
	page, err = store.Query(TaskQuery{ContextID: "ctx", Status: &working})
	require.NoError(t, err)
	require.Len(t, page.Tasks, 1)
	assert.Equal(t, "t2", page.Tasks[0].ID)

	after := base.Add(1500 * time.Millisecond)
	page, err = store.Query(TaskQuery{ContextID: "ctx", StatusAfter: &after})
	require.NoError(t, err)
	ids := []string{}
	for _, task := range page.Tasks {
		ids = append(ids, task.ID)
	}
	assert.ElementsMatch(t, []string{"t2", "t3", "t4"}, ids, "t2 was just updated")

	page, err = store.Query(TaskQuery{ContextID: "ctx", Offset: 10})
	require.NoError(t, err)
	assert.Empty(t, page.Tasks)
	assert.Equal(t, 4, page.Total)
}

// listOnlyStore hides the in-memory store's Query, so the server has to page
// through List itself.
type listOnlyStore struct{ TaskStore }

func TestQueryTasks_FallsBackToList(t *testing.T) {
	inner := NewInMemoryTaskStore()
	base := time.Now().UTC()
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("t%d", i)
		_, err := inner.Create(id, "ctx")
		require.NoError(t, err)
		setTaskTimestamp(inner, id, base.Add(time.Duration(i)*time.Second))
	}
	page, err := queryTasks(listOnlyStore{inner}, TaskQuery{ContextID: "ctx", Limit: 2, Offset: 1})
	require.NoError(t, err)
	assert.Equal(t, 3, page.Total)
	require.Len(t, page.Tasks, 2)
	assert.Equal(t, "t1", page.Tasks[0].ID)
	assert.Equal(t, "t0", page.Tasks[1].ID)
}

func TestInMemoryTaskStore_EvictTerminal_Empty(t *testing.T) {
	store := NewInMemoryTaskStore()
	evicted := store.EvictTerminal(time.Now())
	assert.Empty(t, evicted)
}

func TestInMemoryTaskStore_EvictTerminal_AllTerminalStates(t *testing.T) {
	terminals := []a2a.TaskState{
		a2a.TaskStateCompleted,
		a2a.TaskStateFailed,
		a2a.TaskStateCanceled,
		a2a.TaskStateRejected,
	}

	for _, terminal := range terminals {
		t.Run(string(terminal), func(t *testing.T) {
			store := NewInMemoryTaskStore()
			_, err := store.Create("t", "c")
			require.NoError(t, err)
			require.NoError(t, store.SetState("t", a2a.TaskStateWorking, nil))
			require.NoError(t, store.SetState("t", terminal, nil))

			old := time.Now().Add(-2 * time.Hour)
			setTaskTimestamp(store, "t", old)

			evicted := store.EvictTerminal(time.Now().Add(-1 * time.Hour))
			assert.Equal(t, []string{"t"}, evicted)

			_, err = store.Get("t")
			assert.ErrorIs(t, err, ErrTaskNotFound)
		})
	}
}
