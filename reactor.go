package armrecorder

import (
	"context"
	"fmt"
	"sync"
	"time"

	sensor "go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/generic"
	vision "go.viam.com/rdk/services/vision"
	"go.viam.com/rdk/vision/objectdetection"
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

	logger        logging.Logger
	vision        vision.Service
	recorder      sensor.Sensor
	camera        string
	labelSessions map[string]string
	minConf       float64
	pollInterval  time.Duration
	cooldown      time.Duration

	mu           sync.Mutex
	reacting     bool
	cancel       context.CancelFunc
	done         chan struct{}
	lastLabel    string
	lastSession  string
	lastPlayedAt time.Time
	lastError    string
}

func newReactor(ctx context.Context, deps resource.Dependencies, rawConf resource.Config, logger logging.Logger) (resource.Resource, error) {
	conf, err := resource.NativeConfig[*ReactorConfig](rawConf)
	if err != nil {
		return nil, err
	}
	vis, err := vision.FromProvider(deps, conf.VisionService)
	if err != nil {
		return nil, fmt.Errorf("could not get vision service %q: %w", conf.VisionService, err)
	}
	rec, err := sensor.FromProvider(deps, conf.Recorder)
	if err != nil {
		return nil, fmt.Errorf("could not get recorder %q: %w", conf.Recorder, err)
	}
	return &reactor{
		Named:         rawConf.ResourceName().AsNamed(),
		logger:        logger,
		vision:        vis,
		recorder:      rec,
		camera:        conf.Camera,
		labelSessions: conf.LabelSessions,
		minConf:       conf.minConfidence(),
		pollInterval:  conf.pollInterval(),
		cooldown:      conf.cooldown(),
	}, nil
}

func (r *reactor) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	command, ok := cmd["command"].(string)
	if !ok || command == "" {
		return nil, fmt.Errorf("missing required argument %q", "command")
	}
	switch command {
	case "start_reacting":
		return r.startReacting()
	case "stop_reacting":
		return r.stopReacting()
	case "status":
		return r.status(), nil
	default:
		return nil, fmt.Errorf("unknown command %q", command)
	}
}

func (r *reactor) startReacting() (map[string]interface{}, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.reacting {
		return nil, fmt.Errorf("already reacting")
	}
	wctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	r.reacting = true
	r.cancel = cancel
	r.done = done
	r.lastError = ""
	go r.reactLoop(wctx, done)
	r.logger.Infof("started reacting (poll %v, cooldown %v, min_confidence %.2f)", r.pollInterval, r.cooldown, r.minConf)
	return map[string]interface{}{"status": "reacting"}, nil
}

func (r *reactor) stopReacting() (map[string]interface{}, error) {
	r.mu.Lock()
	if !r.reacting {
		r.mu.Unlock()
		return nil, fmt.Errorf("not reacting")
	}
	cancel, done := r.cancel, r.done
	r.reacting = false
	r.cancel = nil
	r.done = nil
	r.mu.Unlock()

	cancel()
	<-done
	// Safety: halt any playback the loop may have started.
	if _, err := r.recorder.DoCommand(context.Background(), map[string]interface{}{"command": "stop_playback"}); err != nil {
		r.logger.Debugf("stop_playback on disarm: %v", err) // not playing -> benign
	}
	return map[string]interface{}{"status": "stopped"}, nil
}

func (r *reactor) reactLoop(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

func (r *reactor) tick(ctx context.Context) {
	dets, err := r.vision.DetectionsFromCamera(ctx, r.camera, nil)
	if err != nil {
		if ctx.Err() == nil {
			r.logger.Warnf("detection failed: %v", err)
			r.mu.Lock()
			r.lastError = err.Error()
			r.mu.Unlock()
		}
		return
	}
	label, session, ok := selectSession(dets, r.labelSessions, r.minConf)
	if !ok {
		return
	}
	r.mu.Lock()
	cooldownElapsed := time.Since(r.lastPlayedAt) >= r.cooldown
	r.mu.Unlock()
	if !cooldownElapsed {
		return
	}
	// play is asynchronous: the recorder's playback worker runs on its own
	// context, not the reactor loop's. Cancelling the loop does NOT stop arm
	// motion — stopReacting issues stop_playback to actually halt the arm.
	if _, err := r.recorder.DoCommand(ctx, map[string]interface{}{"command": "play", "session": session}); err != nil {
		// Recorder busy or play error: skip without stamping cooldown; retry next tick.
		if ctx.Err() == nil {
			r.logger.Infof("skipped play of %q (label %q): %v", session, label, err)
			r.mu.Lock()
			r.lastError = err.Error()
			r.mu.Unlock()
		}
		return
	}
	r.mu.Lock()
	r.lastPlayedAt = time.Now()
	r.lastLabel = label
	r.lastSession = session
	r.mu.Unlock()
	r.logger.Infof("reacted to %q -> played session %q", label, session)
}

func (r *reactor) status() map[string]interface{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]interface{}{"reacting": r.reacting}
	if r.lastLabel != "" {
		out["last_label"] = r.lastLabel
		out["last_session"] = r.lastSession
		out["last_played_at"] = r.lastPlayedAt.UTC().Format(time.RFC3339)
		remaining := r.cooldown - time.Since(r.lastPlayedAt)
		if remaining < 0 {
			remaining = 0
		}
		out["cooldown_remaining_sec"] = remaining.Seconds()
	}
	if r.lastError != "" {
		out["last_error"] = r.lastError
	}
	return out
}

func (r *reactor) Close(ctx context.Context) error {
	r.mu.Lock()
	reacting := r.reacting
	r.mu.Unlock()
	if reacting {
		_, _ = r.stopReacting()
	}
	return nil
}

// selectSession returns the session for the highest-confidence detection that
// clears minConfidence and whose label exists in labelSessions.
func selectSession(dets []objectdetection.Detection, labelSessions map[string]string, minConfidence float64) (string, string, bool) {
	bestLabel, bestSession, bestScore := "", "", -1.0
	for _, d := range dets {
		if d.Score() < minConfidence {
			continue
		}
		session, ok := labelSessions[d.Label()]
		if !ok {
			continue
		}
		if d.Score() > bestScore {
			bestScore, bestLabel, bestSession = d.Score(), d.Label(), session
		}
	}
	return bestLabel, bestSession, bestScore >= 0
}
