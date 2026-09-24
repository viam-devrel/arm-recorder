package armrecorder

import (
	"math"

	"go.viam.com/rdk/referenceframe"
)

// clampFrame constrains joint positions to the arm's declared limits in place,
// reporting whether anything changed. A frame with no matching limits passes
// through unchanged — recording without limits beats not recording.
func clampFrame(frame []float64, limits []referenceframe.Limit) bool {
	if len(limits) != len(frame) {
		return false
	}
	changed := false
	for i, v := range frame {
		lo, hi := limits[i].Min, limits[i].Max
		if math.IsNaN(lo) || math.IsNaN(hi) {
			continue
		}
		c := math.Max(lo, math.Min(hi, v))
		changed = changed || c != v
		frame[i] = c
	}
	return changed
}
