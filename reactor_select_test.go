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
