package evals

import (
	"math"
	"testing"
)

func TestPassRate(t *testing.T) {
	t.Run("empty slice", func(t *testing.T) {
		got := PassRate(nil)
		if got != 0 {
			t.Errorf("PassRate(nil) = %v, want 0", got)
		}
	})

	t.Run("all pass", func(t *testing.T) {
		got := PassRate([]bool{true, true, true})
		if got != 1.0 {
			t.Errorf("PassRate(all true) = %v, want 1.0", got)
		}
	})

	t.Run("all fail", func(t *testing.T) {
		got := PassRate([]bool{false, false, false})
		if got != 0 {
			t.Errorf("PassRate(all false) = %v, want 0", got)
		}
	})

	t.Run("mixed", func(t *testing.T) {
		got := PassRate([]bool{true, false, true, false, true})
		if got != 0.6 {
			t.Errorf("PassRate(3/5) = %v, want 0.6", got)
		}
	})

	t.Run("single pass", func(t *testing.T) {
		got := PassRate([]bool{true})
		if got != 1.0 {
			t.Errorf("PassRate([true]) = %v, want 1.0", got)
		}
	})

	t.Run("single fail", func(t *testing.T) {
		got := PassRate([]bool{false})
		if got != 0 {
			t.Errorf("PassRate([false]) = %v, want 0", got)
		}
	})
}

func TestPassAtK(t *testing.T) {
	t.Run("empty slice", func(t *testing.T) {
		got := PassAtK(nil, 1)
		if got != 0 {
			t.Errorf("PassAtK(nil, 1) = %v, want 0", got)
		}
	})

	t.Run("k zero", func(t *testing.T) {
		got := PassAtK([]bool{false, false}, 0)
		if got != 1.0 {
			t.Errorf("PassAtK(_, 0) = %v, want 1.0", got)
		}
	})

	t.Run("k negative", func(t *testing.T) {
		got := PassAtK([]bool{false}, -1)
		if got != 1.0 {
			t.Errorf("PassAtK(_, -1) = %v, want 1.0", got)
		}
	})

	t.Run("all pass", func(t *testing.T) {
		got := PassAtK([]bool{true, true, true}, 2)
		if got != 1.0 {
			t.Errorf("PassAtK(all true, 2) = %v, want 1.0", got)
		}
	})

	t.Run("all fail k=1", func(t *testing.T) {
		got := PassAtK([]bool{false, false, false}, 1)
		if got != 0 {
			t.Errorf("PassAtK(all false, 1) = %v, want 0", got)
		}
	})

	t.Run("k greater than n", func(t *testing.T) {
		got := PassAtK([]bool{true, false}, 5)
		if got != 0 {
			t.Errorf("PassAtK(n=2, k=5) = %v, want 0", got)
		}
	})

	t.Run("1 of 5 pass k=1", func(t *testing.T) {
		// n=5, c=1, k=1: pass@1 = 1 - C(4,1)/C(5,1) = 1 - 4/5 = 0.2
		got := PassAtK([]bool{true, false, false, false, false}, 1)
		if math.Abs(got-0.2) > 1e-9 {
			t.Errorf("PassAtK(1/5, 1) = %v, want 0.2", got)
		}
	})

	t.Run("2 of 5 pass k=2", func(t *testing.T) {
		// n=5, c=2, k=2: pass@2 = 1 - C(3,2)/C(5,2) = 1 - 3/10 = 0.7
		got := PassAtK([]bool{true, true, false, false, false}, 2)
		if math.Abs(got-0.7) > 1e-9 {
			t.Errorf("PassAtK(2/5, 2) = %v, want 0.7", got)
		}
	})

	t.Run("3 of 5 pass k=3", func(t *testing.T) {
		// n=5, c=3, k=3: pass@3 = 1 - C(2,3)/C(5,3) = 1 - 0/10 = 1.0
		// C(2,3) = 0 because 3 > 2
		got := PassAtK([]bool{true, true, true, false, false}, 3)
		if got != 1.0 {
			t.Errorf("PassAtK(3/5, 3) = %v, want 1.0", got)
		}
	})

	t.Run("k equals n", func(t *testing.T) {
		// n=4, c=2, k=4: pass@4 = 1 - C(2,4)/C(4,4) = 1 - 0/1 = 1.0
		got := PassAtK([]bool{true, true, false, false}, 4)
		if got != 1.0 {
			t.Errorf("PassAtK(2/4, k=4) = %v, want 1.0", got)
		}
	})

	t.Run("large n no overflow", func(t *testing.T) {
		// n=200, c=10, k=5 — should not overflow with big.Int
		results := make([]bool, 200)
		for i := 0; i < 10; i++ {
			results[i] = true
		}
		got := PassAtK(results, 5)
		if got < 0 || got > 1 {
			t.Errorf("PassAtK(10/200, 5) = %v, want value in [0, 1]", got)
		}
	})
}

func TestFlakinessScore(t *testing.T) {
	t.Run("empty slice", func(t *testing.T) {
		got := FlakinessScore(nil)
		if got != 0 {
			t.Errorf("FlakinessScore(nil) = %v, want 0", got)
		}
	})

	t.Run("all pass", func(t *testing.T) {
		got := FlakinessScore([]bool{true, true, true})
		if got != 0 {
			t.Errorf("FlakinessScore(all true) = %v, want 0", got)
		}
	})

	t.Run("all fail", func(t *testing.T) {
		got := FlakinessScore([]bool{false, false, false})
		if got != 0 {
			t.Errorf("FlakinessScore(all false) = %v, want 0", got)
		}
	})

	t.Run("50/50 split", func(t *testing.T) {
		got := FlakinessScore([]bool{true, false, true, false})
		if got != 1.0 {
			t.Errorf("FlakinessScore(50/50) = %v, want 1.0", got)
		}
	})

	t.Run("symmetry 30 and 70 percent", func(t *testing.T) {
		// 3 of 10 pass
		results30 := []bool{true, true, true, false, false, false, false, false, false, false}
		// 7 of 10 pass
		results70 := []bool{true, true, true, true, true, true, true, false, false, false}

		score30 := FlakinessScore(results30)
		score70 := FlakinessScore(results70)

		if math.Abs(score30-score70) > 1e-9 {
			t.Errorf("FlakinessScore symmetry: 30%% = %v, 70%% = %v, want equal", score30, score70)
		}
	})

	t.Run("single element pass", func(t *testing.T) {
		got := FlakinessScore([]bool{true})
		if got != 0 {
			t.Errorf("FlakinessScore([true]) = %v, want 0", got)
		}
	})

	t.Run("mostly pass", func(t *testing.T) {
		// 9 of 10 pass: passRate=0.9, flakiness = 1 - |2*0.9 - 1| = 1 - 0.8 = 0.2
		results := []bool{true, true, true, true, true, true, true, true, true, false}
		got := FlakinessScore(results)
		if math.Abs(got-0.2) > 1e-9 {
			t.Errorf("FlakinessScore(9/10) = %v, want 0.2", got)
		}
	})
}
