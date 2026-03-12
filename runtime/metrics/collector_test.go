package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// newTestCollector creates a Collector with a fresh registry for test isolation.
func newTestCollector(opts ...func(*CollectorOpts)) (*Collector, *prometheus.Registry) {
	reg := prometheus.NewRegistry()
	o := CollectorOpts{
		Registerer: reg,
		Namespace:  "test",
	}
	for _, fn := range opts {
		fn(&o)
	}
	return NewCollector(o), reg
}

// gatherMetrics collects all metrics from a registry and returns them as
// Prometheus text format string for assertion.
func gatherMetrics(t *testing.T, reg *prometheus.Registry) string {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gathering metrics: %v", err)
	}
	var buf strings.Builder
	enc := expfmt.NewEncoder(&buf, expfmt.NewFormat(expfmt.TypeTextPlain))
	for _, mf := range families {
		if err := enc.Encode(mf); err != nil {
			t.Fatalf("encoding metric family: %v", err)
		}
	}
	return buf.String()
}

func TestNewCollector_DefaultNamespace(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewCollector(CollectorOpts{Registerer: reg})
	if c.namespace != defaultNamespace {
		t.Errorf("namespace = %q, want %q", c.namespace, defaultNamespace)
	}
	if c.Registry() != reg {
		t.Error("Registry() should return the provided registry")
	}
}

func TestNewCollector_CustomNamespace(t *testing.T) {
	c, _ := newTestCollector(func(o *CollectorOpts) {
		o.Namespace = "myapp"
	})
	if c.namespace != "myapp" {
		t.Errorf("namespace = %q, want %q", c.namespace, "myapp")
	}
}

func TestNewCollector_NilRegisterer_UsesDefault(t *testing.T) {
	// Verify it uses DefaultRegisterer and doesn't panic.
	// Note: DefaultRegisterer may or may not be a *Registry depending on
	// the prometheus library version, so we just check it's non-nil.
	c := NewCollector(CollectorOpts{
		Namespace:              "safe_test_ns",
		DisablePipelineMetrics: true, // avoid polluting global registry
	})
	if c.registerer == nil {
		t.Error("registerer should not be nil")
	}
}

func TestNewCollector_DisablePipelineMetrics(t *testing.T) {
	c, reg := newTestCollector(func(o *CollectorOpts) {
		o.DisablePipelineMetrics = true
	})
	if c.pipelineDuration != nil {
		t.Error("pipeline metrics should be nil when disabled")
	}
	// No pipeline metrics should be registered.
	families, _ := reg.Gather()
	if len(families) != 0 {
		t.Errorf("expected 0 metric families, got %d", len(families))
	}
}

func TestNewEvalOnlyCollector(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewEvalOnlyCollector(CollectorOpts{
		Registerer: reg,
		Namespace:  "evalonly",
	})
	if c.pipelineDuration != nil {
		t.Error("pipeline metrics should be nil for eval-only collector")
	}
	if c.disablePipeline != true {
		t.Error("disablePipeline should be true for eval-only collector")
	}
	if c.disableEval {
		t.Error("disableEval should be false for eval-only collector")
	}
	// No pipeline metrics should be registered.
	families, _ := reg.Gather()
	if len(families) != 0 {
		t.Errorf("expected 0 metric families, got %d", len(families))
	}
}

func TestNewEvalOnlyCollector_PreservesOtherOpts(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewEvalOnlyCollector(CollectorOpts{
		Registerer:     reg,
		Namespace:      "custom",
		ConstLabels:    prometheus.Labels{"env": "test"},
		InstanceLabels: []string{"tenant"},
	})
	if c.namespace != "custom" {
		t.Errorf("namespace = %q, want %q", c.namespace, "custom")
	}
	if len(c.instanceLabels) != 1 || c.instanceLabels[0] != "tenant" {
		t.Errorf("instanceLabels = %v, want [tenant]", c.instanceLabels)
	}
}

func TestCollector_InstanceLabels_Sorted(t *testing.T) {
	c, _ := newTestCollector(func(o *CollectorOpts) {
		o.InstanceLabels = []string{"z_label", "a_label", "m_label"}
	})
	want := []string{"a_label", "m_label", "z_label"}
	if len(c.instanceLabels) != len(want) {
		t.Fatalf("instanceLabels length = %d, want %d", len(c.instanceLabels), len(want))
	}
	for i, l := range c.instanceLabels {
		if l != want[i] {
			t.Errorf("instanceLabels[%d] = %q, want %q", i, l, want[i])
		}
	}
}

func TestMetricContext_PipelineCompleted(t *testing.T) {
	c, reg := newTestCollector()
	ctx := c.Bind(nil)

	ctx.OnEvent(&events.Event{
		Type: events.EventPipelineCompleted,
		Data: &events.PipelineCompletedData{
			Duration: 2500 * time.Millisecond,
		},
	})

	output := gatherMetrics(t, reg)
	if !strings.Contains(output, "test_pipeline_duration_seconds") {
		t.Error("expected pipeline_duration_seconds metric")
	}
}

func TestMetricContext_PipelineFailed(t *testing.T) {
	c, reg := newTestCollector()
	ctx := c.Bind(nil)

	ctx.OnEvent(&events.Event{
		Type: events.EventPipelineFailed,
		Data: &events.PipelineFailedData{
			Duration: 1 * time.Second,
			Error:    nil,
		},
	})

	output := gatherMetrics(t, reg)
	if !strings.Contains(output, `status="error"`) {
		t.Error("expected status=error label in pipeline metric")
	}
}

func TestMetricContext_ProviderCallCompleted(t *testing.T) {
	c, reg := newTestCollector()
	ctx := c.Bind(nil)

	ctx.OnEvent(&events.Event{
		Type: events.EventProviderCallCompleted,
		Data: &events.ProviderCallCompletedData{
			Provider:     "openai",
			Model:        "gpt-4o",
			Duration:     500 * time.Millisecond,
			InputTokens:  100,
			OutputTokens: 50,
			CachedTokens: 10,
			Cost:         0.005,
		},
	})

	output := gatherMetrics(t, reg)

	checks := []string{
		"test_provider_request_duration_seconds",
		"test_provider_requests_total",
		"test_provider_tokens_total",
		"test_provider_cost_total",
		`provider="openai"`,
		`model="gpt-4o"`,
		`type="input"`,
		`type="output"`,
		`type="cached"`,
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("expected %q in output", check)
		}
	}
}

func TestMetricContext_ProviderCallFailed(t *testing.T) {
	c, reg := newTestCollector()
	ctx := c.Bind(nil)

	ctx.OnEvent(&events.Event{
		Type: events.EventProviderCallFailed,
		Data: &events.ProviderCallFailedData{
			Provider: "anthropic",
			Model:    "claude-3",
			Duration: 200 * time.Millisecond,
		},
	})

	output := gatherMetrics(t, reg)
	if !strings.Contains(output, `status="error"`) {
		t.Error("expected status=error in provider request metric")
	}
	if !strings.Contains(output, `provider="anthropic"`) {
		t.Error("expected provider=anthropic label")
	}
}

func TestMetricContext_ToolCallCompleted(t *testing.T) {
	c, reg := newTestCollector()
	ctx := c.Bind(nil)

	ctx.OnEvent(&events.Event{
		Type: events.EventToolCallCompleted,
		Data: &events.ToolCallCompletedData{
			ToolName: "search",
			Duration: 100 * time.Millisecond,
			Status:   "success",
		},
	})

	output := gatherMetrics(t, reg)
	if !strings.Contains(output, `tool="search"`) {
		t.Error("expected tool=search label")
	}
	if !strings.Contains(output, "test_tool_calls_total") {
		t.Error("expected tool_calls_total metric")
	}
}

func TestMetricContext_ToolCallFailed(t *testing.T) {
	c, reg := newTestCollector()
	ctx := c.Bind(nil)

	ctx.OnEvent(&events.Event{
		Type: events.EventToolCallFailed,
		Data: &events.ToolCallFailedData{
			ToolName: "calculator",
			Duration: 50 * time.Millisecond,
		},
	})

	output := gatherMetrics(t, reg)
	if !strings.Contains(output, `status="error"`) {
		t.Error("expected status=error in tool metric")
	}
}

func TestMetricContext_ValidationPassed(t *testing.T) {
	c, reg := newTestCollector()
	ctx := c.Bind(nil)

	ctx.OnEvent(&events.Event{
		Type: events.EventValidationPassed,
		Data: &events.ValidationPassedData{
			ValidatorName: "profanity_filter",
			ValidatorType: "output",
			Duration:      5 * time.Millisecond,
		},
	})

	output := gatherMetrics(t, reg)
	if !strings.Contains(output, `validator="profanity_filter"`) {
		t.Error("expected validator label")
	}
	if !strings.Contains(output, `status="passed"`) {
		t.Error("expected status=passed label")
	}
}

func TestMetricContext_ValidationFailed(t *testing.T) {
	c, reg := newTestCollector()
	ctx := c.Bind(nil)

	ctx.OnEvent(&events.Event{
		Type: events.EventValidationFailed,
		Data: &events.ValidationFailedData{
			ValidatorName: "length_check",
			ValidatorType: "output",
			Duration:      2 * time.Millisecond,
		},
	})

	output := gatherMetrics(t, reg)
	if !strings.Contains(output, `status="failed"`) {
		t.Error("expected status=failed label")
	}
}

func TestMetricContext_IgnoredEvents(t *testing.T) {
	c, reg := newTestCollector()
	ctx := c.Bind(nil)

	// These events should not produce any metrics.
	ignored := []*events.Event{
		{Type: events.EventPipelineStarted, Data: &events.PipelineStartedData{}},
		{Type: events.EventMessageCreated, Data: &events.MessageCreatedData{}},
		{Type: events.EventConversationStarted, Data: &events.ConversationStartedData{}},
	}
	for _, e := range ignored {
		ctx.OnEvent(e)
	}

	families, _ := reg.Gather()
	// Only pipeline metrics are registered, but none should have observations.
	for _, fam := range families {
		for _, m := range fam.GetMetric() {
			if m.GetHistogram() != nil && m.GetHistogram().GetSampleCount() > 0 {
				t.Errorf("unexpected observation in %s", fam.GetName())
			}
			if m.GetCounter() != nil && m.GetCounter().GetValue() > 0 {
				t.Errorf("unexpected counter value in %s", fam.GetName())
			}
		}
	}
}

func TestMetricContext_DisabledPipeline_NoOp(t *testing.T) {
	c, reg := newTestCollector(func(o *CollectorOpts) {
		o.DisablePipelineMetrics = true
	})
	ctx := c.Bind(nil)

	// Should not panic even though pipeline metrics are nil.
	ctx.OnEvent(&events.Event{
		Type: events.EventPipelineCompleted,
		Data: &events.PipelineCompletedData{Duration: time.Second},
	})

	families, _ := reg.Gather()
	if len(families) != 0 {
		t.Errorf("expected 0 metric families with disabled pipeline, got %d", len(families))
	}
}

func TestMetricContext_InstanceLabels(t *testing.T) {
	c, reg := newTestCollector(func(o *CollectorOpts) {
		o.InstanceLabels = []string{"tenant", "prompt_name"}
	})
	ctx := c.Bind(map[string]string{
		"tenant":      "acme",
		"prompt_name": "support",
	})

	ctx.OnEvent(&events.Event{
		Type: events.EventPipelineCompleted,
		Data: &events.PipelineCompletedData{Duration: time.Second},
	})

	output := gatherMetrics(t, reg)
	if !strings.Contains(output, `tenant="acme"`) {
		t.Error("expected tenant=acme instance label")
	}
	if !strings.Contains(output, `prompt_name="support"`) {
		t.Error("expected prompt_name=support instance label")
	}
}

func TestMetricContext_MultipleBinds_DifferentLabels(t *testing.T) {
	c, reg := newTestCollector(func(o *CollectorOpts) {
		o.InstanceLabels = []string{"tenant"}
	})

	ctx1 := c.Bind(map[string]string{"tenant": "acme"})
	ctx2 := c.Bind(map[string]string{"tenant": "globex"})

	ctx1.OnEvent(&events.Event{
		Type: events.EventPipelineCompleted,
		Data: &events.PipelineCompletedData{Duration: time.Second},
	})
	ctx2.OnEvent(&events.Event{
		Type: events.EventPipelineCompleted,
		Data: &events.PipelineCompletedData{Duration: 2 * time.Second},
	})

	output := gatherMetrics(t, reg)
	if !strings.Contains(output, `tenant="acme"`) {
		t.Error("expected tenant=acme")
	}
	if !strings.Contains(output, `tenant="globex"`) {
		t.Error("expected tenant=globex")
	}
}

func TestMetricContext_ConstLabels(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewCollector(CollectorOpts{
		Registerer:  reg,
		Namespace:   "test",
		ConstLabels: prometheus.Labels{"env": "prod", "region": "us-east-1"},
	})
	ctx := c.Bind(nil)

	ctx.OnEvent(&events.Event{
		Type: events.EventPipelineCompleted,
		Data: &events.PipelineCompletedData{Duration: time.Second},
	})

	output := gatherMetrics(t, reg)
	if !strings.Contains(output, `env="prod"`) {
		t.Error("expected env=prod const label")
	}
	if !strings.Contains(output, `region="us-east-1"`) {
		t.Error("expected region=us-east-1 const label")
	}
}

// --- Eval metric tests ---

func float64Ptr(v float64) *float64 { return &v }

func TestMetricContext_EvalGauge(t *testing.T) {
	c, reg := newTestCollector()
	ctx := c.Bind(nil)

	err := ctx.Record(
		evals.EvalResult{Score: float64Ptr(0.85)},
		&evals.MetricDef{Name: "quality_score", Type: evals.MetricGauge},
	)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	output := gatherMetrics(t, reg)
	if !strings.Contains(output, "test_quality_score") {
		t.Error("expected quality_score metric")
	}
	if !strings.Contains(output, "0.85") {
		t.Error("expected value 0.85")
	}
}

func TestMetricContext_EvalCounter(t *testing.T) {
	c, reg := newTestCollector()
	ctx := c.Bind(nil)

	metric := &evals.MetricDef{Name: "eval_runs", Type: evals.MetricCounter}
	for range 3 {
		if err := ctx.Record(evals.EvalResult{}, metric); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	output := gatherMetrics(t, reg)
	if !strings.Contains(output, "test_eval_runs") {
		t.Error("expected eval_runs metric")
	}
	if !strings.Contains(output, "3") {
		t.Error("expected counter value 3")
	}
}

func TestMetricContext_EvalHistogram(t *testing.T) {
	c, reg := newTestCollector()
	ctx := c.Bind(nil)

	metric := &evals.MetricDef{Name: "latency", Type: evals.MetricHistogram}
	for _, v := range []float64{0.1, 0.5, 1.0, 2.5} {
		if err := ctx.Record(evals.EvalResult{Score: float64Ptr(v)}, metric); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	output := gatherMetrics(t, reg)
	if !strings.Contains(output, "test_latency_bucket") {
		t.Error("expected histogram bucket")
	}
	if !strings.Contains(output, "test_latency_sum") {
		t.Error("expected histogram sum")
	}
	if !strings.Contains(output, "test_latency_count 4") {
		t.Error("expected histogram count 4")
	}
}

func TestMetricContext_EvalBoolean(t *testing.T) {
	c, reg := newTestCollector()
	ctx := c.Bind(nil)

	metric := &evals.MetricDef{Name: "safety_check", Type: evals.MetricBoolean}

	if err := ctx.Record(evals.EvalResult{Passed: true}, metric); err != nil {
		t.Fatalf("Record: %v", err)
	}

	output := gatherMetrics(t, reg)
	if !strings.Contains(output, "test_safety_check 1") {
		t.Errorf("expected boolean metric value 1, got:\n%s", output)
	}

	// Record failed
	if err := ctx.Record(evals.EvalResult{Passed: false}, metric); err != nil {
		t.Fatalf("Record: %v", err)
	}

	output = gatherMetrics(t, reg)
	if !strings.Contains(output, "test_safety_check 0") {
		t.Errorf("expected boolean metric value 0, got:\n%s", output)
	}
}

func TestMetricContext_EvalWithLabels(t *testing.T) {
	c, reg := newTestCollector()
	ctx := c.Bind(nil)

	metric := &evals.MetricDef{
		Name:   "quality",
		Type:   evals.MetricGauge,
		Labels: map[string]string{"category": "tone", "eval_type": "llm_judge"},
	}

	if err := ctx.Record(evals.EvalResult{Score: float64Ptr(0.9)}, metric); err != nil {
		t.Fatalf("Record: %v", err)
	}

	output := gatherMetrics(t, reg)
	if !strings.Contains(output, `category="tone"`) {
		t.Error("expected category=tone label")
	}
	if !strings.Contains(output, `eval_type="llm_judge"`) {
		t.Error("expected eval_type=llm_judge label")
	}
}

func TestMetricContext_EvalWithInstanceLabels(t *testing.T) {
	c, reg := newTestCollector(func(o *CollectorOpts) {
		o.InstanceLabels = []string{"tenant"}
	})
	ctx := c.Bind(map[string]string{"tenant": "acme"})

	metric := &evals.MetricDef{
		Name: "quality",
		Type: evals.MetricGauge,
	}

	if err := ctx.Record(evals.EvalResult{Score: float64Ptr(0.75)}, metric); err != nil {
		t.Fatalf("Record: %v", err)
	}

	output := gatherMetrics(t, reg)
	if !strings.Contains(output, `tenant="acme"`) {
		t.Error("expected tenant=acme instance label on eval metric")
	}
}

func TestMetricContext_EvalNilMetric(t *testing.T) {
	c, _ := newTestCollector()
	ctx := c.Bind(nil)

	err := ctx.Record(evals.EvalResult{}, nil)
	if err == nil {
		t.Error("expected error for nil metric")
	}
}

func TestMetricContext_EvalUnknownType(t *testing.T) {
	c, _ := newTestCollector()
	ctx := c.Bind(nil)

	err := ctx.Record(evals.EvalResult{}, &evals.MetricDef{
		Name: "bad", Type: "unknown_type",
	})
	if err == nil {
		t.Error("expected error for unknown metric type")
	}
}

func TestMetricContext_EvalDisabled(t *testing.T) {
	c, reg := newTestCollector(func(o *CollectorOpts) {
		o.DisableEvalMetrics = true
	})
	ctx := c.Bind(nil)

	err := ctx.Record(
		evals.EvalResult{Score: float64Ptr(1.0)},
		&evals.MetricDef{Name: "quality", Type: evals.MetricGauge},
	)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	output := gatherMetrics(t, reg)
	if strings.Contains(output, "quality") {
		t.Error("eval metric should not be recorded when disabled")
	}
}

func TestMetricContext_EvalNamespacePrefix(t *testing.T) {
	c, reg := newTestCollector()
	ctx := c.Bind(nil)

	// Already prefixed name should not double-prefix.
	if err := ctx.Record(
		evals.EvalResult{Score: float64Ptr(1.0)},
		&evals.MetricDef{Name: "test_already_prefixed", Type: evals.MetricGauge},
	); err != nil {
		t.Fatalf("Record: %v", err)
	}

	output := gatherMetrics(t, reg)
	if strings.Contains(output, "test_test_already_prefixed") {
		t.Error("should not double-prefix metric name")
	}
	if !strings.Contains(output, "test_already_prefixed") {
		t.Error("expected test_already_prefixed metric")
	}
}

func TestMetricContext_EvalMetricValue_Preferred(t *testing.T) {
	c, reg := newTestCollector()
	ctx := c.Bind(nil)

	if err := ctx.Record(
		evals.EvalResult{
			Score:       float64Ptr(0.5),
			MetricValue: float64Ptr(0.99),
		},
		&evals.MetricDef{Name: "quality", Type: evals.MetricGauge},
	); err != nil {
		t.Fatalf("Record: %v", err)
	}

	output := gatherMetrics(t, reg)
	if !strings.Contains(output, "0.99") {
		t.Error("expected MetricValue (0.99) to be preferred over Score (0.5)")
	}
}

func TestMetricContext_ConcurrentRecord(t *testing.T) {
	c, _ := newTestCollector()
	ctx := c.Bind(nil)

	metric := &evals.MetricDef{Name: "concurrent", Type: evals.MetricCounter}

	done := make(chan struct{})
	for range 10 {
		go func() {
			for range 100 {
				_ = ctx.Record(evals.EvalResult{}, metric)
			}
			done <- struct{}{}
		}()
	}

	for range 10 {
		<-done
	}
}

func TestPrefixedName(t *testing.T) {
	tests := []struct {
		ns, name, want string
	}{
		{"test", "quality", "test_quality"},
		{"test", "test_quality", "test_quality"},
		{"promptkit", "promptkit_pipeline", "promptkit_pipeline"},
		{"myapp", "quality", "myapp_quality"},
	}
	for _, tt := range tests {
		got := prefixedName(tt.ns, tt.name)
		if got != tt.want {
			t.Errorf("prefixedName(%q, %q) = %q, want %q", tt.ns, tt.name, got, tt.want)
		}
	}
}

func TestSortedKeys(t *testing.T) {
	got := sortedKeys(map[string]string{"z": "1", "a": "2", "m": "3"})
	want := []string{"a", "m", "z"}
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %d, want %d", len(got), len(want))
	}
	for i, k := range got {
		if k != want[i] {
			t.Errorf("sortedKeys[%d] = %q, want %q", i, k, want[i])
		}
	}
}

func TestSortedKeys_Empty(t *testing.T) {
	got := sortedKeys(nil)
	if got != nil {
		t.Errorf("expected nil for empty map, got %v", got)
	}
}

func TestMetricContext_WrongEventDataType(t *testing.T) {
	c, reg := newTestCollector()
	ctx := c.Bind(nil)

	// Pass wrong data type for event — should not panic, just skip.
	ctx.OnEvent(&events.Event{
		Type: events.EventPipelineCompleted,
		Data: &events.PipelineStartedData{}, // wrong type
	})

	// No observations should be recorded.
	families, _ := reg.Gather()
	for _, fam := range families {
		if fam.GetName() == "test_pipeline_duration_seconds" {
			for _, m := range fam.GetMetric() {
				if m.GetHistogram() != nil && m.GetHistogram().GetSampleCount() > 0 {
					t.Error("should not record metric with wrong data type")
				}
			}
		}
	}
}
