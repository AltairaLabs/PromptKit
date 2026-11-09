package metrics

import (
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

// counterValue gathers reg and returns the value of the single-series
// counter named name, failing when it is absent.
func counterValue(t *testing.T, reg *prometheus.Registry, name string) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range families {
		if mf.GetName() == name {
			if len(mf.GetMetric()) != 1 {
				t.Fatalf("%s has %d series, want 1", name, len(mf.GetMetric()))
			}
			return mf.GetMetric()[0].GetCounter().GetValue()
		}
	}
	t.Fatalf("metric %s not gathered", name)
	return 0
}

func writeMetric(t *testing.T, m prometheus.Metric) *dto.Metric {
	t.Helper()
	var out dto.Metric
	if err := m.Write(&out); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	return &out
}

func exemplarLabels(e *dto.Exemplar) map[string]string {
	out := map[string]string{}
	for _, l := range e.GetLabel() {
		out[l.GetName()] = l.GetValue()
	}
	return out
}

func TestObserveWithExemplar(t *testing.T) {
	t.Run("attaches the exemplar to the observation's bucket", func(t *testing.T) {
		hist := prometheus.NewHistogram(prometheus.HistogramOpts{Name: "h_ex", Buckets: []float64{1, 5, 10}})
		observeWithExemplar(hist, 2.5, prometheus.Labels{"trace_id": "abc123"})

		h := writeMetric(t, hist).GetHistogram()
		if h.GetSampleCount() != 1 || h.GetSampleSum() != 2.5 {
			t.Fatalf("count=%d sum=%v, want 1 and 2.5", h.GetSampleCount(), h.GetSampleSum())
		}
		var found bool
		for _, b := range h.GetBucket() {
			if e := b.GetExemplar(); e != nil {
				found = true
				if b.GetUpperBound() != 5 || e.GetValue() != 2.5 || exemplarLabels(e)["trace_id"] != "abc123" {
					t.Errorf("exemplar on bucket %v = %v", b.GetUpperBound(), e)
				}
			}
		}
		if !found {
			t.Error("no exemplar recorded")
		}
	})

	t.Run("nil exemplar observes without one", func(t *testing.T) {
		hist := prometheus.NewHistogram(prometheus.HistogramOpts{Name: "h_nil", Buckets: []float64{1, 5, 10}})
		observeWithExemplar(hist, 2.5, nil)

		h := writeMetric(t, hist).GetHistogram()
		if h.GetSampleCount() != 1 || h.GetSampleSum() != 2.5 {
			t.Fatalf("count=%d sum=%v, want 1 and 2.5", h.GetSampleCount(), h.GetSampleSum())
		}
		for _, b := range h.GetBucket() {
			if b.GetExemplar() != nil {
				t.Errorf("unexpected exemplar on bucket %v", b.GetUpperBound())
			}
		}
	})
}

func TestIncWithExemplar(t *testing.T) {
	t.Run("increments and attaches the exemplar", func(t *testing.T) {
		counter := prometheus.NewCounter(prometheus.CounterOpts{Name: "c_ex"})
		incWithExemplar(counter, prometheus.Labels{"trace_id": "abc123"})

		c := writeMetric(t, counter).GetCounter()
		if c.GetValue() != 1 {
			t.Fatalf("value = %v, want 1", c.GetValue())
		}
		if got := exemplarLabels(c.GetExemplar())["trace_id"]; got != "abc123" {
			t.Errorf("exemplar trace_id = %q, want abc123", got)
		}
	})

	t.Run("nil exemplar increments without one", func(t *testing.T) {
		counter := prometheus.NewCounter(prometheus.CounterOpts{Name: "c_nil"})
		incWithExemplar(counter, nil)

		c := writeMetric(t, counter).GetCounter()
		if c.GetValue() != 1 {
			t.Fatalf("value = %v, want 1", c.GetValue())
		}
		if c.GetExemplar() != nil {
			t.Errorf("unexpected exemplar %v", c.GetExemplar())
		}
	})
}

// TestMetricContext_ConcurrentRecord records one counter from many
// goroutines released together; under -race, and every increment counts.
func TestMetricContext_ConcurrentRecord(t *testing.T) {
	c, reg := newTestCollector()
	ctx := c.Bind(nil)
	metric := &evals.MetricDef{Name: "concurrent", Type: evals.MetricCounter}

	const goroutines, perGoroutine = 10, 100
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range perGoroutine {
				if err := ctx.Record(evals.EvalResult{}, metric); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := counterValue(t, reg, "test_eval_concurrent"); got != goroutines*perGoroutine {
		t.Errorf("counter = %v, want %d", got, goroutines*perGoroutine)
	}
}
