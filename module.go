package armrecorder

import (
	"context"
	"errors"
	"fmt"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

var (
	Recorder         = resource.NewModel("devrel", "arm-recorder", "recorder")
	errUnimplemented = errors.New("unimplemented")
)

const defaultFrequencyHz = 10.0

func init() {
	resource.RegisterComponent(sensor.API, Recorder,
		resource.Registration[sensor.Sensor, *Config]{
			Constructor: newArmRecorderRecorder,
		},
	)
}

type Config struct {
	Arm         string  `json:"arm"`
	FrequencyHz float64 `json:"frequency_hz"`
}

func (cfg *Config) Validate(path string) ([]string, []string, error) {
	if cfg.Arm == "" {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "arm")
	}
	if cfg.FrequencyHz < 0 {
		return nil, nil, fmt.Errorf("%s: frequency_hz must not be negative", path)
	}
	return []string{cfg.Arm}, nil, nil
}

func (cfg *Config) frequencyHz() float64 {
	if cfg.FrequencyHz <= 0 {
		return defaultFrequencyHz
	}
	return cfg.FrequencyHz
}

type armRecorderRecorder struct {
	resource.AlwaysRebuild
	resource.Named

	logger logging.Logger
	cfg    *Config
}

func newArmRecorderRecorder(ctx context.Context, deps resource.Dependencies, rawConf resource.Config, logger logging.Logger) (sensor.Sensor, error) {
	conf, err := resource.NativeConfig[*Config](rawConf)
	if err != nil {
		return nil, err
	}
	return &armRecorderRecorder{
		Named:  rawConf.ResourceName().AsNamed(),
		logger: logger,
		cfg:    conf,
	}, nil
}

func (s *armRecorderRecorder) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	return nil, errUnimplemented
}

func (s *armRecorderRecorder) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	return nil, errUnimplemented
}

func (s *armRecorderRecorder) Close(ctx context.Context) error {
	return nil
}

