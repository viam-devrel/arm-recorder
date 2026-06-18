package armrecorder

import (
	"context"
	"fmt"

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

func (cfg *ReactorConfig) Validate(path string) ([]string, []string, error) {
	return nil, nil, nil
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
