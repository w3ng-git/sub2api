package service

import (
	"testing"
)

func TestShouldTransferCacheTokens(t *testing.T) {
	tests := []struct {
		name        string
		probability float64
		wantResult  *bool // nil means random, true/false means deterministic
	}{
		{
			name:        "probability 0 always returns false",
			probability: 0,
			wantResult:  testBoolPtr(false),
		},
		{
			name:        "probability negative always returns false",
			probability: -0.5,
			wantResult:  testBoolPtr(false),
		},
		{
			name:        "probability 1 always returns true",
			probability: 1.0,
			wantResult:  testBoolPtr(true),
		},
		{
			name:        "probability > 1 always returns true",
			probability: 1.5,
			wantResult:  testBoolPtr(true),
		},
		{
			name:        "probability 0.5 is random",
			probability: 0.5,
			wantResult:  nil, // random
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantResult != nil {
				// Deterministic case - check result is always as expected
				for i := 0; i < 100; i++ {
					got := ShouldTransferCacheTokens(tt.probability)
					if got != *tt.wantResult {
						t.Errorf("ShouldTransferCacheTokens(%v) = %v, want %v", tt.probability, got, *tt.wantResult)
					}
				}
			} else {
				// Random case - ensure both true and false can be returned
				trueCount := 0
				falseCount := 0
				iterations := 1000

				for i := 0; i < iterations; i++ {
					if ShouldTransferCacheTokens(tt.probability) {
						trueCount++
					} else {
						falseCount++
					}
				}

				// With probability 0.5 and 1000 iterations, we should see both true and false
				if trueCount == 0 {
					t.Errorf("ShouldTransferCacheTokens(%v) never returned true in %d iterations", tt.probability, iterations)
				}
				if falseCount == 0 {
					t.Errorf("ShouldTransferCacheTokens(%v) never returned false in %d iterations", tt.probability, iterations)
				}

				// Check that the ratio is roughly correct (within 10% tolerance)
				expectedRatio := tt.probability
				actualRatio := float64(trueCount) / float64(iterations)
				tolerance := 0.1
				if actualRatio < expectedRatio-tolerance || actualRatio > expectedRatio+tolerance {
					t.Errorf("ShouldTransferCacheTokens(%v) ratio %v is outside expected range [%v, %v]",
						tt.probability, actualRatio, expectedRatio-tolerance, expectedRatio+tolerance)
				}
			}
		})
	}
}

func TestShouldTransferCacheTokensProbabilityDistribution(t *testing.T) {
	// Test various probability values
	probabilities := []float64{0.1, 0.3, 0.7, 0.9}
	iterations := 10000 // More iterations for better statistical accuracy
	tolerance := 0.05   // 5% tolerance

	for _, p := range probabilities {
		t.Run("probability "+floatToString(p), func(t *testing.T) {
			trueCount := 0
			for i := 0; i < iterations; i++ {
				if ShouldTransferCacheTokens(p) {
					trueCount++
				}
			}

			actualRatio := float64(trueCount) / float64(iterations)
			if actualRatio < p-tolerance || actualRatio > p+tolerance {
				t.Errorf("ShouldTransferCacheTokens(%v) ratio %v is outside expected range [%v, %v]",
					p, actualRatio, p-tolerance, p+tolerance)
			}
		})
	}
}

func testBoolPtr(b bool) *bool {
	return &b
}

func floatToString(f float64) string {
	switch f {
	case 0.1:
		return "0.1"
	case 0.3:
		return "0.3"
	case 0.7:
		return "0.7"
	case 0.9:
		return "0.9"
	default:
		return "unknown"
	}
}
