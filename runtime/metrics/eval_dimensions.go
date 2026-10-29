package metrics

import (
	"regexp"
	"sort"

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
	values := make([]string, 0, len(mc.collector.instanceLabels))
	for _, l := range mc.collector.instanceLabels {
		values = append(values, mc.labels[l])
	}
	values = append(values, metricName, reason)
	mc.collector.dimensionsDropped.WithLabelValues(values...).Inc()
}

// dimensionFamilyFor returns the derived family, registering it on first use.
// Its labels are the eval metric's plus dimension; a family that cannot be
// registered (a pack label already called "dimension", a name collision) is
// logged once and its dimensions are skipped thereafter.
func (c *Collector) dimensionFamilyFor(familyName string, metric *evals.MetricDef) *dimensionFamily {
	c.mu.RLock()
	family, ok := c.dimensionFamilies[familyName]
	failed := c.dimensionFamilyErrs[familyName]
	c.mu.RUnlock()
	if ok {
		return family
	}
	if failed {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.dimensionFamilies[familyName]; ok {
		return existing
	}
	if c.dimensionFamilyErrs[familyName] {
		return nil
	}

	labelNames := append(c.allLabels(sortedKeys(evals.MetricLabels(metric))), dimensionLabel)
	vec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        familyName,
		Help:        "Named sub-scores of eval metric " + metric.Name,
		ConstLabels: c.constLabels,
	}, labelNames)
	if err := c.registerer.Register(vec); err != nil {
		logger.Warn("eval dimensions not exported: family could not be registered",
			"metric", familyName, "error", err)
		c.dimensionFamilyErrs[familyName] = true
		return nil
	}
	family = &dimensionFamily{gauge: vec, seen: make(map[string]bool)}
	c.dimensionFamilies[familyName] = family
	return family
}
