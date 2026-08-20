package armrecorder

import (
	"fmt"
	"strings"
)

// armPositionSaverGoTo is the "go to" position of an arm-position-saver switch,
// whose positions are ["idle", "update config", "go to"].
const armPositionSaverGoTo = 2

// validateHomePose checks a configured home_pose switch name.
func validateHomePose(name, path string) error {
	// A padded name would never resolve as a dependency.
	if strings.TrimSpace(name) != name {
		return fmt.Errorf("%s: home_pose %q has surrounding whitespace", path, name)
	}
	return nil
}
