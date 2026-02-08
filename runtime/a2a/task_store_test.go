package a2a

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemoryTaskStore_Create(t *testing.T) {
	store := NewInMemoryTaskStore()

	task, err := store.Create("task-1", "ctx-1")
	require.NoError(t, err)

	assert.Equal(t, "task-1", task.ID)
	assert.Equal(t, "ctx-1", task.ContextID)
	assert.Equal(t, TaskStateSubmitted, task.Status.State)
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
	assert.Equal(t, TaskStateSubmitted, task.Status.State)
}

func TestInMemoryTaskStore_ValidTransitions(t *testing.T) {
	tests := []struct {
		name string
		from TaskState
		to   TaskState
	}{
		{"submitted→working", TaskStateSubmitted, TaskStateWorking},
		{"working→completed", TaskStateWorking, TaskStateCompleted},
		{"working→failed", TaskStateWorking, TaskStateFailed},
		{"working→canceled", TaskStateWorking, TaskStateCanceled},
		{"working→input_required", TaskStateWorking, TaskStateInputRequired},
		{"working→auth_required", TaskStateWorking, TaskStateAuthRequired},
		{"working→rejected", TaskStateWorking, TaskStateRejected},
		{"input_required→working", TaskStateInputRequired, TaskStateWorking},
		{"input_required→canceled", TaskStateInputRequired, TaskStateCanceled},
		{"auth_required→working", TaskStateAuthRequired, TaskStateWorking},
		{"auth_required→canceled", TaskStateAuthRequired, TaskStateCanceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewInMemoryTaskStore()
			task, err := store.Create("t", "c")
			require.NoError(t, err)

			// Walk to the "from" state via valid transitions.
			switch tt.from {
			case TaskStateSubmitted:
				// Already there.
			case TaskStateWorking:
				require.NoError(t, store.SetState("t", TaskStateWorking, nil))
			case TaskStateInputRequired:
				require.NoError(t, store.SetState("t", TaskStateWorking, nil))
				require.NoError(t, store.SetState("t", TaskStateInputRequired, nil))
			case TaskStateAuthRequired:
				require.NoError(t, store.SetState("t", TaskStateWorking, nil))
				require.NoError(t, store.SetState("t", TaskStateAuthRequired, nil))
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
		from TaskState
		to   TaskState
	}{
		{"submitted→completed", TaskStateSubmitted, TaskStateCompleted},
		{"submitted→failed", TaskStateSubmitted, TaskStateFailed},
		{"submitted→input_required", TaskStateSubmitted, TaskStateInputRequired},
		{"working→submitted", TaskStateWorking, TaskStateSubmitted},
		{"input_required→completed", TaskStateInputRequired, TaskStateCompleted},
		{"auth_required→completed", TaskStateAuthRequired, TaskStateCompleted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewInMemoryTaskStore()
			_, err := store.Create("t", "c")
			require.NoError(t, err)

			// Walk to the "from" state.
			switch tt.from {
			case TaskStateSubmitted:
				// Already there.
			case TaskStateWorking:
				require.NoError(t, store.SetState("t", TaskStateWorking, nil))
			case TaskStateInputRequired:
				require.NoError(t, store.SetState("t", TaskStateWorking, nil))
				require.NoError(t, store.SetState("t", TaskStateInputRequired, nil))
			case TaskStateAuthRequired:
				require.NoError(t, store.SetState("t", TaskStateWorking, nil))
				require.NoError(t, store.SetState("t", TaskStateAuthRequired, nil))
			}

			err = store.SetState("t", tt.to, nil)
			assert.Error(t, err)
			assert.True(t, errors.Is(err, ErrInvalidTransition))
		})
	}
}

func TestInMemoryTaskStore_TerminalStateTransitions(t *testing.T) {
	terminals := []TaskState{
		TaskStateCompleted,
		TaskStateFailed,
		TaskStateCanceled,
		TaskStateRejected,
	}

	for _, terminal := range terminals {
		t.Run(string(terminal), func(t *testing.T) {
			store := NewInMemoryTaskStore()
			_, err := store.Create("t", "c")
			require.NoError(t, err)
			require.NoError(t, store.SetState("t", TaskStateWorking, nil))
			require.NoError(t, store.SetState("t", terminal, nil))

			err = store.SetState("t", TaskStateWorking, nil)
			assert.Error(t, err)
			assert.True(t, errors.Is(err, ErrTaskTerminal))
		})
	}
}

func TestInMemoryTaskStore_SetStateWithMessage(t *testing.T) {
	store := NewInMemoryTaskStore()
	_, err := store.Create("t", "c")
	require.NoError(t, err)

	msg := &Message{
		MessageID: "status-1",
		Role:      RoleAgent,
		Parts:     []Part{{Text: ptr("Working on it")}},
	}

	require.NoError(t, store.SetState("t", TaskStateWorking, msg))

	task, err := store.Get("t")
	require.NoError(t, err)
	require.NotNil(t, task.Status.Message)
	assert.Equal(t, "status-1", task.Status.Message.MessageID)
	assert.Equal(t, "Working on it", *task.Status.Message.Parts[0].Text)
}

func TestInMemoryTaskStore_SetStateNotFound(t *testing.T) {
	store := NewInMemoryTaskStore()
	err := store.SetState("nonexistent", TaskStateWorking, nil)
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

func TestInMemoryTaskStore_Cancel(t *testing.T) {
	cancellable := []struct {
		name string
		from TaskState
	}{
		{"from submitted", TaskStateSubmitted},
		{"from working", TaskStateWorking},
		{"from input_required", TaskStateInputRequired},
		{"from auth_required", TaskStateAuthRequired},
	}

	for _, tt := range cancellable {
		t.Run(tt.name, func(t *testing.T) {
			store := NewInMemoryTaskStore()
			_, err := store.Create("t", "c")
			require.NoError(t, err)

			switch tt.from {
			case TaskStateSubmitted:
				// Already there.
			case TaskStateWorking:
				require.NoError(t, store.SetState("t", TaskStateWorking, nil))
			case TaskStateInputRequired:
				require.NoError(t, store.SetState("t", TaskStateWorking, nil))
				require.NoError(t, store.SetState("t", TaskStateInputRequired, nil))
			case TaskStateAuthRequired:
				require.NoError(t, store.SetState("t", TaskStateWorking, nil))
				require.NoError(t, store.SetState("t", TaskStateAuthRequired, nil))
			}

			err = store.Cancel("t")
			assert.NoError(t, err)

			task, err := store.Get("t")
			require.NoError(t, err)
			assert.Equal(t, TaskStateCanceled, task.Status.State)
		})
	}
}

func TestInMemoryTaskStore_CancelTerminal(t *testing.T) {
	terminals := []TaskState{TaskStateCompleted, TaskStateFailed}

	for _, terminal := range terminals {
		t.Run(string(terminal), func(t *testing.T) {
			store := NewInMemoryTaskStore()
			_, err := store.Create("t", "c")
			require.NoError(t, err)
			require.NoError(t, store.SetState("t", TaskStateWorking, nil))
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

	err = store.AddArtifacts("t", []Artifact{
		{ArtifactID: "a1", Parts: []Part{{Text: ptr("first")}}},
	})
	require.NoError(t, err)

	err = store.AddArtifacts("t", []Artifact{
		{ArtifactID: "a2", Parts: []Part{{Text: ptr("second")}}},
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
	err := store.AddArtifacts("nonexistent", []Artifact{{ArtifactID: "a1"}})
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
			_ = store.SetState(fmt.Sprintf("t%d", i), TaskStateWorking, nil)
		})
	}
	wg.Wait()

	// Verify all tasks are in working state.
	for i := range n {
		task, err := store.Get(fmt.Sprintf("t%d", i))
		require.NoError(t, err)
		assert.Equal(t, TaskStateWorking, task.Status.State)
	}
}
