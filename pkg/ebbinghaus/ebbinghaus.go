// Package ebbinghaus implements the Ebbinghaus retention curve used by
// reverie's decay system. The math is pure — no state, no configuration,
// no project-internal dependencies — so it lives as a leaf package
// reachable from both internal/decay (which adds cluster-aware wrappers
// and config) and internal/memory (which recomputes retention on entity
// tick paths). Keeping the formula in one place prevents drift between
// the decay engine and store-layer entity decay.
package ebbinghaus

import "math"

const (
	// DefaultTemperature is the global temperature T used when no
	// configuration value is provided (or the configured value is <= 0).
	// Higher values slow decay; lower values accelerate it.
	DefaultTemperature = 10.0

	// Epsilon is a small additive constant that prevents stability from
	// reaching zero when both utility and frequency are zero. Without it,
	// a brand-new cluster with U=F=0 would have stability=0, making the
	// retention formula undefined (division by zero).
	Epsilon = 0.01
)

// Stability computes S = (U + F + epsilon) * T.
// This is the denominator scaling factor in the Ebbinghaus retention
// curve. Exported separately for debuggability and testing.
func Stability(utility, frequency, temperature float64) float64 {
	return (utility + frequency + Epsilon) * temperature
}

// Retention computes R = exp(-n / S) where S = (U + F + epsilon) * T.
//
// Parameters:
//   - turnsSince:  n — the number of turns since the item was last accessed.
//   - utility:     U — the item's utility score, typically in [0,1].
//   - frequency:   F — the item's frequency score, typically in [0,1].
//   - temperature: T — the global decay temperature (higher = slower decay).
//
// Returns 0 if stability is non-positive (guards against divide-by-zero
// when temperature <= 0). Returns a value in (0, 1] otherwise.
func Retention(turnsSince int, utility, frequency, temperature float64) float64 {
	stability := Stability(utility, frequency, temperature)
	if stability <= 0 {
		return 0
	}
	return math.Exp(-float64(turnsSince) / stability)
}
