package handlers

// scorePtr returns a pointer to a float64 value.
func scorePtr(v float64) *float64 {
	return &v
}

// boolScore returns 1.0 for true, 0.0 for false.
func boolScore(b bool) *float64 {
	if b {
		return scorePtr(1.0)
	}
	return scorePtr(0.0)
}

// thresholdScore scores a measurement against a threshold the check itself
// declares, the way min_length and latency_budget do: 1.0 when value meets
// threshold, otherwise the fraction of it reached, clamped to [0, 1]. A
// perfect score is what an assertion or guardrail with no threshold of its own
// requires, so the check passes exactly when its threshold is met, and the
// shortfall still reads as a gradient. A non-positive threshold has no
// meaningful fraction, so a value below one scores 0.
func thresholdScore(value, threshold float64) float64 {
	if value >= threshold {
		return 1.0
	}
	if threshold <= 0 || value <= 0 {
		return 0
	}
	return value / threshold
}

// ratioScore returns a clamped ratio of found/total in [0,1].
// Returns 1.0 when total is zero (nothing to match = perfect).
func ratioScore(found, total int) *float64 {
	if total == 0 {
		return scorePtr(1.0)
	}
	s := float64(found) / float64(total)
	if s > 1.0 {
		s = 1.0
	}
	return scorePtr(s)
}

// inverseRatioScore returns 1 - (found/total), clamped to [0,1].
// Useful when "found" means violations (fewer = better).
// Returns 1.0 when total is zero.
func inverseRatioScore(found, total int) *float64 {
	if total == 0 {
		return scorePtr(1.0)
	}
	s := 1.0 - float64(found)/float64(total)
	if s < 0 {
		s = 0
	}
	return scorePtr(s)
}
