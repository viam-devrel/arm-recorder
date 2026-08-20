package armrecorder

import (
	"testing"

	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/utils"
)

func TestConfigValidate(t *testing.T) {
	t.Run("missing arm is an error", func(t *testing.T) {
		_, _, err := (&Config{}).Validate("components.0")
		if err == nil {
			t.Fatal("expected error for missing arm")
		}
	})

	t.Run("arm returned as required dependency", func(t *testing.T) {
		req, opt, err := (&Config{Arm: "my_arm"}).Validate("components.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(req) != 1 || req[0] != "my_arm" {
			t.Fatalf("expected [my_arm], got %v", req)
		}
		if len(opt) != 0 {
			t.Fatalf("expected no optional deps, got %v", opt)
		}
	})
}

func TestFrequencyDefault(t *testing.T) {
	if got := (&Config{}).frequencyHz(); got != defaultFrequencyHz {
		t.Fatalf("expected default %v, got %v", defaultFrequencyHz, got)
	}
	if got := (&Config{FrequencyHz: 25}).frequencyHz(); got != 25 {
		t.Fatalf("expected 25, got %v", got)
	}
}

func TestGripperConfig(t *testing.T) {
	t.Run("gripper added to required deps when set", func(t *testing.T) {
		req, _, err := (&Config{Arm: "my_arm", Gripper: "my_gripper"}).Validate("components.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(req) != 2 {
			t.Fatalf("expected 2 required deps, got %v", req)
		}
		found := false
		for _, r := range req {
			if r == "my_gripper" {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected my_gripper in required deps, got %v", req)
		}
	})

	t.Run("gripper absent from deps when not set", func(t *testing.T) {
		req, _, err := (&Config{Arm: "my_arm"}).Validate("components.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(req) != 1 {
			t.Fatalf("expected 1 required dep, got %v", req)
		}
	})

	t.Run("negative velocity rejected", func(t *testing.T) {
		_, _, err := (&Config{Arm: "my_arm", MaxVelocityRadsPerSec: -1}).Validate("components.0")
		if err == nil {
			t.Fatal("expected error for negative velocity")
		}
	})

	t.Run("negative acceleration rejected", func(t *testing.T) {
		_, _, err := (&Config{Arm: "my_arm", MaxAccelerationRadsPerSec: -1}).Validate("components.0")
		if err == nil {
			t.Fatal("expected error for negative acceleration")
		}
	})

	t.Run("default gripper position key", func(t *testing.T) {
		cfg := &Config{Arm: "my_arm"}
		if got := cfg.gripperPositionKey(); got != "position" {
			t.Fatalf("expected default key %q, got %q", "position", got)
		}
	})

	t.Run("custom gripper position key", func(t *testing.T) {
		cfg := &Config{Arm: "my_arm", GripperPositionKey: "my_key"}
		if got := cfg.gripperPositionKey(); got != "my_key" {
			t.Fatalf("expected key %q, got %q", "my_key", got)
		}
	})
}

// homePoseFromAttributes decodes through the same path viam-server uses:
// resource.TransformAttributeMap, which is mapstructure with TagName "json" and
// no decode hooks. Testing json.Unmarshal instead is what let a config that
// viam-server rejects pass a full green test suite.
func homePoseFromAttributes(t *testing.T, raw interface{}) (*Config, error) {
	t.Helper()
	attrs := utils.AttributeMap{"arm": "a"}
	if raw != nil {
		attrs["home_pose"] = raw
	}
	return resource.TransformAttributeMap[*Config](attrs)
}

func TestRecorderConfigHomePose(t *testing.T) {
	t.Run("a switch name decodes and becomes a dependency", func(t *testing.T) {
		cfg, err := homePoseFromAttributes(t, "cam-pose")
		if err != nil {
			t.Fatalf("viam-server would reject this config: %v", err)
		}
		if cfg.HomePose != "cam-pose" {
			t.Fatalf("expected cam-pose, got %q", cfg.HomePose)
		}
		deps, _, err := cfg.Validate("p")
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		found := false
		for _, d := range deps {
			if d == "cam-pose" {
				found = true
			}
		}
		if !found {
			t.Fatalf("switch must be a dependency, got %v", deps)
		}
	})

	t.Run("home_pose is optional", func(t *testing.T) {
		cfg, err := homePoseFromAttributes(t, nil)
		if err != nil {
			t.Fatal(err)
		}
		deps, _, err := cfg.Validate("p")
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		if len(deps) != 1 {
			t.Fatalf("expected only the arm, got %v", deps)
		}
	})

	t.Run("a padded name is rejected", func(t *testing.T) {
		cfg, err := homePoseFromAttributes(t, " cam-pose ")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := cfg.Validate("p"); err == nil {
			t.Fatal("a padded name would never resolve as a dependency")
		}
	})

	t.Run("non-string forms are rejected at decode", func(t *testing.T) {
		// A plain string field is a type mapstructure handles natively, so these
		// fail in the decoder rather than needing custom parsing.
		for _, bad := range []interface{}{
			[]interface{}{-0.48, -0.16},
			map[string]interface{}{"pose": "x"},
		} {
			if _, err := homePoseFromAttributes(t, bad); err == nil {
				t.Fatalf("expected %v (%T) to be rejected", bad, bad)
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
