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

	t.Run("a value just past the limit is pulled back to it", func(t *testing.T) {
		out, changed := clampFrame([]float64{actual}, lim([2]float64{lo, 2.84}))
		if !changed {
			t.Fatal("expected the out-of-range value to be clamped")
		}
		if out[0] != lo {
			t.Fatalf("expected %.8f, got %.8f", lo, out[0])
		}
	})

	t.Run("in-range values pass through untouched", func(t *testing.T) {
		in := []float64{0.1, -0.2, 0.3}
		out, changed := clampFrame(in, lim([2]float64{-1, 1}, [2]float64{-1, 1}, [2]float64{-1, 1}))
		if changed {
			t.Fatal("nothing should have been clamped")
		}
		for i := range in {
			if out[i] != in[i] {
				t.Fatalf("value %d changed: %v -> %v", i, in[i], out[i])
			}
		}
	})

	t.Run("the original frame is not mutated", func(t *testing.T) {
		in := []float64{5.0}
		clampFrame(in, lim([2]float64{-1, 1}))
		if in[0] != 5.0 {
			t.Fatalf("clampFrame mutated its input: %v", in[0])
		}
	})

	t.Run("no limits means record unclamped", func(t *testing.T) {
		in := []float64{99}
		out, changed := clampFrame(in, nil)
		if changed || out[0] != 99 {
			t.Fatalf("expected passthrough, got %v changed=%v", out, changed)
		}
	})

	t.Run("a length mismatch is passthrough, not a panic", func(t *testing.T) {
		out, changed := clampFrame([]float64{1, 2, 3}, lim([2]float64{-1, 1}))
		if changed || len(out) != 3 {
			t.Fatalf("expected passthrough, got %v changed=%v", out, changed)
		}
	})

	t.Run("nonsensical limits are ignored per joint", func(t *testing.T) {
		out, _ := clampFrame(
			[]float64{5, 5},
			lim([2]float64{math.NaN(), 1}, [2]float64{10, -10}), // NaN, and min > max
		)
		if out[0] != 5 || out[1] != 5 {
			t.Fatalf("unusable limits should leave values alone, got %v", out)
		}
	})

	t.Run("both ends clamp", func(t *testing.T) {
		out, _ := clampFrame([]float64{-9, 9}, lim([2]float64{-1, 1}, [2]float64{-1, 1}))
		if out[0] != -1 || out[1] != 1 {
			t.Fatalf("expected [-1 1], got %v", out)
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
	a.KinematicsFunc = func(context.Context) (referenceframe.Model, error) {
		return referenceframe.NewSimpleModel("arm"), nil
	}

	rec := &armRecorderRecorder{
		Named:   resource.NewName(resource.APINamespaceRDK.WithComponentType("sensor"), "rec").AsNamed(),
		logger:  logging.NewTestLogger(t),
		cfg:     &Config{Arm: "a"},
		arm:     a,
		freqHz:  50,
		dataDir: t.TempDir(),
		state:   stateIdle,
	}
	// Limits the model would report; SimpleModel has no DoF of its own here.
	rec.jointLimits = lim([2]float64{lo, hi})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	rec.mu.Lock()
	rec.state = stateRecording
	rec.session = "s"
	rec.workerCancel = cancel
	rec.workerDone = done
	rec.mu.Unlock()
	go rec.recordLoop(ctx, done)

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
