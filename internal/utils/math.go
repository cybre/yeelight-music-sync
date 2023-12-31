package utils

import "golang.org/x/exp/constraints"

// Clamp constrains v to the range [minVal, maxVal].
func Clamp[T constraints.Ordered](v, minVal, maxVal T) T {
	if v < minVal {
		return minVal
	}
	if v > maxVal {
		return maxVal
	}
	return v
}

// SpectralBalance returns the normalized contribution of a within (a+b).
func SpectralBalance(a, b float64) float64 {
	total := a + b
	if total <= 1e-9 {
		return 0.5
	}
	return Clamp(a/total, 0.0, 1.0)
}

// ClampIndex bounds idx to the valid range for a slice of length.
func ClampIndex(idx, length int) int {
	if length <= 0 {
		return 0
	}
	if idx < 0 {
		return 0
	}
	if idx >= length {
		return length - 1
	}
	return idx
}
