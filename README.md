# arm-recorder

`devrel:arm-recorder:recorder` is a Viam `sensor` component that records a configured arm's joint positions at a configurable frequency into named session JSON files, and replays those sessions by commanding the arm back through the captured positions — all driven via `DoCommand`. It is designed as a proof-of-concept demonstration tool for motion capture and replay on Viam-connected arms.

## Configuration

Add the component to your machine's config and list the arm it should control as a dependency:

```json
{
  "name": "recorder",
  "model": "devrel:arm-recorder:recorder",
  "type": "sensor",
  "attributes": {
    "arm": "my_arm",
    "frequency_hz": 10
  },
  "depends_on": ["my_arm"]
}
```

### Attributes

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `arm` | string | yes | — | Name of the arm component to record and replay |
| `frequency_hz` | number | no | `10` | Sampling rate for recording and tick rate for playback. Must not be negative; zero or omitted uses the default. |

## Readings

`Readings()` returns the current component status plus a live snapshot of the arm's joint positions:

| Key | Always present | Description |
|---|---|---|
| `state` | yes | One of `"idle"`, `"recording"`, or `"playing"` |
| `session` | yes | Name of the active or most-recently-used session (empty string when idle with no prior session) |
| `frame_count` | yes | Number of frames currently buffered in memory. Grows while recording; retains the last recorded count after `stop_recording` until the next `start_recording` clears the buffer |
| `joints` | when arm is reachable | Current joint positions as a list of float64 values (radians or degrees depending on the arm) |
| `joint_count` | when arm is reachable | Number of joints reported by the arm |
| `last_error` | when a background error occurred | Last error message from a failed joint read or playback move |
| `joints_error` | when Readings() joint read fails | Error from the in-request joint position read (does not stop the component) |

Example response during recording:

```json
{
  "state": "recording",
  "session": "my-motion",
  "frame_count": 42,
  "joints": [0.0, -0.785, 1.571, 0.0, 0.0, 0.0],
  "joint_count": 6
}
```

## DoCommand reference

All commands are sent as `{"command": "<verb>", ...args}` and return a `map[string]interface{}`.

### `start_recording`

Begin sampling the arm's joint positions at `frequency_hz` into an in-memory buffer. The component must be in the `idle` state.

**Request:**
```json
{"command": "start_recording", "session": "my-motion"}
```

**Arguments:** `session` (string, required) — name for this recording; used as the filename (`<session>.json`) when saved.

**Response:**
```json
{"status": "recording", "session": "my-motion"}
```

---

### `stop_recording`

Stop the recording loop and save buffered frames to `$VIAM_MODULE_DATA/<session>.json`. The component must be in the `recording` state.

**Request:**
```json
{"command": "stop_recording"}
```

**Response:**
```json
{"status": "saved", "session": "my-motion", "frame_count": 150}
```

---

### `play`

Load a saved session and replay it on the arm. The component first moves the arm slowly to the first recorded frame (safe-entry move), then steps through the remaining frames at the session's recorded frequency. The component must be in the `idle` state.

**Request:**
```json
{"command": "play", "session": "my-motion"}
```

**Arguments:** `session` (string, required) — name of the session to replay.

**Response:**
```json
{"status": "playing", "session": "my-motion", "frame_count": 150}
```

The response is returned immediately; playback runs in the background. Poll `Readings()` to observe progress.

---

### `stop_playback`

Abort a playback in progress and send a stop command to the arm. The component must be in the `playing` state.

**Request:**
```json
{"command": "stop_playback"}
```

**Response:**
```json
{"status": "stopped"}
```

---

### `list_sessions`

Return the names of all saved sessions in the data directory.

**Request:**
```json
{"command": "list_sessions"}
```

**Response:**
```json
{"sessions": ["my-motion", "approach-sequence", "test1"]}
```

---

### `delete_session`

Delete a saved session file from the data directory.

**Request:**
```json
{"command": "delete_session", "session": "my-motion"}
```

**Arguments:** `session` (string, required) — name of the session to delete.

**Response:**
```json
{"status": "deleted", "session": "my-motion"}
```

---

## Behavior and caveats

- **Single activity at a time.** Only one operation (recording or playback) runs at a time. Attempting to start a second while one is active returns an error.
- **Safe-entry move on playback.** Before timed replay begins, the arm is commanded to the first recorded frame via a single blocking `MoveToJointPositions` call. This allows the arm to travel at its own speed to avoid jumps. Timed playback of subsequent frames starts only after that move completes.
- **High-frequency sessions replay as fast as the arm can move.** Each `MoveToJointPositions` call blocks until the move completes. If a move takes longer than the ticker interval (common at high frequencies), the ticker drops the skipped ticks rather than queueing them. Playback degrades gracefully to "as fast as the arm can execute" rather than stalling or buffering.
- **Session storage.** Sessions are stored as `<session>.json` under `$VIAM_MODULE_DATA`. If that environment variable is not set (e.g. on a dev workstation), the component falls back to a temporary directory under `os.TempDir()` and logs a warning. Viam sets `VIAM_MODULE_DATA` automatically when the module is deployed via the Viam platform.
- **Invalid `frequency_hz` in a session file.** If a loaded session file contains a zero, negative, `NaN`, or infinite `frequency_hz`, the component logs a warning and falls back to the default (10 Hz) for that playback.
- **Session name safety.** Session names must not contain path separators (`/`, `\`) or the sequence `..`. Invalid names are rejected with an error.

## Manual validation

These steps verify the module on a real or simulated arm connected to a Viam machine. Use the **Control** tab in the [Viam app](https://app.viam.com) or the `viam` CLI to send DoCommands.

**Prerequisites:** the component is configured and the machine is online. The arm named in `attributes.arm` must be present and reachable.

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
   Observe the arm move (possibly slowly) to the first recorded frame, then step through the rest. During playback, `Readings()` shows `state: "playing"`.

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
