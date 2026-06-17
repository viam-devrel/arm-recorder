package armrecorder

import (
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
}
