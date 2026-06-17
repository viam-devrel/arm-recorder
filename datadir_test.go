package armrecorder

import (
	"testing"

	"go.viam.com/rdk/logging"
)

func TestResolveDataDirFromEnv(t *testing.T) {
	want := t.TempDir()
	t.Setenv("VIAM_MODULE_DATA", want)
	got := resolveDataDir(logging.NewTestLogger(t))
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestResolveDataDirFallback(t *testing.T) {
	t.Setenv("VIAM_MODULE_DATA", "")
	got := resolveDataDir(logging.NewTestLogger(t))
	if got == "" {
		t.Fatal("expected a non-empty fallback dir")
	}
}
