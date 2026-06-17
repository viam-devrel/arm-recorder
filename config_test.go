package armrecorder

import "testing"

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
