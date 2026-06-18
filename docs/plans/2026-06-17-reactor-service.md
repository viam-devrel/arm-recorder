# Reactor Service Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.
> Design: `docs/plans/2026-06-17-reactor-design.md`. Builds on the existing `recorder` sensor in this module.

**Goal:** Add a second model `devrel:arm-recorder:reactor` — a Viam `generic` service that polls a detector vision service and, when a mapped object label is detected, triggers the `recorder` sensor to `play` the matching session (vision-based feedback for manipulation).

**Architecture:** New `reactor.go` in package `armrecorder`. A `generic` service holding a guarded background poll loop (default off; armed via `start_reacting`, disarmed via `stop_reacting`). Each tick runs `DetectionsFromCamera`, picks the highest-confidence detection whose label is in a config map and clears a confidence threshold, and — if a global cooldown has elapsed — calls the recorder's `play` DoCommand. Reuses the recorder's concurrency discipline (cancelable ctx + done channel; mutex never held across a vision/recorder call).

**Tech Stack:** Go, `go.viam.com/rdk` v0.131.0 — `services/generic` (API), `services/vision` (`Service`, `FromProvider`, `DetectionsFromCamera`), `components/sensor` (`FromProvider`, `DoCommand`), `vision/objectdetection` (`Detection`), `resource`, `logging`.

**Verified API facts (RDK v0.131.0):**
- `generic.API` is the service API; `generic.Service` is just `resource.Resource` (DoCommand/Close/Name/Reconfigure). Register with `resource.RegisterService(generic.API, Reactor, resource.Registration[resource.Resource, *ReactorConfig]{...})`.
- `vision.FromProvider(deps, name) (vision.Service, error)`; `vision.Service.DetectionsFromCamera(ctx, cameraName string, extra) ([]objectdetection.Detection, error)`.
- `sensor.FromProvider(deps, name) (sensor.Sensor, error)`; call its `DoCommand` for `play`/`stop_playback`.
- `objectdetection.Detection`: `Label() string`, `Score() float64`. Construct in tests via `objectdetection.NewDetectionWithoutImgBounds(image.Rectangle, score, label)`.

**Naming note:** the package already has a `Config` struct (recorder). The reactor's config is `ReactorConfig` and its model var is `Reactor`.

---

### Task 1: Register the reactor generic-service skeleton

**Files:**
- Modify: `reactor.go` (currently a one-line comment stub — replace with the skeleton)
- Modify: `cmd/module/main.go`

**Step 1: Replace `reactor.go` with a compiling skeleton**

```go
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
```

**Step 2: Wire the second model into `cmd/module/main.go`**

```go
package main

import (
	"armrecorder"

	sensor "go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/generic"
)

func main() {
	module.ModularMain(
		resource.APIModel{API: sensor.API, Model: armrecorder.Recorder},
		resource.APIModel{API: generic.API, Model: armrecorder.Reactor},
	)
}
```

**Step 3: Verify build**

Run: `go build ./... && go vet ./...`
Expected: clean.

**Step 4: Commit**

```bash
git add reactor.go cmd/module/main.go
git commit -m "chore: register reactor generic-service skeleton"
```

---

### Task 2: ReactorConfig validation + defaults (TDD)

**Files:**
- Modify: `reactor.go`
- Create: `reactor_config_test.go`

**Step 1: Write the failing test**

```go
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
```

**Step 2: Run to verify it fails**

Run: `go test ./... -run Reactor -v`
Expected: FAIL (undefined `pollInterval`/`minConfidence`/`cooldown`/`defaultPollIntervalMs`; Validate too permissive).

**Step 3: Implement** — replace `Validate` and add constants + helpers in `reactor.go`:

```go
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
```

Add `"time"` to the imports. (Note: `min_confidence: 0` is treated as "use default 0.5"; documented as such — acceptable for the POC.)

**Step 4: Run to verify it passes**

Run: `go test ./... -run Reactor -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add reactor.go reactor_config_test.go
git commit -m "feat: reactor config validation and tuning defaults"
```

---

### Task 3: `selectSession` match selection (TDD)

**Files:**
- Modify: `reactor.go`
- Create: `reactor_select_test.go`

**Step 1: Write the failing test**

```go
package armrecorder

import (
	"image"
	"testing"

	"go.viam.com/rdk/vision/objectdetection"
)

func det(label string, score float64) objectdetection.Detection {
	return objectdetection.NewDetectionWithoutImgBounds(image.Rect(0, 0, 1, 1), score, label)
}

func TestSelectSession(t *testing.T) {
	m := map[string]string{"cup": "grab-cup", "bottle": "grab-bottle"}

	t.Run("no detections -> no match", func(t *testing.T) {
		if _, _, ok := selectSession(nil, m, 0.5); ok {
			t.Fatal("expected no match")
		}
	})

	t.Run("below threshold -> no match", func(t *testing.T) {
		if _, _, ok := selectSession([]objectdetection.Detection{det("cup", 0.4)}, m, 0.5); ok {
			t.Fatal("expected no match below threshold")
		}
	})

	t.Run("unmapped label -> no match", func(t *testing.T) {
		if _, _, ok := selectSession([]objectdetection.Detection{det("spoon", 0.9)}, m, 0.5); ok {
			t.Fatal("expected no match for unmapped label")
		}
	})

	t.Run("highest confidence mapped wins", func(t *testing.T) {
		dets := []objectdetection.Detection{det("cup", 0.6), det("bottle", 0.95), det("spoon", 0.99)}
		label, session, ok := selectSession(dets, m, 0.5)
		if !ok || label != "bottle" || session != "grab-bottle" {
			t.Fatalf("expected bottle/grab-bottle, got %q/%q ok=%v", label, session, ok)
		}
	})
}
```

**Step 2: Run to verify it fails**

Run: `go test ./... -run TestSelectSession -v`
Expected: FAIL (undefined `selectSession`).

**Step 3: Implement** in `reactor.go`:

```go
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
```

Add `"go.viam.com/rdk/vision/objectdetection"` to imports.

**Step 4: Run to verify it passes**

Run: `go test ./... -run TestSelectSession -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add reactor.go reactor_select_test.go
git commit -m "feat: reactor detection-to-session selection"
```

---

### Task 4: Constructor wiring, control DoCommands, and the poll loop

**Files:**
- Modify: `reactor.go`

**Step 1: Expand the struct and constructor**

Add imports: `"sync"`, `vision "go.viam.com/rdk/services/vision"`, `sensor "go.viam.com/rdk/components/sensor"`. Replace the struct + constructor:

```go
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
```

**Step 2: DoCommand router + control handlers + loop**

```go
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
	if _, err := r.recorder.DoCommand(ctx, map[string]interface{}{"command": "play", "session": session}); err != nil {
		// Recorder busy or play error: skip without stamping cooldown; retry next tick.
		if ctx.Err() == nil {
			r.logger.Infof("skipped play of %q (label %q): %v", session, label, err)
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
```

**Concurrency rules (match the recorder):** `vision`, `recorder`, `camera`, `labelSessions`, `minConf`, `pollInterval`, `cooldown` are set once in the constructor and never mutated, so they are read lock-free in the loop. The mutex guards only `reacting`/`cancel`/`done`/`last*` and is never held across a `DetectionsFromCamera` or recorder `DoCommand` call.

**Step 3: Verify**

Run: `go build ./... && go vet ./... && gofmt -l . && go test ./... -race -count=1`
Expected: all clean (existing recorder tests + new reactor unit tests pass; `-race` covers the loop's shared-state access).

**Step 4: Commit**

```bash
git add reactor.go
git commit -m "feat: reactor poll loop with armed start/stop and cooldown"
```

---

### Task 5: Documentation

**Files:**
- Create: `devrel_arm-recorder_reactor.md` (resource doc, mirroring the recorder's `devrel_arm-recorder_recorder.md`)
- Modify: `README.md` (add a Reactor section/link)

Document: what the reactor does; the config attributes (`vision_service`, `camera`, `recorder` required; `label_sessions` map required; `poll_interval_ms` default 500; `min_confidence` default 0.5, note `0`→default; `cooldown_sec` default 5) with an example config JSON showing the service plus `depends_on: [vision_service, camera, recorder]`; the three DoCommands (`start_reacting`, `stop_reacting`, `status`) with example payloads/responses (read the router to be exact); the behavior (default off; global cooldown; highest-confidence wins; recorder-busy ticks are skipped; `stop_reacting`/`Close` halt the arm via the recorder's `stop_playback`); and a safety note that the arm moves autonomously once armed.

**Step: Commit**

```bash
git add devrel_arm-recorder_reactor.md README.md
git commit -m "docs: reactor service configuration and DoCommands"
```

---

### Task 6: Manual hardware validation

Not automated (needs a camera + detector vision service + arm/recorder). Checklist:
1. Configure a detector `vision` service over a `camera`, the existing `recorder` sensor (with at least one saved session), and the `reactor` service mapping a real detector label to that session; list all three in `depends_on`.
2. `{"command":"status"}` → `reacting:false`.
3. `{"command":"start_reacting"}`; present the mapped object to the camera → the arm replays the mapped session. Confirm via `recorder` `Readings()` showing `state:"playing"` and reactor `status` showing `last_label`/`last_session`.
4. Keep the object in view → confirm it does NOT replay again until `cooldown_sec` elapses.
5. `{"command":"stop_reacting"}` → loop stops; if mid-playback, the arm halts (stop_playback).
6. Remove the object / show an unmapped object → no reaction.

```bash
git commit -am "fix: <issue found during hardware testing>"
```

---

## Notes for the executing engineer

- **TDD scope:** Tasks 2–3 are unit-tested pure logic. Task 4 touches live vision/recorder and is validated by build + `-race` + manual testing (no fake vision/sensor in this POC; an injectable interface + fakes is a future enhancement).
- **Reuse, don't reinvent:** the loop/armed-state pattern mirrors the recorder's record/play loops — same cancel+done+mutex discipline.
- **Run the full suite before each commit:** `go test ./... -race`.
