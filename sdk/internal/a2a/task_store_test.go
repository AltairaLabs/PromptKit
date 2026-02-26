package a2a

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	rta2a "github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func taskStorePtr(s string) *string { return &s }

func TestInMemoryTaskStore_Create(t *testing.T) {
	store := NewInMemoryTaskStore()

	task, err := store.Create("task-1", "ctx-1")
	require.NoError(t, err)

	assert.Equal(t, "task-1", task.ID)
	assert.Equal(t, "ctx-1", task.ContextID)
	assert.Equal(t, rta2a.TaskStateSubmitted, task.Status.State)
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
	assert.Equal(t, rta2a.TaskStateSubmitted, task.Status.State)
}

func TestInMemoryTaskStore_ValidTransitions(t *testing.T) {
	tests := []struct {
		name string
		from rta2a.TaskState
		to   rta2a.TaskState
	}{
		{"submitted→working", rta2a.TaskStateSubmitted, rta2a.TaskStateWorking},
		{"working→completed", rta2a.TaskStateWorking, rta2a.TaskStateCompleted},
		{"working→failed", rta2a.TaskStateWorking, rta2a.TaskStateFailed},
		{"working→canceled", rta2a.TaskStateWorking, rta2a.TaskStateCanceled},
		{"working→input_required", rta2a.TaskStateWorking, rta2a.TaskStateInputRequired},
		{"working→auth_required", rta2a.TaskStateWorking, rta2a.TaskStateAuthRequired},
		{"working→rejected", rta2a.TaskStateWorking, rta2a.TaskStateRejected},
		{"input_required→working", rta2a.TaskStateInputRequired, rta2a.TaskStateWorking},
		{"input_required→canceled", rta2a.TaskStateInputRequired, rta2a.TaskStateCanceled},
		{"auth_required→working", rta2a.TaskStateAuthRequired, rta2a.TaskStateWorking},
		{"auth_required→canceled", rta2a.TaskStateAuthRequired, rta2a.TaskStateCanceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewInMemoryTaskStore()
			task, err := store.Create("t", "c")
			require.NoError(t, err)

			// Walk to the "from" state via valid transitions.
			switch tt.from {
			case rta2a.TaskStateSubmitted:
				// Already there.
			case rta2a.TaskStateWorking:
				require.NoError(t, store.SetState("t", rta2a.TaskStateWorking, nil))
			case rta2a.TaskStateInputRequired:
				require.NoError(t, store.SetState("t", rta2a.TaskStateWorking, nil))
				require.NoError(t, store.SetState("t", rta2a.TaskStateInputRequired, nil))
			case rta2a.TaskStateAuthRequired:
				require.NoError(t, store.SetState("t", rta2a.TaskStateWorking, nil))
				require.NoError(t, store.SetState("t", rta2a.TaskStateAuthRequired, nil))
			}

			err = store.SetState("t", tt.to, nil)
			assert.NoError(t, err)

			task, err = store.Get("t")
			require.NoError(t, err)
			assert.Equal(t, tt.to, task.Status.State)
		})
	}
}

func TestInMemoryTaskStore_InvalidTransitions(t *testing.T) {
	tests := []struct {
		name string
		from rta2a.TaskState
		to   rta2a.TaskState
	}{
		{"submitted→completed", rta2a.TaskStateSubmitted, rta2a.TaskStateCompleted},
		{"submitted→failed", rta2a.TaskStateSubmitted, rta2a.TaskStateFailed},
		{"submitted→input_required", rta2a.TaskStateSubmitted, rta2a.TaskStateInputRequired},
		{"working→submitted", rta2a.TaskStateWorking, rta2a.TaskStateSubmitted},
		{"input_required→completed", rta2a.TaskStateInputRequired, rta2a.TaskStateCompleted},
		{"auth_required→completed", rta2a.TaskStateAuthRequired, rta2a.TaskStateCompleted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewInMemoryTaskStore()
			_, err := store.Create("t", "c")
			require.NoError(t, err)

			// Walk to the "from" state.
			switch tt.from {
			case rta2a.TaskStateSubmitted:
				// Already there.
			case rta2a.TaskStateWorking:
				require.NoError(t, store.SetState("t", rta2a.TaskStateWorking, nil))
			case rta2a.TaskStateInputRequired:
				require.NoError(t, store.SetState("t", rta2a.TaskStateWorking, nil))
				require.NoError(t, store.SetState("t", rta2a.TaskStateInputRequired, nil))
			case rta2a.TaskStateAuthRequired:
				require.NoError(t, store.SetState("t", rta2a.TaskStateWorking, nil))
				require.NoError(t, store.SetState("t", rta2a.TaskStateAuthRequired, nil))
			}

			err = store.SetState("t", tt.to, nil)
			assert.Error(t, err)
			assert.True(t, errors.Is(err, ErrInvalidTransition))
		})
	}
}

func TestInMemoryTaskStore_TerminalStateTransitions(t *testing.T) {
	terminals := []rta2a.TaskState{
		rta2a.TaskStateCompleted,
		rta2a.TaskStateFailed,
		rta2a.TaskStateCanceled,
		rta2a.TaskStateRejected,
	}

	for _, terminal := range terminals {
		t.Run(string(terminal), func(t *testing.T) {
			store := NewInMemoryTaskStore()
			_, err := store.Create("t", "c")
			require.NoError(t, err)
			require.NoError(t, store.SetState("t", rta2a.TaskStateWorking, nil))
			require.NoError(t, store.SetState("t", terminal, nil))

			err = store.SetState("t", rta2a.TaskStateWorking, nil)
			assert.Error(t, err)
			assert.True(t, errors.Is(err, ErrTaskTerminal))
		})
	}
}

func TestInMemoryTaskStore_SetStateWithMessage(t *testing.T) {
	store := NewInMemoryTaskStore()
	_, err := store.Create("t", "c")
	require.NoError(t, err)

	msg := &rta2a.Message{
		MessageID: "status-1",
		Role:      rta2a.RoleAgent,
		Parts:     []rta2a.Part{{Text: taskStorePtr("Working on it")}},
	}

	require.NoError(t, store.SetState("t", rta2a.TaskStateWorking, msg))

	task, err := store.Get("t")
	require.NoError(t, err)
	require.NotNil(t, task.Status.Message)
	assert.Equal(t, "status-1", task.Status.Message.MessageID)
	assert.Equal(t, "Working on it", *task.Status.Message.Parts[0].Text)
}

func TestInMemoryTaskStore_SetStateNotFound(t *testing.T) {
	store := NewInMemoryTaskStore()
	err := store.SetState("nonexistent", rta2a.TaskStateWorking, nil)
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

func TestInMemoryTaskStore_Cancel(t *testing.T) {
	cancellable := []struct {
		name string
		from rta2a.TaskState
	}{
		{"from submitted", rta2a.TaskStateSubmitted},
		{"from working", rta2a.TaskStateWorking},
		{"from input_required", rta2a.TaskStateInputRequired},
		{"from auth_required", rta2a.TaskStateAuthRequired},
	}

	for _, tt := range cancellable {
		t.Run(tt.name, func(t *testing.T) {
			store := NewInMemoryTaskStore()
			_, err := store.Create("t", "c")
			require.NoError(t, err)

			switch tt.from {
			case rta2a.TaskStateSubmitted:
				// Already there.
			case rta2a.TaskStateWorking:
				require.NoError(t, store.SetState("t", rta2a.TaskStateWorking, nil))
			case rta2a.TaskStateInputRequired:
				require.NoError(t, store.SetState("t", rta2a.TaskStateWorking, nil))
				require.NoError(t, store.SetState("t", rta2a.TaskStateInputRequired, nil))
			case rta2a.TaskStateAuthRequired:
				require.NoError(t, store.SetState("t", rta2a.TaskStateWorking, nil))
				require.NoError(t, store.SetState("t", rta2a.TaskStateAuthRequired, nil))
			}

			err = store.Cancel("t")
			assert.NoError(t, err)

			task, err := store.Get("t")
			require.NoError(t, err)
			assert.Equal(t, rta2a.TaskStateCanceled, task.Status.State)
		})
	}
}

func TestInMemoryTaskStore_CancelTerminal(t *testing.T) {
	terminals := []rta2a.TaskState{rta2a.TaskStateCompleted, rta2a.TaskStateFailed}

	for _, terminal := range terminals {
		t.Run(string(terminal), func(t *testing.T) {
			store := NewInMemoryTaskStore()
			_, err := store.Create("t", "c")
			require.NoError(t, err)
			require.NoError(t, store.SetState("t", rta2a.TaskStateWorking, nil))
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

	err = store.AddArtifacts("t", []rta2a.Artifact{
		{ArtifactID: "a1", Parts: []rta2a.Part{{Text: taskStorePtr("first")}}},
	})
	require.NoError(t, err)

	err = store.AddArtifacts("t", []rta2a.Artifact{
		{ArtifactID: "a2", Parts: []rta2a.Part{{Text: taskStorePtr("second")}}},
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
	err := store.AddArtifacts("nonexistent", []rta2a.Artifact{{ArtifactID: "a1"}})
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

func TestInMemoryTaskStore_List(t *testing.T) {
	store := NewInMemoryTaskStore()

	// Create tasks in two contexts.
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

	// Concurrent creates.
	for i := range n {
		wg.Go(func() {
			_, _ = store.Create(fmt.Sprintf("t%d", i), "ctx")
		})
	}
	wg.Wait()

	// Verify all tasks were created.
	tasks, err := store.List("ctx", 0, 0)
	require.NoError(t, err)
	assert.Len(t, tasks, n)

	// Concurrent state updates.
	for i := range n {
		wg.Go(func() {
			_ = store.SetState(fmt.Sprintf("t%d", i), rta2a.TaskStateWorking, nil)
		})
	}
	wg.Wait()

	// Verify all tasks are in working state.
	for i := range n {
		task, err := store.Get(fmt.Sprintf("t%d", i))
		require.NoError(t, err)
		assert.Equal(t, rta2a.TaskStateWorking, task.Status.State)
	}
}

func TestInMemoryTaskStore_EvictTerminal(t *testing.T) {
	store := NewInMemoryTaskStore()

	// Create a completed task with an old timestamp.
	_, err := store.Create("old-completed", "ctx")
	require.NoError(t, err)
	require.NoError(t, store.SetState("old-completed", rta2a.TaskStateWorking, nil))
	require.NoError(t, store.SetState("old-completed", rta2a.TaskStateCompleted, nil))

	// Backdate the timestamp.
	task, _ := store.Get("old-completed")
	old := time.Now().Add(-2 * time.Hour)
	task.Status.Timestamp = &old

	// Create a recent completed task.
	_, err = store.Create("new-completed", "ctx")
	require.NoError(t, err)
	require.NoError(t, store.SetState("new-completed", rta2a.TaskStateWorking, nil))
	require.NoError(t, store.SetState("new-completed", rta2a.TaskStateCompleted, nil))

	// Create a working (non-terminal) task with old timestamp.
	_, err = store.Create("old-working", "ctx")
	require.NoError(t, err)
	require.NoError(t, store.SetState("old-working", rta2a.TaskStateWorking, nil))
	workingTask, _ := store.Get("old-working")
	workingTask.Status.Timestamp = &old

	// Create a failed task with an old timestamp.
	_, err = store.Create("old-failed", "ctx")
	require.NoError(t, err)
	require.NoError(t, store.SetState("old-failed", rta2a.TaskStateWorking, nil))
	require.NoError(t, store.SetState("old-failed", rta2a.TaskStateFailed, nil))
	failedTask, _ := store.Get("old-failed")
	failedTask.Status.Timestamp = &old

	// Evict tasks older than 1 hour.
	cutoff := time.Now().Add(-1 * time.Hour)
	evicted := store.EvictTerminal(cutoff)

	// old-completed and old-failed should be evicted.
	assert.Len(t, evicted, 2)
	assert.Contains(t, evicted, "old-completed")
	assert.Contains(t, evicted, "old-failed")

	// Verify they are gone.
	_, err = store.Get("old-completed")
	assert.ErrorIs(t, err, ErrTaskNotFound)
	_, err = store.Get("old-failed")
	assert.ErrorIs(t, err, ErrTaskNotFound)

	// new-completed and old-working should remain.
	_, err = store.Get("new-completed")
	assert.NoError(t, err)
	_, err = store.Get("old-working")
	assert.NoError(t, err)
}

func TestInMemoryTaskStore_EvictTerminal_Empty(t *testing.T) {
	store := NewInMemoryTaskStore()
	evicted := store.EvictTerminal(time.Now())
	assert.Empty(t, evicted)
}

func TestInMemoryTaskStore_EvictTerminal_AllTerminalStates(t *testing.T) {
	terminals := []rta2a.TaskState{
		rta2a.TaskStateCompleted,
		rta2a.TaskStateFailed,
		rta2a.TaskStateCanceled,
		rta2a.TaskStateRejected,
	}

	for _, terminal := range terminals {
		t.Run(string(terminal), func(t *testing.T) {
			store := NewInMemoryTaskStore()
			_, err := store.Create("t", "c")
			require.NoError(t, err)
			require.NoError(t, store.SetState("t", rta2a.TaskStateWorking, nil))
			require.NoError(t, store.SetState("t", terminal, nil))

			// Backdate.
			task, _ := store.Get("t")
			old := time.Now().Add(-2 * time.Hour)
			task.Status.Timestamp = &old

			evicted := store.EvictTerminal(time.Now().Add(-1 * time.Hour))
			assert.Equal(t, []string{"t"}, evicted)

			_, err = store.Get("t")
			assert.ErrorIs(t, err, ErrTaskNotFound)
		})
	}
}
