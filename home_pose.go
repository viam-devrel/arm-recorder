package armrecorder

import (
	"encoding/json"
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

func (h *HomePose) UnmarshalJSON(b []byte) error {
	var name string
	if err := json.Unmarshal(b, &name); err == nil {
		h.Switch, h.Joints = name, nil
		return nil
	}
	var joints []float64
	if err := json.Unmarshal(b, &joints); err == nil {
		h.Switch, h.Joints = "", joints
		return nil
	}
	return fmt.Errorf(
		"home_pose must be either the name of a pose-saving switch (string) " +
			"or joint positions (array of numbers)")
}

func (h HomePose) MarshalJSON() ([]byte, error) {
	if h.Switch != "" {
		return json.Marshal(h.Switch)
	}
	return json.Marshal(h.Joints)
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
