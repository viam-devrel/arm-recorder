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
	Name               string      `json:"session"`
	FrequencyHz        float64     `json:"frequency_hz"`
	RecordedAt         string      `json:"recorded_at"`
	JointCount         int         `json:"joint_count"`
	Frames             [][]float64 `json:"frames"`
	HasGripper         bool        `json:"has_gripper,omitempty"`
	GripperPositionKey string      `json:"gripper_position_key,omitempty"`
	GripperPositions   []float64   `json:"gripper_positions,omitempty"`
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
