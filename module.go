package armrecorder

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

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

func argString(cmd map[string]interface{}, key string) (string, error) {
	v, ok := cmd[key]
	if !ok {
		return "", fmt.Errorf("missing required argument %q", key)
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("argument %q must be a non-empty string", key)
	}
	return s, nil
}

func (s *armRecorderRecorder) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	command, err := argString(cmd, "command")
	if err != nil {
		return nil, err
	}
	switch command {
	case "start_recording":
		return s.startRecording(cmd)
	case "stop_recording":
		return s.stopRecording()
	case "play":
		return s.play(cmd)
	case "stop_playback":
		return s.stopPlayback()
	case "list_sessions":
		names, err := listSessions(s.dataDir)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"sessions": names}, nil
	case "delete_session":
		session, err := argString(cmd, "session")
		if err != nil {
			return nil, err
		}
		if err := deleteSession(s.dataDir, session); err != nil {
			return nil, err
		}
		return map[string]interface{}{"status": "deleted", "session": session}, nil
	default:
		return nil, fmt.Errorf("unknown command %q", command)
	}
}

func (s *armRecorderRecorder) startRecording(cmd map[string]interface{}) (map[string]interface{}, error) {
	session, err := argString(cmd, "session")
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != stateIdle {
		return nil, fmt.Errorf("cannot start recording while %s", s.state)
	}
	wctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.state = stateRecording
	s.session = session
	s.frames = nil
	s.workerCancel = cancel
	s.workerDone = done
	go s.recordLoop(wctx, done)
	s.logger.Infof("started recording session %q at %.2f Hz", session, s.freqHz)
	return map[string]interface{}{"status": "recording", "session": session}, nil
}

func (s *armRecorderRecorder) recordLoop(ctx context.Context, done chan struct{}) {
	defer close(done)
	interval := time.Duration(float64(time.Second) / s.freqHz)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			joints, err := s.arm.JointPositions(ctx, nil)
			if err != nil {
				s.logger.Errorf("recording stopped: joint read failed: %v", err)
				return
			}
			frame := make([]float64, len(joints))
			copy(frame, joints)
			s.mu.Lock()
			s.frames = append(s.frames, frame)
			s.mu.Unlock()
		}
	}
}

func (s *armRecorderRecorder) stopRecording() (map[string]interface{}, error) {
	s.mu.Lock()
	if s.state != stateRecording {
		s.mu.Unlock()
		return nil, fmt.Errorf("not recording (state is %s)", s.state)
	}
	cancel, done := s.workerCancel, s.workerDone
	s.mu.Unlock()

	cancel()
	<-done

	s.mu.Lock()
	defer s.mu.Unlock()
	jointCount := 0
	if len(s.frames) > 0 {
		jointCount = len(s.frames[0])
	}
	sess := &Session{
		Name:        s.session,
		FrequencyHz: s.freqHz,
		JointCount:  jointCount,
		Frames:      s.frames,
	}
	if err := saveSession(s.dataDir, sess); err != nil {
		return nil, fmt.Errorf("could not save session: %w", err)
	}
	count := len(s.frames)
	name := s.session
	s.state = stateIdle
	s.workerCancel = nil
	s.workerDone = nil
	s.logger.Infof("saved session %q with %d frames", name, count)
	return map[string]interface{}{"status": "saved", "session": name, "frame_count": count}, nil
}

func (s *armRecorderRecorder) play(cmd map[string]interface{}) (map[string]interface{}, error) {
	session, err := argString(cmd, "session")
	if err != nil {
		return nil, err
	}
	sess, err := loadSession(s.dataDir, session)
	if err != nil {
		return nil, err
	}
	if len(sess.Frames) == 0 {
		return nil, fmt.Errorf("session %q has no frames", session)
	}

	// Validate joint count against the live arm before moving anything.
	current, err := s.arm.JointPositions(context.Background(), nil)
	if err != nil {
		return nil, fmt.Errorf("could not read arm joints: %w", err)
	}
	if len(current) != len(sess.Frames[0]) {
		return nil, fmt.Errorf("joint count mismatch: arm has %d, session has %d", len(current), len(sess.Frames[0]))
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != stateIdle {
		return nil, fmt.Errorf("cannot play while %s", s.state)
	}
	wctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.state = statePlaying
	s.session = session
	s.workerCancel = cancel
	s.workerDone = done
	go s.playLoop(wctx, done, sess)
	s.logger.Infof("started playback of session %q (%d frames)", session, len(sess.Frames))
	return map[string]interface{}{"status": "playing", "session": session, "frame_count": len(sess.Frames)}, nil
}

func (s *armRecorderRecorder) playLoop(ctx context.Context, done chan struct{}, sess *Session) {
	defer close(done)
	defer func() {
		s.mu.Lock()
		s.state = stateIdle
		s.workerCancel = nil
		s.workerDone = nil
		s.mu.Unlock()
	}()

	// Safe entry: move to the first frame and block until reached.
	if err := s.arm.MoveToJointPositions(ctx, sess.Frames[0], nil); err != nil {
		if ctx.Err() == nil {
			s.logger.Errorf("playback aborted moving to first frame: %v", err)
		}
		return
	}

	interval := time.Duration(float64(time.Second) / sess.FrequencyHz)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for i := 1; i < len(sess.Frames); i++ {
		select {
		case <-ctx.Done():
			_ = s.arm.Stop(context.Background(), nil)
			return
		case <-ticker.C:
			if err := s.arm.MoveToJointPositions(ctx, sess.Frames[i], nil); err != nil {
				if ctx.Err() == nil {
					s.logger.Errorf("playback aborted at frame %d: %v", i, err)
				}
				return
			}
		}
	}
	s.logger.Infof("playback of session %q complete", sess.Name)
}

func (s *armRecorderRecorder) stopPlayback() (map[string]interface{}, error) {
	s.mu.Lock()
	if s.state != statePlaying {
		s.mu.Unlock()
		return nil, fmt.Errorf("not playing (state is %s)", s.state)
	}
	cancel, done := s.workerCancel, s.workerDone
	s.mu.Unlock()

	cancel()
	<-done
	_ = s.arm.Stop(context.Background(), nil)
	return map[string]interface{}{"status": "stopped"}, nil
}

func (s *armRecorderRecorder) Close(ctx context.Context) error {
	s.mu.Lock()
	cancel, done := s.workerCancel, s.workerDone
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
		_ = s.arm.Stop(ctx, nil)
	}
	return nil
}
