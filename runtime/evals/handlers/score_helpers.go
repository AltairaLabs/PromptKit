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
