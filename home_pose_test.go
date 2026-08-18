package armrecorder

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestHomePoseUnmarshal(t *testing.T) {
	t.Run("string names a pose-saving switch", func(t *testing.T) {
		var h HomePose
		if err := json.Unmarshal([]byte(`"cam-pose"`), &h); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if h.Switch != "cam-pose" || h.Joints != nil {
			t.Fatalf("expected a switch name, got %+v", h)
		}
		if !h.usesSwitch() {
			t.Fatal("expected usesSwitch to be true")
		}
	})

	t.Run("array is literal joint positions", func(t *testing.T) {
		var h HomePose
		if err := json.Unmarshal([]byte(`[-0.48, -0.16, 0.32]`), &h); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if h.Switch != "" || len(h.Joints) != 3 || h.Joints[0] != -0.48 {
			t.Fatalf("expected literal joints, got %+v", h)
		}
		if h.usesSwitch() {
			t.Fatal("expected usesSwitch to be false")
		}
	})

	t.Run("other forms are rejected with a usable message", func(t *testing.T) {
		for _, bad := range []string{`42`, `{"pose":"x"}`, `true`, `["a","b"]`} {
			var h HomePose
			err := json.Unmarshal([]byte(bad), &h)
			if err == nil {
				t.Fatalf("expected %s to be rejected", bad)
			}
			if !strings.Contains(err.Error(), "home_pose") {
				t.Fatalf("error should mention home_pose, got %v", err)
			}
		}
	})

	t.Run("round-trips through marshal", func(t *testing.T) {
		for _, in := range []string{`"cam-pose"`, `[1,2,3]`} {
			var h HomePose
			if err := json.Unmarshal([]byte(in), &h); err != nil {
				t.Fatal(err)
			}
			out, err := json.Marshal(h)
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != strings.ReplaceAll(in, " ", "") {
				t.Fatalf("round-trip changed %s into %s", in, out)
			}
		}
	})
}

func TestHomePoseValidate(t *testing.T) {
	t.Run("switch form", func(t *testing.T) {
		if err := (&HomePose{Switch: "cam-pose"}).validate("p"); err != nil {
			t.Fatalf("valid switch name rejected: %v", err)
		}
		if err := (&HomePose{Switch: " cam-pose "}).validate("p"); err == nil {
			t.Fatal("a padded switch name would never resolve as a dependency; expected an error")
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
	// erh:vmodutils:arm-position-saver reports its positions as
	// ["idle", "update config", "go to"], so "go to" is index 2. If that ever
	// changes upstream, returning home would silently re-save the pose instead
	// of replaying it — which is why this constant is asserted rather than
	// inlined at the call site.
	if armPositionSaverGoTo != 2 {
		t.Fatalf("expected the go-to position to be 2, got %d", armPositionSaverGoTo)
	}
}
