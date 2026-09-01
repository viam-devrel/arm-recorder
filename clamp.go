package armrecorder

import (
	"math"

	"go.viam.com/rdk/referenceframe"
)

// clampFrame constrains joint positions to the arm's declared limits, reporting
// whether anything changed. A frame with no matching limits passes through
// unchanged — recording without limits beats not recording.
func clampFrame(frame []float64, limits []referenceframe.Limit) ([]float64, bool) {
	if len(limits) != len(frame) {
		return frame, false
	}
	out := make([]float64, len(frame))
	changed := false
	for i, v := range frame {
		lo, hi := limits[i].Min, limits[i].Max
		if math.IsNaN(lo) || math.IsNaN(hi) || lo > hi {
			out[i] = v
			continue
		}
		c := math.Max(lo, math.Min(hi, v))
		if c != v {
			changed = true
		}
		out[i] = c
	}
	return out, changed
}
