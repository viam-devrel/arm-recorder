package armrecorder

import (
	"math"
	"strings"
	"testing"
)

func TestParseHomePose(t *testing.T) {
	t.Run("string is a switch name", func(t *testing.T) {
		h, err := parseHomePose("cam-pose")
		if err != nil || h.Switch != "cam-pose" || h.Joints != nil {
			t.Fatalf("got %+v err=%v", h, err)
		}
		if !h.usesSwitch() {
			t.Fatal("expected usesSwitch to be true")
		}
	})

	t.Run("float64 slice from a protobuf struct", func(t *testing.T) {
		h, err := parseHomePose([]interface{}{-0.48, -0.16, 0.32})
		if err != nil || len(h.Joints) != 3 || h.Joints[0] != -0.48 {
			t.Fatalf("got %+v err=%v", h, err)
		}
		if h.usesSwitch() {
			t.Fatal("expected usesSwitch to be false")
		}
	})

	t.Run("integers are widened", func(t *testing.T) {
		h, err := parseHomePose([]interface{}{0, 1, 2})
		if err != nil || len(h.Joints) != 3 || h.Joints[2] != 2 {
			t.Fatalf("got %+v err=%v", h, err)
		}
	})

	t.Run("native float64 slice", func(t *testing.T) {
		h, err := parseHomePose([]float64{1, 2})
		if err != nil || len(h.Joints) != 2 {
			t.Fatalf("got %+v err=%v", h, err)
		}
	})

	t.Run("nil is absent, not an error", func(t *testing.T) {
		h, err := parseHomePose(nil)
		if err != nil || h != nil {
			t.Fatalf("got %+v err=%v", h, err)
		}
	})

	t.Run("other forms are rejected, naming the attribute", func(t *testing.T) {
		for _, bad := range []interface{}{
			float64(42), true, map[string]interface{}{"pose": "x"},
		} {
			_, err := parseHomePose(bad)
			if err == nil {
				t.Fatalf("expected %v (%T) to be rejected", bad, bad)
			}
			if !strings.Contains(err.Error(), "home_pose") {
				t.Fatalf("error should mention home_pose, got %v", err)
			}
		}
	})

	t.Run("a non-numeric element names its index", func(t *testing.T) {
		_, err := parseHomePose([]interface{}{0.1, "elbow", 0.3})
		if err == nil || !strings.Contains(err.Error(), "joint 1") {
			t.Fatalf("expected the offending index in the error, got %v", err)
		}
	})
}

func TestHomePoseValidate(t *testing.T) {
	t.Run("switch form", func(t *testing.T) {
		if err := (&HomePose{Switch: "cam-pose"}).validate("p"); err != nil {
			t.Fatalf("valid switch name rejected: %v", err)
		}
		if err := (&HomePose{Switch: " cam-pose "}).validate("p"); err == nil {
			t.Fatal("a padded name would never resolve as a dependency; expected an error")
		}
	})

	t.Run("literal form", func(t *testing.T) {
		if err := (&HomePose{Joints: []float64{0, 1}}).validate("p"); err != nil {
			t.Fatalf("valid joints rejected: %v", err)
		}
		if err := (&HomePose{}).validate("p"); err == nil {
			t.Fatal("an empty home_pose must be rejected")
		}
		for _, bad := range [][]float64{{math.NaN()}, {math.Inf(1)}, {0, math.Inf(-1)}} {
			if err := (&HomePose{Joints: bad}).validate("p"); err == nil {
				t.Fatalf("expected %v to be rejected", bad)
			}
		}
	})
}

func TestArmPositionSaverGoToPosition(t *testing.T) {
	// arm-position-saver reports ["idle", "update config", "go to"], so "go to"
	// is index 2. If that drifts upstream, returning home would drive the switch
	// to "update config" and overwrite the saved pose instead of replaying it.
	if armPositionSaverGoTo != 2 {
		t.Fatalf("expected the go-to position to be 2, got %d", armPositionSaverGoTo)
	}
}
