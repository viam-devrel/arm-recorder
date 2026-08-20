package armrecorder

import (
	"fmt"
	"math"
	"strings"
)

// armPositionSaverGoTo is the "go to" position of an arm-position-saver switch,
// whose positions are ["idle", "update config", "go to"].
const armPositionSaverGoTo = 2

// HomePose is where the arm returns after a playback completes: either the name
// of a switch component whose "go to" position replays a saved pose, or literal
// joint positions.
type HomePose struct {
	Switch string
	Joints []float64
}

// parseHomePose reads the raw home_pose attribute.
//
// It cannot be a typed struct field with an UnmarshalJSON: viam-server decodes
// attributes with mapstructure (TagName "json", no decode hooks), which never
// consults json.Unmarshaler and rejects a bare string with "expected a map or
// struct". Numbers arrive as float64 from the protobuf struct.
func parseHomePose(raw interface{}) (*HomePose, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case string:
		return &HomePose{Switch: v}, nil
	case []float64:
		return &HomePose{Joints: v}, nil
	case []interface{}:
		joints := make([]float64, 0, len(v))
		for i, e := range v {
			f, ok := toFloat(e)
			if !ok {
				return nil, fmt.Errorf("home_pose joint %d is not a number", i)
			}
			joints = append(joints, f)
		}
		return &HomePose{Joints: joints}, nil
	default:
		return nil, fmt.Errorf(
			"home_pose must be either the name of a pose-saving switch (string) "+
				"or joint positions (array of numbers), got %T", raw)
	}
}

func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

func (h *HomePose) usesSwitch() bool {
	return h.Switch != ""
}

func (h *HomePose) validate(path string) error {
	if h.usesSwitch() {
		// A padded name would never resolve as a dependency.
		if strings.TrimSpace(h.Switch) != h.Switch {
			return fmt.Errorf("%s: home_pose switch name %q has surrounding whitespace", path, h.Switch)
		}
		return nil
	}
	if len(h.Joints) == 0 {
		return fmt.Errorf("%s: home_pose must name a pose-saving switch or list joint positions", path)
	}
	for i, j := range h.Joints {
		if math.IsNaN(j) || math.IsInf(j, 0) {
			return fmt.Errorf("%s: home_pose joint %d is not a finite number", path, i)
		}
	}
	return nil
}
