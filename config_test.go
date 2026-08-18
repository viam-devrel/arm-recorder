package armrecorder

import (
	"encoding/json"
	"math"
	"testing"
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

func TestRecorderConfigHomePose(t *testing.T) {
	base := func() *Config { return &Config{Arm: "a"} }

	t.Run("home_pose is optional", func(t *testing.T) {
		if _, _, err := base().Validate("p"); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("switch form is accepted and becomes a dependency", func(t *testing.T) {
		cfg := base()
		cfg.HomePose = &HomePose{Switch: "cam-pose"}
		deps, _, err := cfg.Validate("p")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		found := false
		for _, d := range deps {
			if d == "cam-pose" {
				found = true
			}
		}
		if !found {
			t.Fatalf("a switch-form home_pose must be declared as a dependency, got %v", deps)
		}
	})

	t.Run("literal form is accepted and adds no dependency", func(t *testing.T) {
		cfg := base()
		cfg.HomePose = &HomePose{Joints: []float64{0, 1, 2}}
		deps, _, err := cfg.Validate("p")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(deps) != 1 || deps[0] != "a" {
			t.Fatalf("expected only the arm, got %v", deps)
		}
	})

	t.Run("empty home_pose is rejected", func(t *testing.T) {
		cfg := base()
		cfg.HomePose = &HomePose{}
		if _, _, err := cfg.Validate("p"); err == nil {
			t.Fatal("expected an error for an empty home_pose")
		}
	})

	t.Run("non-finite joints are rejected", func(t *testing.T) {
		cfg := base()
		cfg.HomePose = &HomePose{Joints: []float64{math.NaN()}}
		if _, _, err := cfg.Validate("p"); err == nil {
			t.Fatal("expected an error for NaN joints")
		}
	})

	t.Run("parses from a full config document", func(t *testing.T) {
		for raw, wantSwitch := range map[string]string{
			`{"arm":"a","home_pose":"cam-pose"}`:         "cam-pose",
			`{"arm":"a","home_pose":[-0.48,-0.16,0.32]}`: "",
		} {
			var cfg Config
			if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
				t.Fatalf("unmarshal %s: %v", raw, err)
			}
			if cfg.HomePose == nil {
				t.Fatalf("home_pose not parsed from %s", raw)
			}
			if cfg.HomePose.Switch != wantSwitch {
				t.Fatalf("%s: expected switch %q, got %q", raw, wantSwitch, cfg.HomePose.Switch)
			}
			if _, _, err := cfg.Validate("p"); err != nil {
				t.Fatalf("validate %s: %v", raw, err)
			}
		}
	})
}
