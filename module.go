package armrecorder

import (
	"context"
	"fmt"
	"os"
	"sync"

	"go.viam.com/rdk/components/arm"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

var Recorder = resource.NewModel("devrel", "arm-recorder", "recorder")

const defaultFrequencyHz = 10.0

const (
	stateIdle      = "idle"
	stateRecording = "recording"
	statePlaying   = "playing"
)

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

	logger  logging.Logger
	cfg     *Config
	arm     arm.Arm
	freqHz  float64
	dataDir string

	mu           sync.Mutex
	state        string
	session      string
	frames       [][]float64
	workerCancel context.CancelFunc
	workerDone   chan struct{}
}

func newArmRecorderRecorder(ctx context.Context, deps resource.Dependencies, rawConf resource.Config, logger logging.Logger) (sensor.Sensor, error) {
	conf, err := resource.NativeConfig[*Config](rawConf)
	if err != nil {
		return nil, err
	}
	a, err := arm.FromProvider(deps, conf.Arm)
	if err != nil {
		return nil, fmt.Errorf("could not get arm %q: %w", conf.Arm, err)
	}
	dataDir := resolveDataDir(logger)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("could not create data dir %q: %w", dataDir, err)
	}
	return &armRecorderRecorder{
		Named:   rawConf.ResourceName().AsNamed(),
		logger:  logger,
		cfg:     conf,
		arm:     a,
		freqHz:  conf.frequencyHz(),
		dataDir: dataDir,
		state:   stateIdle,
	}, nil
}

func (s *armRecorderRecorder) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	s.mu.Lock()
	state, session, frameCount := s.state, s.session, len(s.frames)
	s.mu.Unlock()

	out := map[string]interface{}{
		"state":       state,
		"session":     session,
		"frame_count": frameCount,
	}
	joints, err := s.arm.JointPositions(ctx, nil)
	if err != nil {
		s.logger.Warnf("could not read joint positions: %v", err)
		out["joints_error"] = err.Error()
		return out, nil
	}
	out["joints"] = joints
	out["joint_count"] = len(joints)
	return out, nil
}

func (s *armRecorderRecorder) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	return nil, fmt.Errorf("unimplemented")
}

func (s *armRecorderRecorder) Close(ctx context.Context) error {
	return nil
}
