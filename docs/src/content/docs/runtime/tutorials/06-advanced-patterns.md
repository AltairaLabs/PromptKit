---
title: 'Tutorial 6: Advanced patterns'
description: Add response caching, a custom pipeline stage with a spend budget, buffered streaming and bounded parallel requests to a runtime application.
sidebar:
  order: 6
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - runtime/pipeline/stage/pipeline.go
    - runtime/pipeline/stage/stage.go
    - runtime/pipeline/stage/stages_core.go
    - runtime/pipeline/stage/stages_provider.go
    - runtime/providers/base_provider.go
    - runtime/providers/provider.go
    - runtime/providers/streaming.go
    - runtime/types/message.go
---
## What we will build

In this tutorial we build four small building blocks, each in its own file of one `package main`: a response cache, a spend budget, a stream buffer and a parallel batch runner. A `main.go` runs all four.

## Prerequisites

- Completed [Tutorial 5](/runtime/tutorials/05-production-deployment/)
- A Go module with the runtime installed: `go get github.com/AltairaLabs/PromptKit/runtime/v2@latest`
- `OPENAI_API_KEY` set in your environment

## Step 1: Write a helper

Every step below sends single prompts through a pipeline. We create `ask.go` with `ask`, which builds a one-stage pipeline and returns the assistant reply. `runPipeline` feeds one user message into any pipeline and collects the reply.

```go
package main

import (
	"context"
	"errors"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// ask sends one prompt through a single-stage pipeline and returns the reply.
func ask(ctx context.Context, p providers.Provider, prompt string) (*types.Message, error) {
	pipe, err := stage.NewPipelineBuilder().
		Chain(stage.NewProviderStage(p, nil, nil, &stage.ProviderConfig{MaxTokens: 500})).
		Build()
	if err != nil {
		return nil, err
	}
	defer pipe.Shutdown(context.Background())

	return runPipeline(ctx, pipe, prompt)
}

// runPipeline feeds one user message to pipe and returns the assistant reply.
func runPipeline(ctx context.Context, pipe *stage.StreamPipeline, prompt string) (*types.Message, error) {
	input := make(chan stage.StreamElement, 1)
	input <- stage.NewMessageElement(&types.Message{Role: "user", Content: prompt})
	close(input)

	output, err := pipe.Execute(ctx, input)
	if err != nil {
		return nil, err
	}

	var reply *types.Message
	for elem := range output {
		if elem.Error != nil {
			err = elem.Error
		}
		if elem.Message != nil && elem.Message.Role == "assistant" && !elem.Meta.FromHistory {
			reply = elem.Message
		}
	}
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, errors.New("pipeline produced no reply")
	}
	return reply, nil
}
```

## Step 2: Cache responses

We cache replies to repeated prompts so we do not pay for them twice. Create `cache.go`:

```go
package main

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

type cacheEntry struct {
	reply   *types.Message
	created time.Time
}

type ResponseCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	ttl     time.Duration
	done    chan struct{}
}

func NewResponseCache(ttl time.Duration) *ResponseCache {
	c := &ResponseCache{
		entries: make(map[string]cacheEntry),
		ttl:     ttl,
		done:    make(chan struct{}),
	}
	go c.cleanup()
	return c
}

// Close stops the background cleanup.
func (c *ResponseCache) Close() { close(c.done) }

func key(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return fmt.Sprintf("%x", sum[:16])
}

func (c *ResponseCache) Get(prompt string) (*types.Message, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	e, ok := c.entries[key(prompt)]
	if !ok || time.Since(e.created) > c.ttl {
		return nil, false
	}
	return e.reply, true
}

func (c *ResponseCache) Set(prompt string, reply *types.Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key(prompt)] = cacheEntry{reply: reply, created: time.Now()}
}

func (c *ResponseCache) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.mu.Lock()
			for k, e := range c.entries {
				if time.Since(e.created) > c.ttl {
					delete(c.entries, k)
				}
			}
			c.mu.Unlock()
		}
	}
}
```

Step 6 calls `Get` before each request and `Set` after each reply.

## Step 3: Add a custom stage

A pipeline is a chain of stages, and a stage is a function over channels of `StreamElement`. `NewMapStage` wraps a function that transforms one element at a time. We create `budget.go` with a stage that adds up the cost of every reply and a check that refuses new requests once the budget is spent:

```go
package main

import (
	"context"
	"errors"
	"log"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

var ErrBudgetExceeded = errors.New("budget exceeded")

// Budget tracks spend in USD across requests.
type Budget struct {
	mu    sync.Mutex
	limit float64
	spent float64
}

func NewBudget(limit float64) *Budget { return &Budget{limit: limit} }

func (b *Budget) Exceeded() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.spent >= b.limit
}

func (b *Budget) Add(cost float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.spent += cost
}

// NewCostMeter returns a stage that adds each reply's cost to the budget.
func NewCostMeter(b *Budget) stage.Stage {
	return stage.NewMapStage("cost_meter", func(elem stage.StreamElement) (stage.StreamElement, error) {
		m := elem.Message
		if m != nil && m.Role == "assistant" && !elem.Meta.FromHistory && m.CostInfo != nil {
			b.Add(m.CostInfo.TotalCost)
			log.Printf("reply cost $%.6f", m.CostInfo.TotalCost)
		}
		return elem, nil
	})
}

func askWithBudget(ctx context.Context, p providers.Provider, b *Budget, prompt string) (*types.Message, error) {
	if b.Exceeded() {
		return nil, ErrBudgetExceeded
	}

	pipe, err := stage.NewPipelineBuilder().
		Chain(
			stage.NewProviderStage(p, nil, nil, &stage.ProviderConfig{MaxTokens: 500}),
			NewCostMeter(b),
		).
		Build()
	if err != nil {
		return nil, err
	}
	defer pipe.Shutdown(context.Background())

	return runPipeline(ctx, pipe, prompt)
}
```

`NewCostMeter` sits after the provider stage, where the assistant message carries its `CostInfo`. `elem.Meta.FromHistory` is true for messages replayed from a state store, so the stage skips them.

## Step 4: Buffer streamed output

Redrawing a terminal or browser on every chunk is wasteful. We create `stream.go` with a buffer that batches chunks and flushes at most once per 100 milliseconds:

```go
package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// StreamBuffer batches chunks so the UI redraws at most once per minDelay.
type StreamBuffer struct {
	chunks    []string
	lastFlush time.Time
	minDelay  time.Duration
}

func NewStreamBuffer(minDelay time.Duration) *StreamBuffer {
	return &StreamBuffer{lastFlush: time.Now(), minDelay: minDelay}
}

func (sb *StreamBuffer) Add(chunk string) string {
	sb.chunks = append(sb.chunks, chunk)
	if time.Since(sb.lastFlush) >= sb.minDelay {
		return sb.Flush()
	}
	return ""
}

func (sb *StreamBuffer) Flush() string {
	if len(sb.chunks) == 0 {
		return ""
	}
	out := strings.Join(sb.chunks, "")
	sb.chunks = sb.chunks[:0]
	sb.lastFlush = time.Now()
	return out
}

func streamWithBuffer(ctx context.Context, p providers.Provider, prompt string) error {
	chunks, err := p.PredictStream(ctx, providers.PredictionRequest{
		Messages:  []types.Message{{Role: "user", Content: prompt}},
		MaxTokens: 500,
	})
	if err != nil {
		return err
	}

	buffer := NewStreamBuffer(100 * time.Millisecond)
	for chunk := range chunks {
		if chunk.Error != nil {
			return chunk.Error
		}
		if out := buffer.Add(chunk.Delta); out != "" {
			fmt.Print(out)
		}
	}
	fmt.Print(buffer.Flush())
	return nil
}
```

`PredictStream` returns a channel of `StreamChunk` values. `Delta` holds the new text in each chunk, and `Error` is set when the stream fails.

## Step 5: Process prompts in parallel

We process prompts concurrently with a cap on parallel requests. Create `batch.go`:

```go
package main

import (
	"context"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

type BatchResult struct {
	Prompt string
	Reply  *types.Message
	Err    error
}

func processBatch(ctx context.Context, p providers.Provider, prompts []string, maxConcurrent int) []BatchResult {
	results := make([]BatchResult, len(prompts))
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	for i, prompt := range prompts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			reply, err := ask(ctx, p, prompt)
			results[i] = BatchResult{Prompt: prompt, Reply: reply, Err: err}
		}()
	}

	wg.Wait()
	return results
}
```

`processBatch` limits parallel requests with a semaphore channel, and each result lands at the index of its prompt.

## Step 6: Combine the pieces

Create `main.go`. It answers repeated prompts from the cache, sends new ones through the budgeted pipeline, streams one reply through the buffer and then runs a parallel batch:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
)

func main() {
	provider := openai.NewProvider("openai", "gpt-4o-mini", "",
		providers.ProviderDefaults{Temperature: 0.7, MaxTokens: 2000}, false)
	defer provider.Close()

	cache := NewResponseCache(10 * time.Minute)
	defer cache.Close()
	budget := NewBudget(1.00) // USD

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	prompts := []string{"What is AI?", "What is machine learning?", "What is AI?"}
	for _, prompt := range prompts {
		if cached, ok := cache.Get(prompt); ok {
			fmt.Printf("[cache] %s\n%s\n\n", prompt, cached.GetContent())
			continue
		}

		reply, err := askWithBudget(ctx, provider, budget, prompt)
		if err != nil {
			log.Printf("%s: %v", prompt, err)
			continue
		}
		cache.Set(prompt, reply)
		fmt.Printf("%s\n%s\n\n", prompt, reply.GetContent())
	}

	fmt.Println("[stream]")
	if err := streamWithBuffer(ctx, provider, "Name three uses of caching."); err != nil {
		log.Fatal(err)
	}
	fmt.Println()

	fmt.Println("[batch]")
	greetings := []string{"Hello", "Hi there", "Good morning"}
	for _, r := range processBatch(ctx, provider, greetings, 3) {
		if r.Err != nil {
			fmt.Printf("%s: %v\n", r.Prompt, r.Err)
			continue
		}
		fmt.Printf("%s: %s\n", r.Prompt, r.Reply.GetContent())
	}
}
```

Run it:

```bash
go run .
```

The first two prompts print a reply. The repeated third prompt prints with a `[cache]` prefix, and the log shows one `reply cost` line per request that reached the provider. The `[stream]` section prints a reply in bursts of text instead of word by word. The `[batch]` section prints one line for each of the three greetings, in the order we listed them.

## Next steps

- Continue to [Tutorial 7: A2A client](/runtime/tutorials/07-a2a-client/) to call another agent.
- The [pipeline reference](/runtime/reference/pipeline/) and the rest of the [runtime reference](/runtime/reference/) list the complete API.
- The [runtime explanation](/runtime/explanation/) pages cover the architecture.
- [Handle errors](/runtime/how-to/pipeline/handle-errors/) and [monitor costs](/runtime/how-to/observability/monitor-costs/) go further with the patterns from this tutorial.
