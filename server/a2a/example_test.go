package a2aserver_test

import (
	"fmt"

	a2aserver "github.com/AltairaLabs/PromptKit/server/a2a"
)

// ExampleInMemoryTaskStore demonstrates the task lifecycle: create a task,
// transition it through states, and read it back. InMemoryTaskStore needs no
// external services, making it a good default for local development and
// tests; production deployments typically supply a persistent TaskStore.
func ExampleInMemoryTaskStore() {
	store := a2aserver.NewInMemoryTaskStore()

	task, err := store.Create("task-1", "ctx-1")
	if err != nil {
		fmt.Println("create error:", err)
		return
	}
	fmt.Println("created:", task.Status.State)

	if err := store.SetState("task-1", "working", nil); err != nil {
		fmt.Println("transition error:", err)
		return
	}

	got, err := store.Get("task-1")
	if err != nil {
		fmt.Println("get error:", err)
		return
	}
	fmt.Println("state:", got.Status.State)
	// Output:
	// created: submitted
	// state: working
}
