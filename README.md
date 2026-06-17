# arm-recorder

`devrel:arm-recorder:recorder` is a Viam `sensor` component that records a configured arm's joint positions at a configurable frequency into named session JSON files, and replays those sessions by commanding the arm back through the captured positions — all driven via `DoCommand`. It is designed as a proof-of-concept demonstration tool for motion capture and replay on Viam-connected arms. Optionally, it also records and replays a gripper's position in parallel with the arm.

## Configuration

See the [recorder documentation](./devrel_arm-recorder_recorder.md) for information about that resource.

## Behavior and caveats

- **Single activity at a time.** Only one operation (recording or playback) runs at a time. Attempting to start a second while one is active returns an error.
- **Playback engine — two-phase motion.**
  1. **Safe-entry move.** The arm is commanded to the first recorded frame via a single-waypoint `MoveThroughJointPositions` call so that the configured `max_velocity_rads_per_sec`/`max_acceleration_rads_per_sec` limits apply. If a gripper is in use, it is simultaneously set to its first recorded position. Both run concurrently; an error in either aborts the entry and stops the arm. **Safety note:** when no velocity/acceleration limits are configured, this initial move runs at the arm driver's default speed and can be a large sweep from the arm's current pose — configure limits (or manually position the arm near the start pose) before testing on hardware.
  2. **Smooth main motion.** The remaining frames are passed to a single `MoveThroughJointPositions` call, which blends through all waypoints in one continuous motion. If a gripper is in use, its positions are stepped on a parallel ticker running at the session's recorded `frequency_hz`.
- **Playback duration is governed by arm speed, not wall clock.** `max_velocity_rads_per_sec` and `max_acceleration_rads_per_sec` apply to the **entire** playback motion — including the initial safe-entry move to the first recorded frame — when those attributes are set; otherwise the arm driver's defaults apply to all moves. Total playback duration will therefore differ from recording duration. Gripper sync is best-effort wall-clock aligned to the recording frequency — it may drift relative to arm motion.
- **Playback failures stop the arm.** Any error during playback (arm motion, gripper command, or context cancellation from `stop_playback`/`Close`) causes the arm to be halted immediately. Internal failures (not user-requested stops) are surfaced in `Readings()` as `last_error`.
- **Session storage.** Sessions are stored as `<session>.json` under `$VIAM_MODULE_DATA`. If that environment variable is not set (e.g. on a dev workstation), the component falls back to a temporary directory under `os.TempDir()` and logs a warning. Viam sets `VIAM_MODULE_DATA` automatically when the module is deployed via the Viam platform.
- **Invalid `frequency_hz` in a session file.** If a loaded session file contains a zero, negative, `NaN`, or infinite `frequency_hz`, the component logs a warning and falls back to the default (10 Hz) for that playback.
- **Session name safety.** Session names must not contain path separators (`/`, `\`) or the sequence `..`. Invalid names are rejected with an error.
- **Session without gripper data.** If a session was recorded without a gripper (or is an older file without gripper fields) but a gripper is currently configured, playback proceeds arm-only and a log message is emitted. If a session has gripper data but no gripper is configured, playback is rejected with an error.

## Session file format

Session files are JSON written to `$VIAM_MODULE_DATA/<session>.json`. The top-level fields are:

| Field | Type | Description |
|---|---|---|
| `session` | string | Session name |
| `frequency_hz` | number | Recording sample rate; also used as the gripper ticker rate during playback |
| `recorded_at` | string | RFC 3339 UTC timestamp of when the session was saved |
| `joint_count` | number | Number of joints per frame |
| `frames` | array of arrays | Arm joint positions; each element is a `[]float64` of length `joint_count` |
| `has_gripper` | bool | `true` when gripper data was recorded (omitted / falsy in older files) |
| `gripper_position_key` | string | The DoCommand key used during recording (omitted in older files) |
| `gripper_positions` | array of numbers | Gripper positions parallel to `frames`; one entry per frame (omitted in older files) |

Older session files that predate gripper support load correctly — the absence of `has_gripper`, `gripper_position_key`, and `gripper_positions` is treated as no gripper data, and playback proceeds arm-only.

## Manual validation

These steps verify the module on a real or simulated arm connected to a Viam machine. Use the **Control** tab in the [Viam app](https://app.viam.com) or the `viam machine part run` CLI to send DoCommands.

**Prerequisites:** the component is configured and the machine is online. The arm named in `attributes.arm` must be present and reachable. If validating gripper support, the gripper must also be configured and reachable, listed in both `attributes.gripper` and `depends_on`.

### Arm-only validation

1. **Confirm `Readings()` returns live joints.**
   Open the Control tab, find the `recorder` sensor, and click **Get Readings**. Verify `state` is `"idle"` and `joints` contains the expected number of joint positions.

2. **Record a session.**
   In the DoCommand panel, send:
   ```json
   {"command": "start_recording", "session": "test1"}
   ```
   Jog the arm to several positions (via the arm's own control panel or by physically moving a back-drivable arm). Then send:
   ```json
   {"command": "stop_recording"}
   ```
   Verify the response shows a non-zero `frame_count`. Optionally inspect `$VIAM_MODULE_DATA/test1.json` on the machine to confirm the file was written.

3. **List sessions.**
   ```json
   {"command": "list_sessions"}
   ```
   Confirm `"test1"` appears in the response.

4. **Move the arm away from the recorded start position.**
   Use the arm's control panel to jog it to a different pose so the safe-entry move is observable.

5. **Replay the session.**
   ```json
   {"command": "play", "session": "test1"}
   ```
   Observe the arm move to the first recorded frame (safe-entry), then execute a single smooth blended motion through the remaining frames. During playback, `Readings()` shows `state: "playing"`. After completion, `state` returns to `"idle"`.

6. **Stop playback early (optional).**
   While playback is running, send:
   ```json
   {"command": "stop_playback"}
   ```
   Confirm the arm stops promptly and `Readings()` returns to `state: "idle"`.

7. **Delete the session.**
   ```json
   {"command": "delete_session", "session": "test1"}
   ```
   Confirm `list_sessions` no longer includes `"test1"`.

### Gripper validation

These steps assume the module is configured with a `gripper` attribute pointing to a gripper component that implements `get_position` and `set_position` via `DoCommand`.

1. **Confirm gripper appears in `Readings()`.**
   Click **Get Readings** and verify `has_gripper` is `true` and `gripper_position` contains a numeric value. If `gripper_error` appears instead, check that the gripper component is reachable and implements the expected DoCommand keys.

2. **Record a session with gripper.**
   ```json
   {"command": "start_recording", "session": "grip-test"}
   ```
   Move the arm and open/close the gripper to several positions. Then:
   ```json
   {"command": "stop_recording"}
   ```
   Inspect `$VIAM_MODULE_DATA/grip-test.json` and confirm the file contains `"has_gripper": true` and a `gripper_positions` array of the same length as `frames`.

3. **Move to a different configuration.**
   Jog the arm away from the recorded start and set the gripper to a different position so that the safe-entry move is visible on both.

4. **Replay the session.**
   ```json
   {"command": "play", "session": "grip-test"}
   ```
   Observe the arm and gripper both move to their first recorded positions concurrently (safe entry). Then watch the arm execute its smooth blended motion while the gripper steps through its recorded positions. Check `Readings()` during playback — `gripper_position` should update as playback proceeds.

5. **Verify no errors.**
   After playback completes, confirm `Readings()` does not contain `last_error`. If it does, the error message describes which phase failed.

### Using the Viam CLI

You can also drive DoCommands from the terminal using `viam machine part run`:

```bash
viam machine part run \
  --machine <machine-id> \
  --part <part-id> \
  --component recorder \
  do-command '{"command":"list_sessions"}'
```

Replace `<machine-id>`, `<part-id>`, and `recorder` with your machine's values.
