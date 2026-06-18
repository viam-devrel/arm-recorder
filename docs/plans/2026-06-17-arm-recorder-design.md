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

---

# Addendum (2026-06-17): gripper tracking + playback smoothing + proto fix

## Bug fixes (existing behavior)

- **Readings proto error.** Sensor `Readings` and `DoCommand` responses are
  serialized as a protobuf `Struct`, which rejects typed slices — confirmed:
  `structpb.NewStruct({"joints": []float64{...}})` → `proto: invalid type:
  []float64` (the reported error), and `[]string` fails identically while
  `[]interface{}` is accepted. Fix: convert `joints` (and the `list_sessions`
  `sessions` `[]string`) to `[]interface{}` before returning. Add a helper and a
  regression test that `structpb.NewStruct` succeeds on the produced maps.

- **Jerky playback.** Replace per-frame `MoveToJointPositions` calls with a
  single `MoveThroughJointPositions(ctx, [][]Input, *MoveOptions, extra)` so the
  arm driver blends through the waypoints instead of decelerating to zero at
  each frame.

## Gripper tracking (additive, optional)

### Config (new optional fields)

```go
Gripper            string  `json:"gripper,omitempty"`
GripperPositionKey string  `json:"gripper_position_key,omitempty"` // default "position"
// playback velocity/accel profile for MoveThroughJointPositions (optional)
MaxVelocityRadsPerSec     float64 `json:"max_velocity_rads_per_sec,omitempty"`
MaxAccelerationRadsPerSec float64 `json:"max_acceleration_rads_per_sec,omitempty"`
```

`Validate` adds the gripper to **required** deps only when `gripper != ""`.
Existing arm-only configs are unaffected.

### Position contract (configurable key, fixed verbs)

- Read: `gripper.DoCommand({"command":"get_position"})`; extract the value at
  `GripperPositionKey`, assert `float64`. Missing key / non-number → clear error.
- Write: `gripper.DoCommand({"command":"set_position", <key>: value})`.
- Dependency resolved via `gripper.FromProvider(deps, name)`; `gripper.Gripper`
  embeds `resource.Resource`, providing `DoCommand`.

### File format (additive, parallel arrays)

```json
{
  "has_gripper": true,
  "gripper_position_key": "position",
  "gripper_positions": [0.42, 0.43]
}
```

`gripper_positions[i]` pairs with `frames[i]`. Old files (fields absent) load
with `has_gripper=false`.

### Recording

Each tick: read joints; if a gripper is configured, also read its position. Both
appended under the same lock hold so the arrays stay index-aligned. A gripper
read error aborts recording exactly like a joint read error (`last_error`, reset
to idle).

### Playback engine: MoveThrough + parallel gripper track (chosen)

1. Up-front validation: every frame length == live arm joint count; if
   `has_gripper` and no gripper configured → hard error; if `has_gripper`,
   `len(gripper_positions) == len(frames)`; if gripper configured but session has
   none → play arm only and log a notice.
2. Safe entry: concurrently `MoveToJointPositions(frames[0])` and (if gripper)
   `set_position(gripper_positions[0])`, block until both done.
3. Then concurrently: `MoveThroughJointPositions(frames[1:], moveOpts)` for the
   arm (one blended motion) AND a gripper goroutine stepping
   `set_position(gripper_positions[1:])` on a ticker at record Hz. Wait for both.
   `moveOpts` built from the optional vel/accel config; `nil` when unset (driver
   defaults).
4. Arm duration is governed by `MoveOptions`, not record Hz, so gripper sync is
   best-effort wall-clock alignment (accepted for the POC). Context cancel aborts
   both and calls `arm.Stop`.

### Readings

Adds `gripper_position` (live read when configured) and `has_gripper`; a failed
live read surfaces `gripper_error` (mirrors `joints_error`). `joints` is now
emitted as `[]interface{}`.

---

# Addendum (2026-06-17): playback interpolation for path fidelity

## Problem

`MoveThroughJointPositions` blends through waypoints (that is what makes it
smooth) but, with sparse 10 Hz capture, it corner-cuts and visibly skips past
recorded positions. `arm.MoveOptions` has no blend-radius/path-tolerance knob —
only velocity/acceleration — so the lever is **waypoint density**: denser
waypoints shrink each blend corner so the executed path hugs the recording.

## Approach (chosen): interpolate at playback

- **New optional config** `playback_interpolation_steps` — number of
  linearly-interpolated waypoints inserted between each consecutive recorded
  frame. **Default 10**; **explicit `0` disables** interpolation (today's
  behavior). Because Go's zero value can't distinguish "omitted" from
  "explicitly 0", the field is a **pointer** (`*int`): `nil` → default 10,
  non-nil → use the value. Negative values rejected in `Validate`.
- **Pure helper** `interpolateFrames(frames [][]float64, steps int) [][]float64`:
  `steps <= 0` (or < 2 frames) returns input unchanged; otherwise for each pair
  `(frames[i], frames[i+1])` emit `frames[i]` + `steps` evenly-spaced
  interpolated points, then append the final frame. Length
  `(len-1)*(steps+1)+1`. Unit-tested (endpoints preserved, midpoint at steps=1,
  length formula, multi-joint).
- **playLoop main motion:** `dense := interpolateFrames(sess.Frames, interpSteps)`
  and pass `dense[1:]` to `MoveThroughJointPositions` (frame[0] already reached by
  the safe-entry move). With interpolation off, `dense == sess.Frames` and
  behavior is byte-for-byte the old path.
- **Unchanged:** safe-entry move; the gripper track (still steps the original
  `gripper_positions` at record Hz — interpolation is arm-path-only, so gripper
  sync stays best-effort as already documented).

## Why faithful, and why it doesn't slow playback

Linear joint-space interpolation makes the blended path converge to the
piecewise-linear path through the recorded samples — the closest reconstruction
the samples allow. Adding waypoints does **not** change total duration: the
driver time-parameterizes by the `MoveOptions` velocity/accel profile, so more
points only improves path tracking, not speed.


