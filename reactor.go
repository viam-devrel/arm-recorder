package armrecorder

import (
	"context"
	"fmt"
	"time"

	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/generic"
)

// Reactor plays a recorded session when a detector vision service reports a
// matching object label — vision-based feedback for manipulation tasks.
var Reactor = resource.NewModel("devrel", "arm-recorder", "reactor")

func init() {
	resource.RegisterService(generic.API, Reactor,
		resource.Registration[resource.Resource, *ReactorConfig]{
			Constructor: newReactor,
		},
	)
}

type ReactorConfig struct {
	VisionService  string            `json:"vision_service"`
	Camera         string            `json:"camera"`
	Recorder       string            `json:"recorder"`
	LabelSessions  map[string]string `json:"label_sessions"`
	PollIntervalMs int               `json:"poll_interval_ms"`
	MinConfidence  float64           `json:"min_confidence"`
	CooldownSec    float64           `json:"cooldown_sec"`
}

const (
	defaultPollIntervalMs = 500
	defaultMinConfidence  = 0.5
	defaultCooldownSec    = 5.0
)

func (cfg *ReactorConfig) Validate(path string) ([]string, []string, error) {
	if cfg.VisionService == "" {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "vision_service")
	}
	if cfg.Camera == "" {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "camera")
	}
	if cfg.Recorder == "" {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "recorder")
	}
	if len(cfg.LabelSessions) == 0 {
		return nil, nil, fmt.Errorf("%s: label_sessions must not be empty", path)
	}
	if cfg.MinConfidence < 0 || cfg.MinConfidence > 1 {
		return nil, nil, fmt.Errorf("%s: min_confidence must be within [0,1]", path)
	}
	if cfg.CooldownSec < 0 {
		return nil, nil, fmt.Errorf("%s: cooldown_sec must not be negative", path)
	}
	if cfg.PollIntervalMs < 0 {
		return nil, nil, fmt.Errorf("%s: poll_interval_ms must not be negative", path)
	}
	return []string{cfg.VisionService, cfg.Camera, cfg.Recorder}, nil, nil
}

func (cfg *ReactorConfig) pollInterval() time.Duration {
	ms := cfg.PollIntervalMs
	if ms <= 0 {
		ms = defaultPollIntervalMs
	}
	return time.Duration(ms) * time.Millisecond
}

func (cfg *ReactorConfig) minConfidence() float64 {
	if cfg.MinConfidence <= 0 {
		return defaultMinConfidence
	}
	return cfg.MinConfidence
}

func (cfg *ReactorConfig) cooldown() time.Duration {
	s := cfg.CooldownSec
	if s <= 0 {
		s = defaultCooldownSec
	}
	return time.Duration(s * float64(time.Second))
}

type reactor struct {
	resource.AlwaysRebuild
	resource.Named

	logger logging.Logger
}

func newReactor(ctx context.Context, deps resource.Dependencies, rawConf resource.Config, logger logging.Logger) (resource.Resource, error) {
	return &reactor{
		Named:  rawConf.ResourceName().AsNamed(),
		logger: logger,
	}, nil
}

func (r *reactor) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *reactor) Close(ctx context.Context) error {
	return nil
}
