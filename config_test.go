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
