package armrecorder

import (
	"math"

	"go.viam.com/rdk/referenceframe"
)

// clampFrame constrains joint positions to the arm's declared limits, returning
// the clamped frame and whether anything changed.
//
// The arm's calibrated range can extend slightly past the limits its kinematic
// model declares, so a pose reached by hand-guiding is not necessarily one the
// arm can be commanded back to: RDK's arm client validates every waypoint
// against the model and refuses the whole move. Clamping at capture keeps
// playback a faithful replay of what was stored, rather than a transformation
// applied on the way out.
//
// A frame with no matching limits is returned unchanged — recording without
// limits is better than not recording.
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
