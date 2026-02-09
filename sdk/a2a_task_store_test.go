package sdk

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func taskStorePtr(s string) *string { return &s }

func TestInMemoryA2ATaskStore_Create(t *testing.T) {
	store := NewInMemoryA2ATaskStore()

	task, err := store.Create("task-1", "ctx-1")
	require.NoError(t, err)

	assert.Equal(t, "task-1", task.ID)
	assert.Equal(t, "ctx-1", task.ContextID)
	assert.Equal(t, a2a.TaskStateSubmitted, task.Status.State)
	require.NotNil(t, task.Status.Timestamp)
	assert.False(t, task.Status.Timestamp.IsZero())
}

func TestInMemoryA2ATaskStore_CreateDuplicate(t *testing.T) {
	store := NewInMemoryA2ATaskStore()

	_, err := store.Create("task-1", "ctx-1")
	require.NoError(t, err)

	_, err = store.Create("task-1", "ctx-1")
	assert.ErrorIs(t, err, ErrTaskAlreadyExists)
}

func TestInMemoryA2ATaskStore_GetNotFound(t *testing.T) {
	store := NewInMemoryA2ATaskStore()

	_, err := store.Get("nonexistent")
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

func TestInMemoryA2ATaskStore_Get(t *testing.T) {
	store := NewInMemoryA2ATaskStore()
	_, err := store.Create("task-1", "ctx-1")
	require.NoError(t, err)

	task, err := store.Get("task-1")
	require.NoError(t, err)
	assert.Equal(t, "task-1", task.ID)
	assert.Equal(t, a2a.TaskStateSubmitted, task.Status.State)
}

func TestInMemoryA2ATaskStore_ValidTransitions(t *testing.T) {
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
			store := NewInMemoryA2ATaskStore()
			task, err := store.Create("t", "c")
			require.NoError(t, err)

			// Walk to the "from" state via valid transitions.
			switch tt.from {
			case a2a.TaskStateSubmitted:
				// Already there.
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

			task, err = store.Get("t")
			require.NoError(t, err)
			assert.Equal(t, tt.to, task.Status.State)
		})
	}
}

func TestInMemoryA2ATaskStore_InvalidTransitions(t *testing.T) {
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
			store := NewInMemoryA2ATaskStore()
			_, err := store.Create("t", "c")
			require.NoError(t, err)

			// Walk to the "from" state.
			switch tt.from {
			case a2a.TaskStateSubmitted:
				// Already there.
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

func TestInMemoryA2ATaskStore_TerminalStateTransitions(t *testing.T) {
	terminals := []a2a.TaskState{
		a2a.TaskStateCompleted,
		a2a.TaskStateFailed,
		a2a.TaskStateCanceled,
		a2a.TaskStateRejected,
	}

	for _, terminal := range terminals {
		t.Run(string(terminal), func(t *testing.T) {
			store := NewInMemoryA2ATaskStore()
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

func TestInMemoryA2ATaskStore_SetStateWithMessage(t *testing.T) {
	store := NewInMemoryA2ATaskStore()
	_, err := store.Create("t", "c")
	require.NoError(t, err)

	msg := &a2a.Message{
		MessageID: "status-1",
		Role:      a2a.RoleAgent,
		Parts:     []a2a.Part{{Text: taskStorePtr("Working on it")}},
	}

	require.NoError(t, store.SetState("t", a2a.TaskStateWorking, msg))

	task, err := store.Get("t")
	require.NoError(t, err)
	require.NotNil(t, task.Status.Message)
	assert.Equal(t, "status-1", task.Status.Message.MessageID)
	assert.Equal(t, "Working on it", *task.Status.Message.Parts[0].Text)
}

func TestInMemoryA2ATaskStore_SetStateNotFound(t *testing.T) {
	store := NewInMemoryA2ATaskStore()
	err := store.SetState("nonexistent", a2a.TaskStateWorking, nil)
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

func TestInMemoryA2ATaskStore_Cancel(t *testing.T) {
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
			store := NewInMemoryA2ATaskStore()
			_, err := store.Create("t", "c")
			require.NoError(t, err)

			switch tt.from {
			case a2a.TaskStateSubmitted:
				// Already there.
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

func TestInMemoryA2ATaskStore_CancelTerminal(t *testing.T) {
	terminals := []a2a.TaskState{a2a.TaskStateCompleted, a2a.TaskStateFailed}

	for _, terminal := range terminals {
		t.Run(string(terminal), func(t *testing.T) {
			store := NewInMemoryA2ATaskStore()
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

func TestInMemoryA2ATaskStore_CancelNotFound(t *testing.T) {
	store := NewInMemoryA2ATaskStore()
	err := store.Cancel("nonexistent")
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

func TestInMemoryA2ATaskStore_AddArtifacts(t *testing.T) {
	store := NewInMemoryA2ATaskStore()
	_, err := store.Create("t", "c")
	require.NoError(t, err)

	err = store.AddArtifacts("t", []a2a.Artifact{
		{ArtifactID: "a1", Parts: []a2a.Part{{Text: taskStorePtr("first")}}},
	})
	require.NoError(t, err)

	err = store.AddArtifacts("t", []a2a.Artifact{
		{ArtifactID: "a2", Parts: []a2a.Part{{Text: taskStorePtr("second")}}},
	})
	require.NoError(t, err)

	task, err := store.Get("t")
	require.NoError(t, err)
	require.Len(t, task.Artifacts, 2)
	assert.Equal(t, "a1", task.Artifacts[0].ArtifactID)
	assert.Equal(t, "a2", task.Artifacts[1].ArtifactID)
}

func TestInMemoryA2ATaskStore_AddArtifactsNotFound(t *testing.T) {
	store := NewInMemoryA2ATaskStore()
	err := store.AddArtifacts("nonexistent", []a2a.Artifact{{ArtifactID: "a1"}})
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

func TestInMemoryA2ATaskStore_List(t *testing.T) {
	store := NewInMemoryA2ATaskStore()

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

func TestInMemoryA2ATaskStore_Concurrent(t *testing.T) {
	store := NewInMemoryA2ATaskStore()
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
			_ = store.SetState(fmt.Sprintf("t%d", i), a2a.TaskStateWorking, nil)
		})
	}
	wg.Wait()

	// Verify all tasks are in working state.
	for i := range n {
		task, err := store.Get(fmt.Sprintf("t%d", i))
		require.NoError(t, err)
		assert.Equal(t, a2a.TaskStateWorking, task.Status.State)
	}
}
