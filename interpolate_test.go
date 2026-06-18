package armrecorder

import "testing"

func TestInterpolateFramesOff(t *testing.T) {
	in := [][]float64{{0, 0}, {1, 1}}
	out := interpolateFrames(in, 0)
	if len(out) != 2 {
		t.Fatalf("steps=0 should be unchanged, got len %d", len(out))
	}
}

func TestInterpolateFramesMidpoint(t *testing.T) {
	out := interpolateFrames([][]float64{{0, 0}, {1, 1}}, 1)
	if len(out) != 3 {
		t.Fatalf("expected 3 frames, got %d", len(out))
	}
	if out[1][0] != 0.5 || out[1][1] != 0.5 {
		t.Fatalf("expected midpoint [0.5 0.5], got %v", out[1])
	}
	// endpoints preserved
	if out[0][0] != 0 || out[2][0] != 1 {
		t.Fatalf("endpoints not preserved: %v / %v", out[0], out[2])
	}
}

func TestInterpolateFramesLength(t *testing.T) {
	// 3 frames, steps=3 -> (3-1)*(3+1)+1 = 9
	out := interpolateFrames([][]float64{{0}, {1}, {2}}, 3)
	if len(out) != 9 {
		t.Fatalf("expected 9 frames, got %d", len(out))
	}
}

func TestInterpolationStepsDefault(t *testing.T) {
	if (&Config{}).interpolationSteps() != defaultInterpolationSteps {
		t.Fatalf("nil should default to %d", defaultInterpolationSteps)
	}
	zero := 0
	if (&Config{PlaybackInterpolationSteps: &zero}).interpolationSteps() != 0 {
		t.Fatal("explicit 0 should disable (return 0)")
	}
	five := 5
	if (&Config{PlaybackInterpolationSteps: &five}).interpolationSteps() != 5 {
		t.Fatal("explicit 5 should return 5")
	}
}
