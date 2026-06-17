# Arm Recorder — Design

**Date:** 2026-06-17
**Model:** `devrel:arm-recorder:recorder` (Viam `sensor` component, Go)
**Status:** Approved — proof of concept

## Goal

A Viam sensor component that records the joint positions of a configured arm at
a configured frequency as a named session, then replays that session by
commanding the arm. Sessions are saved to / loaded from the directory given by
the `VIAM_MODULE_DATA` environment variable. Control is via `DoCommand`.

## Configuration

```go
type Config struct {
    Arm         string  `json:"arm"`          // required — the arm to record/replay
    FrequencyHz float64 `json:"frequency_hz"` // optional, default 10
}
```

- `Validate` returns `[]string{cfg.Arm}` as a **required dependency** so the
  runtime injects the arm into the constructor.
- A zero or invalid `FrequencyHz` falls back to the default (10 Hz).
- The arm is resolved from `deps` in the constructor and held on the struct.

## State & concurrency

A single state machine — `idle` / `recording` / `playing` — guarded by a mutex.
Only one activity runs at a time; starting a second is rejected with a clear
error. Each background activity gets its own cancelable context derived from the
component's lifecycle context.

## Recording

- `start_recording {session}` — spins up a goroutine with a `time.Ticker` at
  `1/FrequencyHz`. Each tick reads the arm's joint positions and appends a frame
  to an in-memory slice.
- `stop_recording` — cancels the loop and writes the session to
  `$VIAM_MODULE_DATA/<session>.json`.

## File format (JSON, one file per session)

```json
{
  "session": "demo",
  "frequency_hz": 10,
  "recorded_at": "2026-06-17T00:00:00Z",
  "joint_count": 6,
  "frames": [[0, 0, 0, 0, 0, 0]]
}
```

- Joints stored as plain `float64` arrays.
- `VIAM_MODULE_DATA` resolved at construct time. If unset, fall back to a temp
  dir with a warning log so it still runs on a dev box.

## Playback — `play {session}`

1. Load + validate the file (joint count must match the live arm).
2. **Slow safe entry:** move to `frames[0]` first and wait until reached, before
   any timed motion.
3. Run a goroutine stepping through the remaining frames at the ticker interval,
   commanding the arm per frame.
4. **Async by design:** the `DoCommand` returns immediately
   (`{"status":"playing"}`) to avoid gRPC timeouts on long sessions; progress is
   visible via `Readings()`. `stop_playback` aborts and calls `arm.Stop`.

## Utility commands

- `list_sessions` — returns the saved session names found in the data dir.
- `delete_session {session}` — deletes a saved session file.

## Readings()

Returns live status plus current joints:

```json
{
  "state": "idle | recording | playing",
  "session": "demo",
  "frame_count": 120,
  "joint_count": 6,
  "joints": [0, 0, 0, 0, 0, 0]
}
```

Doubles as something to watch in the app and to feed Viam data capture.

## Error handling & safety

- Per-command argument validation with clear errors.
- Background loops log and abort on repeated arm errors.
- `Close` and `stop_playback` both call `arm.Stop`.
- Joint-count mismatch on playback is a hard refusal.

## Testing

- Unit test the file save/load round-trip and `Config.Validate`.
- Manual hardware validation via `DoCommand` (the existing `cmd/cli` can drive
  the calls).

## Open implementation detail

The exact arm Go signatures (`JointPositions` / `MoveToJointPositions` — these
moved toward `[]referenceframe.Input` in recent RDK) will be pinned down against
the actual RDK version after `go mod tidy`, rather than guessed during design.
