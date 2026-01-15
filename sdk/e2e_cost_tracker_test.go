//go:build e2e

package sdk

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// =============================================================================
// E2E Cost Tracker
//
// Tracks API costs across all e2e tests and generates a summary report.
// Thread-safe for use with parallel tests.
// =============================================================================

// ProviderCosts holds aggregated costs for a single provider.
type ProviderCosts struct {
	Provider     string
	Model        string
	Calls        int
	InputTokens  int
	OutputTokens int
	CachedTokens int
	TotalCost    float64
	TotalLatency time.Duration
}

// CostTracker accumulates API costs across tests.
type CostTracker struct {
	mu    sync.Mutex
	costs map[string]*ProviderCosts // keyed by "provider:model"
}

// globalCostTracker is the singleton instance used across all tests.
var globalCostTracker = &CostTracker{
	costs: make(map[string]*ProviderCosts),
}

// GetCostTracker returns the global cost tracker instance.
func GetCostTracker() *CostTracker {
	return globalCostTracker
}

// RecordCost records a provider call's cost information.
func (ct *CostTracker) RecordCost(data *events.ProviderCallCompletedData) {
	if data == nil {
		return
	}

	ct.mu.Lock()
	defer ct.mu.Unlock()

	key := fmt.Sprintf("%s:%s", data.Provider, data.Model)
	pc, exists := ct.costs[key]
	if !exists {
		pc = &ProviderCosts{
			Provider: data.Provider,
			Model:    data.Model,
		}
		ct.costs[key] = pc
	}

	pc.Calls++
	pc.InputTokens += data.InputTokens
	pc.OutputTokens += data.OutputTokens
	pc.CachedTokens += data.CachedTokens
	pc.TotalCost += data.Cost
	pc.TotalLatency += data.Duration
}

// GetSummary returns all provider costs sorted by provider name.
func (ct *CostTracker) GetSummary() []ProviderCosts {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	result := make([]ProviderCosts, 0, len(ct.costs))
	for _, pc := range ct.costs {
		result = append(result, *pc)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Provider != result[j].Provider {
			return result[i].Provider < result[j].Provider
		}
		return result[i].Model < result[j].Model
	})

	return result
}

// TotalCost returns the total cost across all providers.
func (ct *CostTracker) TotalCost() float64 {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	var total float64
	for _, pc := range ct.costs {
		total += pc.TotalCost
	}
	return total
}

// Reset clears all tracked costs.
func (ct *CostTracker) Reset() {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.costs = make(map[string]*ProviderCosts)
}

// PrintReport prints a formatted cost report to stdout.
func (ct *CostTracker) PrintReport() {
	summary := ct.GetSummary()
	if len(summary) == 0 {
		fmt.Println("\n📊 E2E Cost Report: No API calls recorded")
		return
	}

	fmt.Println()
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println("📊 E2E Test Cost Report")
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println()

	// Calculate column widths
	maxProviderLen := 8
	maxModelLen := 5
	for _, pc := range summary {
		if len(pc.Provider) > maxProviderLen {
			maxProviderLen = len(pc.Provider)
		}
		if len(pc.Model) > maxModelLen {
			maxModelLen = len(pc.Model)
		}
	}

	// Header
	fmt.Printf("%-*s  %-*s  %6s  %10s  %10s  %10s  %12s  %10s\n",
		maxProviderLen, "Provider",
		maxModelLen, "Model",
		"Calls",
		"Input Tok",
		"Output Tok",
		"Cached Tok",
		"Cost",
		"Avg Latency",
	)
	fmt.Println(strings.Repeat("-", 80+maxProviderLen+maxModelLen))

	var totalCalls int
	var totalInput, totalOutput, totalCached int
	var totalCost float64
	var totalLatency time.Duration

	for _, pc := range summary {
		avgLatency := time.Duration(0)
		if pc.Calls > 0 {
			avgLatency = pc.TotalLatency / time.Duration(pc.Calls)
		}

		fmt.Printf("%-*s  %-*s  %6d  %10d  %10d  %10d  $%10.6f  %10s\n",
			maxProviderLen, pc.Provider,
			maxModelLen, pc.Model,
			pc.Calls,
			pc.InputTokens,
			pc.OutputTokens,
			pc.CachedTokens,
			pc.TotalCost,
			formatDuration(avgLatency),
		)

		totalCalls += pc.Calls
		totalInput += pc.InputTokens
		totalOutput += pc.OutputTokens
		totalCached += pc.CachedTokens
		totalCost += pc.TotalCost
		totalLatency += pc.TotalLatency
	}

	// Footer
	fmt.Println(strings.Repeat("-", 80+maxProviderLen+maxModelLen))

	avgLatency := time.Duration(0)
	if totalCalls > 0 {
		avgLatency = totalLatency / time.Duration(totalCalls)
	}

	fmt.Printf("%-*s  %-*s  %6d  %10d  %10d  %10d  $%10.6f  %10s\n",
		maxProviderLen, "TOTAL",
		maxModelLen, "",
		totalCalls,
		totalInput,
		totalOutput,
		totalCached,
		totalCost,
		formatDuration(avgLatency),
	)

	fmt.Println()
	fmt.Println(strings.Repeat("=", 80))
}

// formatDuration formats a duration in a human-readable way.
func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

// CreateCostTrackingEventBus creates an event bus that tracks costs.
func CreateCostTrackingEventBus() *events.EventBus {
	bus := events.NewEventBus()

	bus.Subscribe(events.EventProviderCallCompleted, func(e *events.Event) {
		if data, ok := e.Data.(*events.ProviderCallCompletedData); ok {
			GetCostTracker().RecordCost(data)
		}
	})

	return bus
}
