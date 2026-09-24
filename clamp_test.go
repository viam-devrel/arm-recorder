package armrecorder

import (
	"context"
	"math"
	"testing"
	"time"

	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/referenceframe"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/testutils/inject"
)

func lim(pairs ...[2]float64) []referenceframe.Limit {
	out := make([]referenceframe.Limit, len(pairs))
	for i, p := range pairs {
		out[i] = referenceframe.Limit{Min: p[0], Max: p[1]}
	}
	return out
}

func TestClampFrame(t *testing.T) {
	// The real case: joint 5's model limit is -157.2110246 deg while the servo
	// reaches -157.53846. In radians that is a third of a degree past the limit.
	lo := -157.2110246 * math.Pi / 180
	actual := -157.53846153846152 * math.Pi / 180

	t.Run("values past either limit are pulled back to it", func(t *testing.T) {
		frame := []float64{actual, 9}
		if !clampFrame(frame, lim([2]float64{lo, 2.84}, [2]float64{-1, 1})) {
			t.Fatal("expected the out-of-range values to be clamped")
		}
		if frame[0] != lo || frame[1] != 1 {
			t.Fatalf("expected [%.8f 1], got %v", lo, frame)
		}
	})

	t.Run("in-range values pass through untouched", func(t *testing.T) {
		frame := []float64{0.1, -0.2, 0.3}
		want := append([]float64(nil), frame...)
		if clampFrame(frame, lim([2]float64{-1, 1}, [2]float64{-1, 1}, [2]float64{-1, 1})) {
			t.Fatal("nothing should have been clamped")
		}
		for i := range want {
			if frame[i] != want[i] {
				t.Fatalf("value %d changed: %v -> %v", i, want[i], frame[i])
			}
		}
	})

	t.Run("no limits means record unclamped", func(t *testing.T) {
		frame := []float64{99}
		if clampFrame(frame, nil) || frame[0] != 99 {
			t.Fatalf("expected passthrough, got %v", frame)
		}
	})

	t.Run("a NaN limit leaves the value alone", func(t *testing.T) {
		frame := []float64{5}
		clampFrame(frame, lim([2]float64{math.NaN(), 1}))
		if frame[0] != 5 {
			t.Fatalf("expected 5, got %v", frame[0])
		}
	})
}

// TestRecordingClampsToArmLimits drives the real record loop with an injected
// arm reporting a position outside its own declared limits — the exact shape of
// the failure, since playback of such a frame is refused by RDK's arm client.
func TestRecordingClampsToArmLimits(t *testing.T) {
	lo, hi := -2.744, 2.841 // roughly joint 5's real limits in radians
	past := lo - 0.006      // ~0.33 degrees beyond

	a := inject.NewArm("a")
	a.JointPositionsFunc = func(context.Context, map[string]interface{}) ([]referenceframe.Input, error) {
		return []referenceframe.Input{past}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	rec := &armRecorderRecorder{
		Named:        resource.NewName(resource.APINamespaceRDK.WithComponentType("sensor"), "rec").AsNamed(),
		logger:       logging.NewTestLogger(t),
		cfg:          &Config{Arm: "a"},
		arm:          a,
		freqHz:       50,
		dataDir:      t.TempDir(),
		state:        stateRecording,
		session:      "s",
		workerCancel: cancel,
		workerDone:   done,
	}
	go rec.recordLoop(ctx, done, lim([2]float64{lo, hi}))

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rec.mu.Lock()
		n := len(rec.frames)
		rec.mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.frames) == 0 {
		t.Fatal("no frames recorded")
	}
	for i, f := range rec.frames {
		if f[0] < lo {
			t.Fatalf("frame %d stored %.6f, below the limit %.6f — playback of this "+
				"frame would be refused by the arm client", i, f[0], lo)
		}
	}
	if rec.clampedFrames == 0 {
		t.Fatal("clamping happened but was not counted, so stop_recording would not report it")
	}
}
