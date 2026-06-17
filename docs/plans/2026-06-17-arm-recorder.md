# Arm Recorder Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.
> Domain reference: superpowers viam-go-platform skill (sensor/resource patterns); arm API confirmed against RDK v0.131.0.

**Goal:** A Viam `sensor` component (`devrel:arm-recorder:recorder`) that records a configured arm's joint positions at a configured frequency into a named session file, then replays that session by commanding the arm — all driven via `DoCommand`.

**Architecture:** Single component in package `armrecorder`. A mutex-guarded state machine (`idle`/`recording`/`playing`) runs one background goroutine at a time. Recording samples `arm.JointPositions` on a ticker and buffers frames; `stop_recording` serializes them to `$VIAM_MODULE_DATA/<session>.json`. Playback loads a file, moves to the first frame slowly, then steps through remaining frames on a ticker via `arm.MoveToJointPositions`. Session file I/O and config validation are pure and unit-tested; motion is validated manually on hardware.

**Tech Stack:** Go 1.25, `go.viam.com/rdk` v0.131.0 (`components/sensor`, `components/arm`, `resource`, `referenceframe`, `logging`), standard `encoding/json`, `os`, `sync`, `time`.

**Key API facts (verified against RDK v0.131.0):**
- `referenceframe.Input` is a type alias for `float64`. `[]referenceframe.Input` ≡ `[]float64` — no conversion needed.
- `arm.Arm`: `JointPositions(ctx, extra) ([]referenceframe.Input, error)`, `MoveToJointPositions(ctx, []referenceframe.Input, extra) error`, `Stop(ctx, extra) error`.
- `arm.FromProvider(deps, name) (arm.Arm, error)` — use this (not deprecated `FromDependencies`).

**DoCommand convention:** every call is `{"command": "<verb>", ...args}` and returns a `map[string]interface{}`.

---

### Task 1: Make the scaffold build with RDK dependencies

The generated `module.go` references `errors`/`fmt` without importing them and has a placeholder `Config`. Get a clean compile first so later tasks have a baseline.

**Files:**
- Modify: `go.mod` (deps already added via `go get go.viam.com/rdk@latest`)
- Modify: `module.go`

**Step 1: Tidy modules**

Run: `go mod tidy`
Expected: completes; `go.mod` requires `go.viam.com/rdk v0.131.0`.

**Step 2: Replace `module.go` with a compiling skeleton**

Replace the entire file with:

```go
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
	return nil, nil, nil
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
```

Note: dropped the hand-rolled `name`/`cancelCtx` fields in favor of embedding `resource.Named` (provides `Name()`) and `resource.AlwaysRebuild`. Lifecycle context for workers is created per-activity in later tasks.

**Step 3: Verify it builds**

Run: `go build ./...`
Expected: no output (success).

**Step 4: Commit**

```bash
git add go.mod go.sum module.go
git commit -m "chore: compiling skeleton with RDK deps"
```

---

### Task 2: Config validation (TDD)

The arm is a required dependency; an unset frequency defaults to 10 Hz via a helper.

**Files:**
- Modify: `module.go`
- Create: `config_test.go`

**Step 1: Write the failing test**

```go
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
```

**Step 2: Run to verify it fails**

Run: `go test ./... -run 'TestConfig|TestFrequency' -v`
Expected: FAIL (compile error: `frequencyHz`/`defaultFrequencyHz` undefined).

**Step 3: Implement**

In `module.go`, add the constant near the top and replace `Validate`, adding the helper:

```go
const defaultFrequencyHz = 10.0

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
```

**Step 4: Run to verify it passes**

Run: `go test ./... -run 'TestConfig|TestFrequency' -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add module.go config_test.go
git commit -m "feat: config validation with arm dependency and frequency default"
```

---

### Task 3: Session type and file I/O round-trip (TDD)

Pure, hardware-free persistence layer. Frames are `[][]float64` (≡ `[][]referenceframe.Input`).

**Files:**
- Create: `session.go`
- Create: `session_test.go`

**Step 1: Write the failing test**

```go
package armrecorder

import (
	"path/filepath"
	"testing"
)

func TestSessionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := &Session{
		Name:        "demo",
		FrequencyHz: 10,
		JointCount:  2,
		Frames:      [][]float64{{0, 0}, {0.1, -0.2}},
	}
	if err := saveSession(dir, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := loadSession(dir, "demo")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if out.Name != "demo" || out.JointCount != 2 || len(out.Frames) != 2 {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
	if out.Frames[1][0] != 0.1 {
		t.Fatalf("frame value mismatch: %v", out.Frames[1])
	}
}

func TestListAndDeleteSessions(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a", "b"} {
		if err := saveSession(dir, &Session{Name: n, FrequencyHz: 10, JointCount: 1, Frames: [][]float64{{0}}}); err != nil {
			t.Fatal(err)
		}
	}
	names, err := listSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 sessions, got %v", names)
	}
	if err := deleteSession(dir, "a"); err != nil {
		t.Fatal(err)
	}
	names, _ = listSessions(dir)
	if len(names) != 1 || names[0] != "b" {
		t.Fatalf("expected [b], got %v", names)
	}
}

func TestSessionNameSanitized(t *testing.T) {
	dir := t.TempDir()
	if err := saveSession(dir, &Session{Name: "../escape", JointCount: 1, Frames: [][]float64{{0}}}); err == nil {
		t.Fatal("expected error for unsafe session name")
	}
	if _, err := loadSession(dir, "../../etc/passwd"); err == nil {
		t.Fatal("expected error for unsafe session name")
	}
	_ = filepath.Separator
}
```

**Step 2: Run to verify it fails**

Run: `go test ./... -run TestSession -v`
Expected: FAIL (undefined `Session`, `saveSession`, etc.).

**Step 3: Implement `session.go`**

```go
package armrecorder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Session is the on-disk representation of a recording.
type Session struct {
	Name        string      `json:"session"`
	FrequencyHz float64     `json:"frequency_hz"`
	RecordedAt  string      `json:"recorded_at"`
	JointCount  int         `json:"joint_count"`
	Frames      [][]float64 `json:"frames"`
}

const sessionExt = ".json"

// safeSessionPath rejects names that could escape the data directory.
func safeSessionPath(dir, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("session name must not be empty")
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", fmt.Errorf("invalid session name %q", name)
	}
	return filepath.Join(dir, name+sessionExt), nil
}

func saveSession(dir string, s *Session) error {
	path, err := safeSessionPath(dir, s.Name)
	if err != nil {
		return err
	}
	if s.RecordedAt == "" {
		s.RecordedAt = time.Now().UTC().Format(time.RFC3339)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func loadSession(dir, name string) (*Session, error) {
	path, err := safeSessionPath(dir, name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read session %q: %w", name, err)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("could not parse session %q: %w", name, err)
	}
	return &s, nil
}

func listSessions(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	names := []string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), sessionExt) {
			names = append(names, strings.TrimSuffix(e.Name(), sessionExt))
		}
	}
	return names, nil
}

func deleteSession(dir, name string) error {
	path, err := safeSessionPath(dir, name)
	if err != nil {
		return err
	}
	return os.Remove(path)
}
```

**Step 4: Run to verify it passes**

Run: `go test ./... -run TestSession -v`
Expected: PASS (all three tests).

**Step 5: Commit**

```bash
git add session.go session_test.go
git commit -m "feat: session file I/O with round-trip, list, delete, path safety"
```

---

### Task 4: Data directory resolution (TDD)

Resolve `VIAM_MODULE_DATA`; fall back to a temp dir with a warning so the module still runs on a dev box.

**Files:**
- Create: `datadir.go`
- Create: `datadir_test.go`

**Step 1: Write the failing test**

```go
package armrecorder

import (
	"os"
	"testing"

	"go.viam.com/rdk/logging"
)

func TestResolveDataDirFromEnv(t *testing.T) {
	want := t.TempDir()
	t.Setenv("VIAM_MODULE_DATA", want)
	got := resolveDataDir(logging.NewTestLogger(t))
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestResolveDataDirFallback(t *testing.T) {
	t.Setenv("VIAM_MODULE_DATA", "")
	os.Unsetenv("VIAM_MODULE_DATA")
	got := resolveDataDir(logging.NewTestLogger(t))
	if got == "" {
		t.Fatal("expected a non-empty fallback dir")
	}
}
```

**Step 2: Run to verify it fails**

Run: `go test ./... -run TestResolveDataDir -v`
Expected: FAIL (undefined `resolveDataDir`).

**Step 3: Implement `datadir.go`**

```go
package armrecorder

import (
	"os"
	"path/filepath"

	"go.viam.com/rdk/logging"
)

// resolveDataDir returns the directory where session files live.
// Prefers VIAM_MODULE_DATA; falls back to a temp dir for dev boxes.
func resolveDataDir(logger logging.Logger) string {
	if dir := os.Getenv("VIAM_MODULE_DATA"); dir != "" {
		return dir
	}
	fallback := filepath.Join(os.TempDir(), "arm-recorder-sessions")
	logger.Warnf("VIAM_MODULE_DATA not set; falling back to %s", fallback)
	return fallback
}
```

**Step 4: Run to verify it passes**

Run: `go test ./... -run TestResolveDataDir -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add datadir.go datadir_test.go
git commit -m "feat: data directory resolution with dev fallback"
```

---

### Task 5: Wire arm dependency, state fields, and Readings()

Connect the constructor to the arm dependency, add the state machine fields, and implement `Readings()` (live status + current joints).

**Files:**
- Modify: `module.go`

**Step 1: Replace the struct, constructor, and Readings**

Update imports to add `sync`, `os`, `go.viam.com/rdk/components/arm`, `go.viam.com/rdk/referenceframe`. Replace the struct/constructor/Readings with:

```go
const (
	stateIdle      = "idle"
	stateRecording = "recording"
	statePlaying   = "playing"
)

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
```

**Step 2: Verify it builds and existing tests pass**

Run: `go build ./... && go test ./...`
Expected: build succeeds; Task 2–4 tests still pass.

**Step 3: Commit**

```bash
git add module.go
git commit -m "feat: arm dependency wiring, state fields, live Readings"
```

---

### Task 6: Recording — start_recording / stop_recording

Background sampling loop plus the DoCommand router. (Router added here, extended in Tasks 7–8.)

**Files:**
- Modify: `module.go`

**Step 1: Add the DoCommand router and recording handlers**

```go
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
```

Note: `import "time"` must be added.

**Step 2: Verify it builds**

Run: `go build ./... && go test ./...`
Expected: success.

**Step 3: Commit**

```bash
git add module.go
git commit -m "feat: start_recording/stop_recording with background sampling loop"
```

---

### Task 7: Playback — play / stop_playback

Load a session, move slowly to the first frame, then step through the rest on the ticker. Async with `arm.Stop` on abort.

**Files:**
- Modify: `module.go`

**Step 1: Add play/stop_playback to the router**

Add cases to the `switch` in `DoCommand`:

```go
	case "play":
		return s.play(cmd)
	case "stop_playback":
		return s.stopPlayback()
```

**Step 2: Implement the handlers**

```go
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
```

Note: `MoveToJointPositions` blocks until the move completes. At high frequencies the per-frame move may take longer than the ticker interval; the ticker drops ticks rather than queueing, so playback degrades gracefully to "as fast as the arm can move." This is acceptable for the POC — flag it in the README.

**Step 3: Verify it builds**

Run: `go build ./... && go test ./...`
Expected: success.

**Step 4: Commit**

```bash
git add module.go
git commit -m "feat: play/stop_playback with safe entry move and abort"
```

---

### Task 8: Utility commands — list_sessions / delete_session

**Files:**
- Modify: `module.go`

**Step 1: Add cases to the router**

```go
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
```

**Step 2: Verify it builds**

Run: `go build ./... && go test ./...`
Expected: success.

**Step 3: Commit**

```bash
git add module.go
git commit -m "feat: list_sessions/delete_session utility commands"
```

---

### Task 9: Close() cleanup

On shutdown, abort any running worker and stop the arm.

**Files:**
- Modify: `module.go`

**Step 1: Implement Close**

```go
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
```

**Step 2: Verify**

Run: `go build ./... && go test ./...`
Expected: success.

**Step 3: Commit**

```bash
git add module.go
git commit -m "feat: Close aborts worker and stops arm"
```

---

### Task 10: Build the module binary and document usage

**Files:**
- Modify: `README.md`

**Step 1: Build the module entrypoint**

Run: `go build -o bin/arm-recorder ./cmd/module`
Expected: produces `bin/arm-recorder`.

**Step 2: Document config + DoCommand usage in README.md**

Include:
- Example component config:
  ```json
  {
    "name": "recorder",
    "model": "devrel:arm-recorder:recorder",
    "type": "sensor",
    "attributes": { "arm": "my_arm", "frequency_hz": 10 },
    "depends_on": ["my_arm"]
  }
  ```
- The six DoCommands with example payloads.
- The playback caveat (high-Hz sessions replay as fast as the arm can move).
- Note that sessions live under `$VIAM_MODULE_DATA`.

**Step 3: Commit**

```bash
git add README.md
git commit -m "docs: configuration and DoCommand usage"
```

---

### Task 11: Manual hardware validation

Not automated — requires a real or simulated arm. Use `viam module reload` or local config to run the module against a machine with an arm.

**Validation checklist:**
1. Configure the component pointing at a real/sim arm; confirm it builds and `Readings()` returns live joints.
2. `{"command":"start_recording","session":"test1"}` → hand-move (or jog) the arm → `{"command":"stop_recording"}`. Confirm a `test1.json` appears under `$VIAM_MODULE_DATA` with frames.
3. `{"command":"list_sessions"}` → shows `test1`.
4. Move the arm elsewhere, then `{"command":"play","session":"test1"}`. Confirm the slow entry move, then the replay. `Readings()` shows `state: playing`.
5. During playback, `{"command":"stop_playback"}` → arm stops promptly.
6. `{"command":"delete_session","session":"test1"}` → file removed.

**Step 1: Commit any fixes found during validation**

```bash
git commit -am "fix: <issue found during hardware testing>"
```

---

## Notes for the executing engineer

- **TDD scope:** Tasks 2–4 are unit-tested (pure logic). Tasks 5–9 touch the arm and are validated by build + manual hardware testing (Task 11) — there is no fake arm in this POC. If you want automated coverage there, introduce a small `armReader`/`armMover` interface the struct depends on and a fake in tests; this is an optional enhancement, not required for the POC.
- **Frequency interval:** `time.Duration(float64(time.Second)/freqHz)` — guard against `freqHz <= 0` is already handled by `Config.frequencyHz()`.
- **`[]referenceframe.Input` ≡ `[]float64`:** you can pass `sess.Frames[i]` directly to `MoveToJointPositions` and assign `arm.JointPositions(...)` results directly into `[]float64` frames; no conversion helpers needed.
- **Run the full suite before each commit:** `go test ./...`.

---

# Addendum tasks (2026-06-17): proto fix, gripper tracking, smooth playback

Verified API facts (RDK v0.131.0):
- Sensor `Readings` / `DoCommand` maps are serialized as protobuf `Struct`. Typed slices are rejected: `structpb.NewStruct({"x": []float64{...}})` → `proto: invalid type: []float64`; `[]string` fails the same way; `[]interface{}` is accepted. (Confirmed by probe.)
- `arm.MoveThroughJointPositions(ctx, positions [][]referenceframe.Input, options *arm.MoveOptions, extra map[string]any) error` — blends through waypoints in one blocking call. `arm.MoveOptions{ MaxVelRads, MaxAccRads float64; ... }`. Pass `nil` for driver defaults.
- `gripper.FromProvider(deps, name) (gripper.Gripper, error)`; `gripper.Gripper` embeds `resource.Resource`, so `gripper.DoCommand(ctx, map)` is available.

### Task 12: Fix proto-unsafe Readings/list_sessions (TDD)

**Files:** Modify `module.go`; create/extend `proto_test.go`.

**Step 1: Failing test** — add `proto_test.go`:

```go
package armrecorder

import (
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
)

func TestReadingsMapIsProtoSafe(t *testing.T) {
	// joints must be []interface{}, not []float64
	if _, err := structpb.NewStruct(map[string]interface{}{
		"joints": toInterfaceSlice([]float64{0.1, -0.2, 0.3}),
	}); err != nil {
		t.Fatalf("joints not proto-encodable: %v", err)
	}
}

func TestSessionsListIsProtoSafe(t *testing.T) {
	if _, err := structpb.NewStruct(map[string]interface{}{
		"sessions": toStringInterfaceSlice([]string{"a", "b"}),
	}); err != nil {
		t.Fatalf("sessions not proto-encodable: %v", err)
	}
}
```

**Step 2:** Run `go test ./... -run Proto -v` → FAIL (undefined helpers).

**Step 3: Implement** in `module.go`:

```go
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
```

Then in `Readings`, set `out["joints"] = toInterfaceSlice(joints)` (joints is `[]float64`). In the `list_sessions` handler, return `map[string]interface{}{"sessions": toStringInterfaceSlice(names)}`.

**Step 4:** Run `go test ./...` → PASS. Also worth a manual note: this is the fix for the reported `[unknown] proto: invalid type: []float64`.

**Step 5: Commit** — `fix: make Readings/list_sessions responses proto-encodable`.

### Task 13: Gripper config + validation + session fields (TDD)

**Files:** Modify `module.go`, `session.go`; extend `config_test.go`, `session_test.go`.

- Add to `Config`: `Gripper string json:"gripper,omitempty"`, `GripperPositionKey string json:"gripper_position_key,omitempty"`, `MaxVelocityRadsPerSec float64 json:"max_velocity_rads_per_sec,omitempty"`, `MaxAccelerationRadsPerSec float64 json:"max_acceleration_rads_per_sec,omitempty"`.
- `Validate`: when `cfg.Gripper != ""`, append it to the required-deps slice. Reject negative vel/accel.
- Add `func (cfg *Config) gripperPositionKey() string` returning `"position"` when empty.
- Add to `Session`: `HasGripper bool json:"has_gripper,omitempty"`, `GripperPositionKey string json:"gripper_position_key,omitempty"`, `GripperPositions []float64 json:"gripper_positions,omitempty"`.

**Tests:** extend `config_test.go` (gripper added to required deps when set; absent when unset; default key) and `session_test.go` (round-trip preserves gripper fields; a file with no gripper fields loads with `HasGripper=false`). TDD: write tests first, see them fail, implement, pass. **Commit** — `feat: gripper config, validation, and session fields`.

### Task 14: Gripper-aware recording + Readings

**Files:** Modify `module.go`.

- Constructor: when `conf.Gripper != ""`, resolve `gripper.FromProvider(deps, conf.Gripper)` and store `s.gripper` + `s.gripperKey = conf.gripperPositionKey()`. Build/store `s.moveOpts *arm.MoveOptions` (nil unless a vel/accel is set). Add struct fields: `gripper gripper.Gripper`, `gripperKey string`, `gripperPositions []float64`, `moveOpts *arm.MoveOptions`.
- Helpers:
  ```go
  func (s *armRecorderRecorder) readGripperPosition(ctx context.Context) (float64, error) {
  	resp, err := s.gripper.DoCommand(ctx, map[string]interface{}{"command": "get_position"})
  	if err != nil {
  		return 0, err
  	}
  	v, ok := resp[s.gripperKey]
  	if !ok {
  		return 0, fmt.Errorf("get_position response missing key %q", s.gripperKey)
  	}
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
  ```
- `recordLoop`: each tick read joints (outside lock); if `s.gripper != nil` read gripper position (outside lock); on either error set `lastError`, reset to idle, return. Then under one lock hold, append the joint frame to `s.frames` AND (if gripper) the position to `s.gripperPositions`. Reset `s.gripperPositions = nil` in `startRecording` alongside `s.frames = nil`.
- `stopRecording`: when saving, set `sess.HasGripper = s.gripper != nil`, `sess.GripperPositionKey = s.gripperKey`, `sess.GripperPositions = s.gripperPositions`.
- `Readings`: convert joints via `toInterfaceSlice`; if `s.gripper != nil` add `out["has_gripper"] = true` and a live `gripper_position` read — on error set `out["gripper_error"]` instead (do the read outside the lock, like joints).

Verify `go build ./... && go vet ./... && go test ./...` clean. **Commit** — `feat: record and report gripper position alongside joints`.

### Task 15: Smooth, gripper-synced playback (MoveThrough + parallel track)

**Files:** Modify `module.go`.

- `play` validation (before launching the worker): keep the existing all-frames joint-count check; then:
  - if `sess.HasGripper && s.gripper == nil` → return error.
  - if `sess.HasGripper && len(sess.GripperPositions) != len(sess.Frames)` → return error.
  - if `!sess.HasGripper && s.gripper != nil` → log a notice that the session has no gripper data; play arm only.
  - Determine `useGripper := sess.HasGripper && s.gripper != nil`.
- Rewrite `playLoop` to:
  1. **Safe entry (concurrent):** move arm to `sess.Frames[0]` via `MoveToJointPositions` and, if `useGripper`, `setGripperPosition(ctx, sess.GripperPositions[0])` — run them concurrently (one in a goroutine), wait for both, abort on either error or ctx cancel.
  2. If more than one frame: **concurrently** run
     - arm: `s.arm.MoveThroughJointPositions(ctx, sess.Frames[1:], s.moveOpts, nil)` (one call), and
     - gripper (if `useGripper`): a ticker at `time.Duration(float64(time.Second)/sess.FrequencyHz)` stepping `setGripperPosition` through `sess.GripperPositions[1:]`, stopping when exhausted or ctx done.
     Wait for both to finish; if either errors, cancel the shared context so the other unwinds; on ctx cancel call `arm.Stop`.
  3. Deferred cleanup resets state to idle and clears worker fields (as today). Keep the `sess.FrequencyHz` validity guard (`<=0`/NaN/Inf → defaultFrequencyHz) from the earlier fix — the gripper ticker depends on it.
- Use a `sync.WaitGroup` + a shared cancelable sub-context (`context.WithCancel(ctx)`); capture the first error. Never hold `s.mu` across any arm/gripper call.

Verify `go build ./... && go vet ./... && gofmt -l . && go test ./... -race` clean (the `-race` run matters for the new concurrent track). **Commit** — `feat: smooth playback via MoveThroughJointPositions with synced gripper track`.

### Task 16: Docs update

Update `README.md`: new optional config attributes (`gripper`, `gripper_position_key`, `max_velocity_rads_per_sec`, `max_acceleration_rads_per_sec`); the gripper `get_position`/`set_position` DoCommand contract; new Readings keys (`has_gripper`, `gripper_position`, `gripper_error`); the smooth-playback behavior note (arm motion governed by MoveOptions, gripper track is best-effort wall-clock aligned); and the session-file gripper fields. **Commit** — `docs: gripper tracking and smooth playback`.
