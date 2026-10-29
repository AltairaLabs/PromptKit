package metrics

import (
	"errors"
	"regexp"
	"sort"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

const (
	// dimensionLabel is the label carrying a dimension's name in a derived
	// <metric>_dimension family.
	dimensionLabel = "dimension"
	// dimensionFamilySuffix names the family derived from an eval metric.
	dimensionFamilySuffix = "_dimension"

	// Reasons on eval_dimensions_dropped_total.
	dropReasonInvalidName = "invalid_name"
	dropReasonOverLimit   = "over_limit"
)

// dimensionNamePattern is what an exported dimension name must look like:
// snake_case, at most 64 characters. Nothing is normalized — "Clarity" is
// dropped, not folded into "clarity" — because a judge that varies the case
// of a key would otherwise mint one series per spelling.
var dimensionNamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,63}$`)

// dimensionFamily is one derived <metric>_dimension gauge family and the
// dimension names it has admitted, which is what the per-process cap counts.
type dimensionFamily struct {
	gauge *prometheus.GaugeVec
	seen  map[string]bool
}

// recordDimensions exports result.Metrics into the family derived from the
// eval's metric, when the collector was configured to. An errored result
// took no measurement, so its dimensions are not exported.
//
// Dimensions are visited in sorted order so the cap admits the same names
// regardless of map iteration.
func (mc *MetricContext) recordDimensions(
	result evals.EvalResult, name string, metric *evals.MetricDef, labelValues []string,
) {
	c := mc.collector
	if c.dimensionLimit == 0 || len(result.Metrics) == 0 || result.Error != "" {
		return
	}
	family := c.dimensionFamilyFor(name+dimensionFamilySuffix, metric)
	if family == nil {
		return
	}

	names := make([]string, 0, len(result.Metrics))
	for dim := range result.Metrics {
		names = append(names, dim)
	}
	sort.Strings(names)

	for _, dim := range names {
		if !dimensionNamePattern.MatchString(dim) {
			mc.dropDimension(metric.Name, dropReasonInvalidName)
			continue
		}
		if !c.admitDimension(family, dim) {
			mc.dropDimension(metric.Name, dropReasonOverLimit)
			continue
		}
		values := append(append(make([]string, 0, len(labelValues)+1), labelValues...), dim)
		family.gauge.WithLabelValues(values...).Set(result.Metrics[dim])
	}
}

// admitDimension reports whether dim may be exported: it already has a
// series, or the family is still under the cap.
func (c *Collector) admitDimension(family *dimensionFamily, dim string) bool {
	c.mu.RLock()
	seen := family.seen[dim]
	c.mu.RUnlock()
	if seen {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if family.seen[dim] {
		return true
	}
	if len(family.seen) >= c.dimensionLimit {
		return false
	}
	family.seen[dim] = true
	return true
}

func (mc *MetricContext) dropDimension(metricName, reason string) {
	if mc.collector.dimensionsDropped == nil {
		return
	}
	values := make([]string, 0, len(mc.collector.instanceLabels))
	for _, l := range mc.collector.instanceLabels {
		values = append(values, mc.labels[l])
	}
	values = append(values, metricName, reason)
	mc.collector.dimensionsDropped.WithLabelValues(values...).Inc()
}

// registerDimensionsDropped registers the drop counter, or reuses one already
// registered. An eval-only collector registers nothing else at construction,
// and workers build one per job against a shared (often the default)
// registry, so a second construction must not panic on a duplicate.
func (c *Collector) registerDimensionsDropped() *prometheus.CounterVec {
	vec := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace:   c.namespace,
		Name:        "eval_dimensions_dropped_total",
		Help:        "Eval dimensions not exported: name not snake_case, or past EvalDimensionLimit",
		ConstLabels: c.constLabels,
	}, c.allLabels([]string{"metric", "reason"}))
	if err := c.registerer.Register(vec); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			if existing, ok := already.ExistingCollector.(*prometheus.CounterVec); ok {
				return existing
			}
		}
		logger.Warn("eval dimension drops will not be counted", "error", err)
		return nil
	}
	return vec
}

// dimensionFamilyFor returns the derived family, registering it on first use.
// Its labels are the eval metric's plus dimension, and it is keyed on that
// label schema as well as its name, for the reason evalMetricKey gives. A
// family that cannot be registered (a pack label already called "dimension",
// an eval metric that already took the derived name, or the same name under
// another label set) is logged once and its dimensions are skipped thereafter.
func (c *Collector) dimensionFamilyFor(familyName string, metric *evals.MetricDef) *dimensionFamily {
	packLabels := sortedKeys(evals.MetricLabels(metric))
	key := familyName + "|" + strings.Join(packLabels, ",")

	c.mu.RLock()
	family, known := c.dimensionFamilies[key]
	c.mu.RUnlock()
	if known {
		return family
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.dimensionFamilies[key]; ok {
		return existing
	}

	vec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        familyName,
		Help:        "Named sub-scores of eval metric " + metric.Name,
		ConstLabels: c.constLabels,
	}, append(c.allLabels(packLabels), dimensionLabel))
	if err := c.registerer.Register(vec); err != nil {
		logger.Warn("eval dimensions not exported: family could not be registered",
			"metric", familyName, "error", err)
		c.dimensionFamilies[key] = nil
		return nil
	}
	family = &dimensionFamily{gauge: vec, seen: make(map[string]bool)}
	c.dimensionFamilies[key] = family
	return family
}
