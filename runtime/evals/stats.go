package evals

import (
	"math"
	"math/big"
)

// PassRate returns the fraction of true values in results (0.0 to 1.0).
// Returns 0 for an empty slice.
func PassRate(results []bool) float64 {
	if len(results) == 0 {
		return 0
	}
	passes := 0
	for _, r := range results {
		if r {
			passes++
		}
	}
	return float64(passes) / float64(len(results))
}

// PassAtK computes the pass@k metric from the Codex paper:
//
//	pass@k = 1 - C(n-c, k) / C(n, k)
//
// where n is the total number of samples, c is the number of correct
// (passing) samples, and k is the number of samples drawn. Returns 1.0
// if k <= 0 or all samples pass. Returns 0.0 if k > c (not enough
// passes to guarantee at least one in any draw of size k... actually
// the formula handles the general case).
func PassAtK(results []bool, k int) float64 {
	n := len(results)
	if k <= 0 {
		return 1.0
	}
	if n == 0 {
		return 0.0
	}

	c := 0
	for _, r := range results {
		if r {
			c++
		}
	}

	if c == n {
		return 1.0
	}
	if k > n {
		return 0.0
	}

	// pass@k = 1 - C(n-c, k) / C(n, k)
	// If n-c < k, then C(n-c, k) = 0, so pass@k = 1.
	if n-c < k {
		return 1.0
	}

	numerator := bigComb(n-c, k)
	denominator := bigComb(n, k)

	// Use big.Rat for exact division, then convert to float64.
	ratio := new(big.Rat).SetFrac(numerator, denominator)
	f, _ := ratio.Float64()
	return 1.0 - f
}

// FlakinessScore returns a value from 0.0 (deterministic) to 1.0
// (maximally flaky, i.e. 50/50 split). The formula is:
//
//	1 - |2*passRate - 1|
//
// Returns 0 for an empty slice.
func FlakinessScore(results []bool) float64 {
	if len(results) == 0 {
		return 0
	}
	pr := PassRate(results)
	return 1.0 - math.Abs(2*pr-1)
}

// bigComb computes C(n, k) as a *big.Int using math/big to avoid overflow.
func bigComb(n, k int) *big.Int {
	if k < 0 || k > n {
		return big.NewInt(0)
	}
	if k == 0 || k == n {
		return big.NewInt(1)
	}
	// C(n, k) = n! / (k! * (n-k)!)
	// Use the multiplicative formula to keep intermediate values smaller.
	// C(n, k) = product(n-k+i, i=1..k) / k!
	// Optimize by using the smaller of k and n-k.
	if k > n-k {
		k = n - k
	}
	result := big.NewInt(1)
	for i := 0; i < k; i++ {
		result.Mul(result, big.NewInt(int64(n-i)))
		result.Div(result, big.NewInt(int64(i+1)))
	}
	return result
}
