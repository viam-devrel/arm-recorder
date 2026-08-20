package armrecorder

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.viam.com/rdk/components/arm"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/referenceframe"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/testutils/inject"
)

// These drive the real playLoop with injected hardware. Every previous bug in
// this feature lived in a path the unit tests never entered — config decoding,
// and before that a frozen entry point and a nonexistent camera method — so the
// return-home behavior is exercised here end to end rather than asserted about.

type recorderHarness struct {
	rec     *armRecorderRecorder
	arm     *inject.Arm
	sw      *inject.Switch
	mu      sync.Mutex
	moves   [][][]float64 // every MoveThroughJointPositions call, in order
	switch3 []uint32      // every SetPosition call, in order
	stops   int
}

func newHarness(t *testing.T, home *HomePose) *recorderHarness {
	t.Helper()
	h := &recorderHarness{arm: inject.NewArm("a"), sw: inject.NewSwitch("cam-pose")}

	h.arm.MoveThroughJointPositionsFunc = func(
		_ context.Context, positions [][]referenceframe.Input, _ *arm.MoveOptions, _ map[string]interface{},
	) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		cp := make([][]float64, len(positions))
		for i, p := range positions {
			cp[i] = append([]float64(nil), p...)
		}
		h.moves = append(h.moves, cp)
		return nil
	}
	h.arm.JointPositionsFunc = func(context.Context, map[string]interface{}) ([]referenceframe.Input, error) {
		return []referenceframe.Input{0, 0}, nil
	}
	h.arm.StopFunc = func(context.Context, map[string]interface{}) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.stops++
		return nil
	}
	h.sw.SetPositionFunc = func(_ context.Context, position uint32, _ map[string]interface{}) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.switch3 = append(h.switch3, position)
		return nil
	}

	h.rec = &armRecorderRecorder{
		Named:       resource.NewName(resource.APINamespaceRDK.WithComponentType("sensor"), "rec").AsNamed(),
		logger:      logging.NewTestLogger(t),
		cfg:         &Config{Arm: "a"},
		arm:         h.arm,
		freqHz:      defaultFrequencyHz,
		dataDir:     t.TempDir(),
		state:       stateIdle,
		interpSteps: 0,
		homePose:    home,
	}
	if home != nil && home.usesSwitch() {
		h.rec.homeSwitch = h.sw
	}
	return h
}

// playAndWait runs a session to completion and returns once the worker exits.
func (h *recorderHarness) playAndWait(t *testing.T, name string) {
	t.Helper()
	sess := &Session{
		Name: name, FrequencyHz: 10, JointCount: 2,
		Frames: [][]float64{{0, 0}, {1, 1}, {2, 2}},
	}
	if err := saveSession(h.rec.dataDir, sess); err != nil {
		t.Fatal(err)
	}
	if _, err := h.rec.play(map[string]interface{}{"command": "play", "session": name}); err != nil {
		t.Fatalf("play: %v", err)
	}
	h.rec.mu.Lock()
	done := h.rec.workerDone
	h.rec.mu.Unlock()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("playback did not finish")
	}
}

func (h *recorderHarness) snapshot() ([][][]float64, []uint32, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.moves, h.switch3, h.stops
}

func TestReturnHomeDrivesTheSwitch(t *testing.T) {
	h := newHarness(t, &HomePose{Switch: "cam-pose"})
	h.playAndWait(t, "wave")

	_, positions, _ := h.snapshot()
	if len(positions) != 1 {
		t.Fatalf("expected exactly one switch call, got %v", positions)
	}
	if positions[0] != armPositionSaverGoTo {
		t.Fatalf("expected the switch driven to %d (go to), got %d — driving it to 1 "+
			"would overwrite the saved pose", armPositionSaverGoTo, positions[0])
	}
}

func TestReturnHomeMovesToLiteralJoints(t *testing.T) {
	h := newHarness(t, &HomePose{Joints: []float64{9, 9}})
	h.playAndWait(t, "wave")

	moves, positions, _ := h.snapshot()
	if len(positions) != 0 {
		t.Fatalf("literal joints must not touch a switch, got %v", positions)
	}
	last := moves[len(moves)-1]
	if len(last) != 1 || last[0][0] != 9 || last[0][1] != 9 {
		t.Fatalf("expected the final move to be the home pose, got %v", last)
	}
}

func TestNoHomePoseLeavesTheArmWhereItEnded(t *testing.T) {
	h := newHarness(t, nil)
	h.playAndWait(t, "wave")

	moves, positions, _ := h.snapshot()
	if len(positions) != 0 {
		t.Fatalf("expected no switch calls, got %v", positions)
	}
	last := moves[len(moves)-1]
	if last[len(last)-1][0] != 2 {
		t.Fatalf("last move should be the final recorded frame, got %v", last)
	}
}

func TestStopPlaybackDoesNotReturnHome(t *testing.T) {
	h := newHarness(t, &HomePose{Switch: "cam-pose"})

	// Block the main motion so the stop lands mid-playback.
	release := make(chan struct{})
	h.arm.MoveThroughJointPositionsFunc = func(
		ctx context.Context, positions [][]referenceframe.Input, _ *arm.MoveOptions, _ map[string]interface{},
	) error {
		h.mu.Lock()
		h.moves = append(h.moves, [][]float64{})
		n := len(h.moves)
		h.mu.Unlock()
		if n >= 2 { // safe-entry is first; block on the main motion
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}

	sess := &Session{Name: "wave", FrequencyHz: 10, JointCount: 2,
		Frames: [][]float64{{0, 0}, {1, 1}}}
	if err := saveSession(h.rec.dataDir, sess); err != nil {
		t.Fatal(err)
	}
	if _, err := h.rec.play(map[string]interface{}{"command": "play", "session": "wave"}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		n := len(h.moves)
		h.mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := h.rec.stopPlayback(); err != nil {
		t.Fatalf("stop_playback: %v", err)
	}
	close(release)

	_, positions, stops := h.snapshot()
	if len(positions) != 0 {
		t.Fatalf("stop_playback must not return home — you asked it to stop, got %v", positions)
	}
	if stops == 0 {
		t.Fatal("stop_playback should have halted the arm")
	}
}

func TestRecorderReportsBusyUntilHomeIsReached(t *testing.T) {
	h := newHarness(t, &HomePose{Switch: "cam-pose"})

	// Hold the switch call open and check the state while the return is running.
	inHome := make(chan struct{})
	release := make(chan struct{})
	var stateDuringReturn string
	h.sw.SetPositionFunc = func(context.Context, uint32, map[string]interface{}) error {
		h.rec.mu.Lock()
		stateDuringReturn = h.rec.state
		h.rec.mu.Unlock()
		close(inHome)
		<-release
		return nil
	}

	sess := &Session{Name: "wave", FrequencyHz: 10, JointCount: 2,
		Frames: [][]float64{{0, 0}, {1, 1}}}
	if err := saveSession(h.rec.dataDir, sess); err != nil {
		t.Fatal(err)
	}
	if _, err := h.rec.play(map[string]interface{}{"command": "play", "session": "wave"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-inHome:
	case <-time.After(10 * time.Second):
		t.Fatal("never reached the home move")
	}

	// A trigger arriving mid-return must be rejected, not raced against.
	if _, err := h.rec.play(map[string]interface{}{"command": "play", "session": "wave"}); err == nil {
		t.Fatal("a play during the return home must be rejected as busy")
	}
	close(release)

	if stateDuringReturn != statePlaying {
		t.Fatalf("expected state %q during the return, got %q", statePlaying, stateDuringReturn)
	}
}
