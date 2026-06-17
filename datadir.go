package armrecorder

import (
	"os"
	"path/filepath"

	"go.viam.com/rdk/logging"
)

// resolveDataDir returns the directory where session files live.
// Prefers VIAM_MODULE_DATA; falls back to a temp dir for dev boxes.
func resolveDataDir(logger logging.Logger) string {
	if dir := os.Getenv("VIAM_MODULE_DATA"); dir != "" {
		return dir
	}
	fallback := filepath.Join(os.TempDir(), "arm-recorder-sessions")
	logger.Warnf("VIAM_MODULE_DATA not set; falling back to %s", fallback)
	return fallback
}
