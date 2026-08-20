package armrecorder

import (
	"context"
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"go.viam.com/rdk/components/arm"
	"go.viam.com/rdk/components/gripper"
	"go.viam.com/rdk/components/sensor"
	toggleswitch "go.viam.com/rdk/components/switch"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

var Recorder = resource.NewModel("devrel", "arm-recorder", "recorder")

const defaultFrequencyHz = 10.0
const defaultInterpolationSteps = 7

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
	Arm                        string  `json:"arm"`
	FrequencyHz                float64 `json:"frequency_hz"`
	Gripper                    string  `json:"gripper,omitempty"`
	GripperPositionKey         string  `json:"gripper_position_key,omitempty"`
	MaxVelocityRadsPerSec      float64 `json:"max_velocity_rads_per_sec,omitempty"`
	MaxAccelerationRadsPerSec  float64 `json:"max_acceleration_rads_per_sec,omitempty"`
	PlaybackInterpolationSteps *int    `json:"playback_interpolation_steps,omitempty"`
	HomePose                   string  `json:"home_pose,omitempty"`
}

func (cfg *Config) Validate(path string) ([]string, []string, error) {
	if cfg.Arm == "" {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "arm")
	}
	if cfg.FrequencyHz < 0 {
		return nil, nil, fmt.Errorf("%s: frequency_hz must not be negative", path)
	}
	if cfg.MaxVelocityRadsPerSec < 0 {
		return nil, nil, fmt.Errorf("%s: max_velocity_rads_per_sec must not be negative", path)
	}
	if cfg.MaxAccelerationRadsPerSec < 0 {
		return nil, nil, fmt.Errorf("%s: max_acceleration_rads_per_sec must not be negative", path)
	}
	if cfg.PlaybackInterpolationSteps != nil && *cfg.PlaybackInterpolationSteps < 0 {
		return nil, nil, fmt.Errorf("%s: playback_interpolation_steps must not be negative", path)
	}
	deps := []string{cfg.Arm}
	if cfg.HomePose != "" {
		if err := validateHomePose(cfg.HomePose, path); err != nil {
			return nil, nil, err
		}
		deps = append(deps, cfg.HomePose)
	}
	if cfg.Gripper != "" {
		deps = append(deps, cfg.Gripper)
	}
	return deps, nil, nil
}

func (cfg *Config) frequencyHz() float64 {
	if cfg.FrequencyHz <= 0 {
		return defaultFrequencyHz
	}
	return cfg.FrequencyHz
}

func (cfg *Config) gripperPositionKey() string {
	if cfg.GripperPositionKey == "" {
		return "position"
	}
	return cfg.GripperPositionKey
}

func (cfg *Config) interpolationSteps() int {
	if cfg.PlaybackInterpolationSteps == nil {
		return defaultInterpolationSteps
	}
	return *cfg.PlaybackInterpolationSteps
}

// interpolateFrames inserts `steps` linearly-interpolated waypoints between each
// consecutive pair of frames. steps<=0 (or <2 frames) returns frames unchanged.
func interpolateFrames(frames [][]float64, steps int) [][]float64 {
	if steps <= 0 || len(frames) < 2 {
		return frames
	}
	out := make([][]float64, 0, (len(frames)-1)*(steps+1)+1)
	for i := 0; i < len(frames)-1; i++ {
		from, to := frames[i], frames[i+1]
		out = append(out, from)
		for s := 1; s <= steps; s++ {
			t := float64(s) / float64(steps+1)
			pt := make([]float64, len(from))
			for j := range from {
				pt[j] = from[j] + (to[j]-from[j])*t
			}
			out = append(out, pt)
		}
	}
	return append(out, frames[len(frames)-1])
}

type armRecorderRecorder struct {
	resource.AlwaysRebuild
	resource.Named

	logger  logging.Logger
	cfg     *Config
	arm     arm.Arm
	freqHz  float64
	dataDir string

	gripper     gripper.Gripper
	gripperKey  string
	homeSwitch  toggleswitch.Switch
	moveOpts    *arm.MoveOptions
	interpSteps int

	mu               sync.Mutex
	state            string
	session          string
	frames           [][]float64
	gripperPositions []float64
	workerCancel     context.CancelFunc
	workerDone       chan struct{}
	lastError        string
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

	rec := &armRecorderRecorder{
		Named:   rawConf.ResourceName().AsNamed(),
		logger:  logger,
		cfg:     conf,
		arm:     a,
		freqHz:  conf.frequencyHz(),
		dataDir: dataDir,
		state:   stateIdle,
	}

	if conf.Gripper != "" {
		g, err := gripper.FromProvider(deps, conf.Gripper)
		if err != nil {
			return nil, fmt.Errorf("could not get gripper %q: %w", conf.Gripper, err)
		}
		rec.gripper = g
		rec.gripperKey = conf.gripperPositionKey()
	}

	if conf.MaxVelocityRadsPerSec > 0 || conf.MaxAccelerationRadsPerSec > 0 {
		rec.moveOpts = &arm.MoveOptions{
			MaxVelRads: conf.MaxVelocityRadsPerSec,
			MaxAccRads: conf.MaxAccelerationRadsPerSec,
		}
	}

	if conf.HomePose != "" {
		sw, err := toggleswitch.FromProvider(deps, conf.HomePose)
		if err != nil {
			return nil, fmt.Errorf("could not get home_pose switch %q: %w", conf.HomePose, err)
		}
		rec.homeSwitch = sw
	}

	rec.interpSteps = conf.interpolationSteps()

	return rec, nil
}

func (s *armRecorderRecorder) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	s.mu.Lock()
	state, session, frameCount, lastErr := s.state, s.session, len(s.frames), s.lastError
	hasGripper := s.gripper != nil
	s.mu.Unlock()

	out := map[string]interface{}{
		"state":       state,
		"session":     session,
		"frame_count": frameCount,
	}
	if lastErr != "" {
		out["last_error"] = lastErr
	}
	joints, err := s.arm.JointPositions(ctx, nil)
	if err != nil {
		s.logger.Warnf("could not read joint positions: %v", err)
		out["joints_error"] = err.Error()
		return out, nil
	}
	out["joints"] = toInterfaceSlice(joints)
	out["joint_count"] = len(joints)

	if hasGripper {
		out["has_gripper"] = true
		gripPos, err := s.readGripperPosition(ctx)
		if err != nil {
			s.logger.Warnf("could not read gripper position: %v", err)
			out["gripper_error"] = err.Error()
		} else {
			out["gripper_position"] = gripPos
		}
	}

	return out, nil
}

func toInterfaceSlice(f []float64) []interface{} {
	out := make([]interface{}, len(f))
	for i, v := range f {
		out[i] = v
	}
	return out
}

func toStringInterfaceSlice(s []string) []interface{} {
	out := make([]interface{}, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

func (s *armRecorderRecorder) readGripperPosition(ctx context.Context) (float64, error) {
	resp, err := s.gripper.DoCommand(ctx, map[string]interface{}{"command": "get_position"})
	if err != nil {
		return 0, err
	}
	v, ok := resp[s.gripperKey]
	if !ok {
		return 0, fmt.Errorf("get_position response missing key %q", s.gripperKey)
	}
	// DoCommand numeric values arrive as float64 via protobuf Struct encoding.
	f, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("gripper position %q is not a number (got %T)", s.gripperKey, v)
	}
	return f, nil
}

func (s *armRecorderRecorder) setGripperPosition(ctx context.Context, pos float64) error {
	_, err := s.gripper.DoCommand(ctx, map[string]interface{}{"command": "set_position", s.gripperKey: pos})
	return err
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
		return map[string]interface{}{"sessions": toStringInterfaceSlice(names)}, nil
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
	s.gripperPositions = nil
	s.lastError = ""
	s.workerCancel = cancel
	s.workerDone = done
	go s.recordLoop(wctx, done)
	s.logger.Infof("started recording session %q at %.2f Hz", session, s.freqHz)
	return map[string]interface{}{"status": "recording", "session": session}, nil
}

// failRecording sets the last error, resets state to idle, and clears worker fields under the lock.
// Callers should log the error before or after calling this.
func (s *armRecorderRecorder) failRecording(err error) {
	s.mu.Lock()
	s.lastError = err.Error()
	s.state = stateIdle
	s.workerCancel = nil
	s.workerDone = nil
	s.mu.Unlock()
}

// setLastError records a playback error in lastError so Readings can surface it.
// It only sets lastError; it does NOT touch state or worker fields — playLoop's
// deferred cleanup handles those.
func (s *armRecorderRecorder) setLastError(err error) {
	s.mu.Lock()
	s.lastError = err.Error()
	s.mu.Unlock()
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
				s.failRecording(err)
				return
			}
			frame := make([]float64, len(joints))
			copy(frame, joints)

			var gripPos float64
			if s.gripper != nil {
				gripPos, err = s.readGripperPosition(ctx)
				if err != nil {
					s.logger.Errorf("recording stopped: gripper read failed: %v", err)
					s.failRecording(err)
					return
				}
			}

			s.mu.Lock()
			s.frames = append(s.frames, frame)
			if s.gripper != nil {
				s.gripperPositions = append(s.gripperPositions, gripPos)
			}
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
		Name:               s.session,
		FrequencyHz:        s.freqHz,
		JointCount:         jointCount,
		Frames:             s.frames,
		HasGripper:         s.gripper != nil,
		GripperPositionKey: s.gripperKey,
		GripperPositions:   s.gripperPositions,
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

	// Guard against a bad FrequencyHz from the on-disk file.
	if sess.FrequencyHz <= 0 || math.IsNaN(sess.FrequencyHz) || math.IsInf(sess.FrequencyHz, 0) {
		s.logger.Warnf("session %q has invalid frequency_hz %v; using default %.2f Hz", session, sess.FrequencyHz, defaultFrequencyHz)
		sess.FrequencyHz = defaultFrequencyHz
	}

	// Validate joint count against the live arm before moving anything.
	current, err := s.arm.JointPositions(context.Background(), nil)
	if err != nil {
		return nil, fmt.Errorf("could not read arm joints: %w", err)
	}
	armJointCount := len(current)
	if armJointCount != len(sess.Frames[0]) {
		return nil, fmt.Errorf("joint count mismatch: arm has %d, session has %d", armJointCount, len(sess.Frames[0]))
	}
	// Validate every frame has the expected joint count.
	for i, frame := range sess.Frames {
		if len(frame) != armJointCount {
			return nil, fmt.Errorf("frame %d has %d joints, expected %d", i, len(frame), armJointCount)
		}
	}

	// Gripper validation.
	if sess.HasGripper && s.gripper == nil {
		return nil, fmt.Errorf("session %q has gripper data but no gripper is configured", session)
	}
	if sess.HasGripper && len(sess.GripperPositions) != len(sess.Frames) {
		return nil, fmt.Errorf("session %q gripper position count (%d) does not match frame count (%d)", session, len(sess.GripperPositions), len(sess.Frames))
	}
	useGripper := sess.HasGripper && s.gripper != nil
	if !sess.HasGripper && s.gripper != nil {
		s.logger.Infof("session %q has no gripper data; playing arm only (gripper configured but will not be moved)", session)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != stateIdle {
		return nil, fmt.Errorf("cannot play while %s", s.state)
	}
	s.lastError = ""
	wctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.state = statePlaying
	s.session = session
	s.workerCancel = cancel
	s.workerDone = done
	go s.playLoop(wctx, done, sess, useGripper)
	s.logger.Infof("started playback of session %q (%d frames)", session, len(sess.Frames))
	return map[string]interface{}{"status": "playing", "session": session, "frame_count": len(sess.Frames)}, nil
}

func (s *armRecorderRecorder) playLoop(ctx context.Context, done chan struct{}, sess *Session, useGripper bool) {
	defer close(done)
	defer func() {
		s.mu.Lock()
		s.state = stateIdle
		s.workerCancel = nil
		s.workerDone = nil
		s.mu.Unlock()
	}()

	// Guard against invalid frequency (belt-and-suspenders; play() already checked,
	// but the gripper ticker divides by FrequencyHz so guard here too).
	if sess.FrequencyHz <= 0 || math.IsNaN(sess.FrequencyHz) || math.IsInf(sess.FrequencyHz, 0) {
		sess.FrequencyHz = defaultFrequencyHz
	}

	// -------------------------------------------------------------------------
	// Safe entry: move arm to first frame, and (if useGripper) set gripper to
	// first position — run both concurrently, abort on the first error or if
	// the outer ctx is canceled.
	// -------------------------------------------------------------------------
	{
		entryCtx, entryCancel := context.WithCancel(ctx)
		defer entryCancel()

		var (
			wg       sync.WaitGroup
			firstErr error
			errMu    sync.Mutex
		)

		// Helper to record the first error and cancel the sibling goroutine.
		captureErr := func(err error) {
			errMu.Lock()
			if firstErr == nil {
				firstErr = err
			}
			errMu.Unlock()
			entryCancel()
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.arm.MoveThroughJointPositions(entryCtx, [][]float64{sess.Frames[0]}, s.moveOpts, nil); err != nil {
				captureErr(err)
			}
		}()

		if useGripper {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := s.setGripperPosition(entryCtx, sess.GripperPositions[0]); err != nil {
					captureErr(err)
				}
			}()
		}

		wg.Wait()

		// Any abort (internal error OR external cancel): defensively halt the arm.
		if firstErr != nil || ctx.Err() != nil {
			if stopErr := s.arm.Stop(context.Background(), nil); stopErr != nil {
				s.logger.Errorf("failed to stop arm after playback abort: %v", stopErr)
			}
		}
		// Genuine internal failure (NOT a user-requested stop) → surface + log + return.
		if firstErr != nil && ctx.Err() == nil {
			s.setLastError(firstErr)
			s.logger.Errorf("playback aborted during safe entry: %v", firstErr)
			return
		}
		// External stop/cancel → clean return, no error surfaced.
		if ctx.Err() != nil {
			return
		}
	}

	// Only one frame — safe entry was the whole playback.
	if len(sess.Frames) <= 1 {
		s.logger.Infof("playback of session %q complete", sess.Name)
		return
	}

	// -------------------------------------------------------------------------
	// Main motion: arm via MoveThroughJointPositions (one blocking call) and
	// gripper (if useGripper) via a ticker stepping through GripperPositions[1:].
	// Both run under a shared cancelable sub-context (ctx2) so that if either
	// errors the other is prompted to stop promptly.
	// -------------------------------------------------------------------------
	ctx2, cancel2 := context.WithCancel(ctx)
	defer cancel2()

	var (
		wg2      sync.WaitGroup
		firstErr error
		errMu    sync.Mutex
	)

	captureErr2 := func(err error) {
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		errMu.Unlock()
		cancel2()
	}

	// Arm goroutine: one call that blends through all remaining waypoints.
	// NOTE: this assumes the arm driver honors context cancellation to return from
	// the blocking MoveThroughJointPositions call; if it doesn't, stopPlayback/Close
	// would block on <-done.
	dense := interpolateFrames(sess.Frames, s.interpSteps)
	wg2.Add(1)
	go func() {
		defer wg2.Done()
		if err := s.arm.MoveThroughJointPositions(ctx2, dense[1:], s.moveOpts, nil); err != nil {
			// If the context was canceled (by us or the outer stop) that's expected —
			// don't treat it as a real error.
			if ctx2.Err() != nil {
				return
			}
			captureErr2(err)
		}
	}()

	// Gripper goroutine: ticker aligned to the recording frequency.
	if useGripper {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			interval := time.Duration(float64(time.Second) / sess.FrequencyHz)
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for _, pos := range sess.GripperPositions[1:] {
				select {
				case <-ctx2.Done():
					return
				case <-ticker.C:
					if err := s.setGripperPosition(ctx2, pos); err != nil {
						if ctx2.Err() != nil {
							return
						}
						captureErr2(err)
						return
					}
				}
			}
		}()
	}

	wg2.Wait()

	// Any abort (internal error OR external cancel): defensively halt the arm.
	if firstErr != nil || ctx.Err() != nil {
		if stopErr := s.arm.Stop(context.Background(), nil); stopErr != nil {
			s.logger.Errorf("failed to stop arm after playback abort: %v", stopErr)
		}
	}
	// Genuine internal failure (NOT a user-requested stop) → surface + log + return.
	if firstErr != nil && ctx.Err() == nil {
		s.setLastError(firstErr)
		s.logger.Errorf("playback aborted during main motion: %v", firstErr)
		return
	}
	// External stop/cancel → clean return, no error surfaced.
	if ctx.Err() != nil {
		return
	}

	// Return home: only reached on clean completion, since every abort path above
	// has returned. The deferred state reset has not run yet, so the component
	// still reports "playing" and a play arriving mid-return is rejected as busy.
	if s.homeSwitch != nil {
		s.returnHome(ctx, sess.Name)
	}

	s.logger.Infof("playback of session %q complete", sess.Name)
}

// returnHome drives the home_pose switch to its "go to" position. The switch
// performs the move, so its own speed limits and motion planning apply rather
// than ours. Failures surface in last_error but do not halt the arm: the
// playback itself succeeded.
func (s *armRecorderRecorder) returnHome(ctx context.Context, playedSession string) {
	if err := s.homeSwitch.SetPosition(ctx, armPositionSaverGoTo, nil); err != nil {
		if ctx.Err() != nil {
			// Stopped while returning home — expected, not a fault.
			return
		}
		s.setLastError(err)
		s.logger.Errorf("could not return to home pose after playing %q: %v", playedSession, err)
		return
	}
	s.logger.Infof("returned to home pose after playing %q", playedSession)
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
