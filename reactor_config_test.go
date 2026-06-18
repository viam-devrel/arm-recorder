package armrecorder

import "testing"

func TestReactorConfigValidate(t *testing.T) {
	base := func() *ReactorConfig {
		return &ReactorConfig{
			VisionService: "vis", Camera: "cam", Recorder: "rec",
			LabelSessions: map[string]string{"cup": "grab-cup"},
		}
	}

	t.Run("valid returns three required deps", func(t *testing.T) {
		req, opt, err := base().Validate("services.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(req) != 3 {
			t.Fatalf("expected 3 deps, got %v", req)
		}
		if len(opt) != 0 {
			t.Fatalf("expected no optional deps, got %v", opt)
		}
	})

	t.Run("missing fields error", func(t *testing.T) {
		for _, mut := range []func(*ReactorConfig){
			func(c *ReactorConfig) { c.VisionService = "" },
			func(c *ReactorConfig) { c.Camera = "" },
			func(c *ReactorConfig) { c.Recorder = "" },
			func(c *ReactorConfig) { c.LabelSessions = nil },
		} {
			c := base()
			mut(c)
			if _, _, err := c.Validate("services.0"); err == nil {
				t.Fatalf("expected error for %+v", c)
			}
		}
	})

	t.Run("negative tuning fields error", func(t *testing.T) {
		c := base()
		c.CooldownSec = -1
		if _, _, err := c.Validate("services.0"); err == nil {
			t.Fatal("expected error for negative cooldown")
		}
	})
}

func TestReactorConfigDefaults(t *testing.T) {
	c := &ReactorConfig{}
	if c.pollInterval().Milliseconds() != defaultPollIntervalMs {
		t.Fatalf("poll default wrong: %v", c.pollInterval())
	}
	if c.minConfidence() != defaultMinConfidence {
		t.Fatalf("confidence default wrong: %v", c.minConfidence())
	}
	if c.cooldown().Seconds() != defaultCooldownSec {
		t.Fatalf("cooldown default wrong: %v", c.cooldown())
	}
}
