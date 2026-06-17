# Model devrel:arm-recorder:recorder

Provide a description of the model and any relevant information.

## Configuration
The following attribute template can be used to configure this model:

```json
{
   "arm": "my_arm",
   "frequency_hz": 10,
   "gripper": "my_gripper",
   "gripper_position_key": "position",
   "max_velocity_rads_per_sec": 1.0,
   "max_acceleration_rads_per_sec": 0.5
}
```

### Attributes

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `arm` | string | yes | — | Name of the arm component to record and replay |
| `frequency_hz` | number | no | `10` | Sampling rate for recording and tick rate for gripper playback. Must not be negative; zero or omitted uses the default. |
| `gripper` | string | no | — | Name of a gripper component. When set, the gripper's position is recorded alongside arm frames and replayed in parallel. The gripper must also appear in `depends_on`. |
| `gripper_position_key` | string | no | `"position"` | The key in the gripper's `get_position`/`set_position` DoCommand payload that holds the numeric position value. Must be symmetric between get and set. |
| `max_velocity_rads_per_sec` | number | no | — (driver default) | Maximum joint velocity (rad/s) passed to `MoveThroughJointPositions` for the entire playback motion, including the initial safe-entry move to the first recorded frame. Omitting this leaves the value unset and uses the arm driver's default. Must not be negative. |
| `max_acceleration_rads_per_sec` | number | no | — (driver default) | Maximum joint acceleration (rad/s²) passed to `MoveThroughJointPositions` for the entire playback motion, including the initial safe-entry move to the first recorded frame. Omitting this leaves the value unset and uses the arm driver's default. Must not be negative. |

### Gripper position contract

When a `gripper` is configured, the recorder communicates with it exclusively through its own `DoCommand` interface:

- **During recording:** the recorder calls `get_position` (i.e., `{"command": "get_position"}`) on every tick. The response must contain the configured key (`gripper_position_key`) with a numeric value. Numeric values arrive as `float64` via protobuf Struct encoding.
- **During playback:** the recorder calls `set_position` with `{"command": "set_position", "<key>": <value>}` for each recorded position. The key used for set must match the key used for get — i.e., `gripper_position_key` governs both directions.

If either call fails during recording the recording stops immediately and the error is surfaced via `last_error`. If either call fails during playback the playback stops, the arm is halted, and the error is surfaced via `last_error`.

## Readings

`Readings()` returns the current component status plus a live snapshot of the arm's joint positions and, when a gripper is configured, the gripper's current position:

| Key | Always present | Description |
|---|---|---|
| `state` | yes | One of `"idle"`, `"recording"`, or `"playing"` |
| `session` | yes | Name of the active or most-recently-used session (empty string when idle with no prior session) |
| `frame_count` | yes | Number of frames currently buffered in memory. Grows while recording; retains the last recorded count after `stop_recording` until the next `start_recording` clears the buffer |
| `joints` | when arm is reachable | Current joint positions as a list of float64 values (radians or degrees depending on the arm) |
| `joint_count` | when arm is reachable | Number of joints reported by the arm |
| `has_gripper` | when a gripper is configured | Boolean `true` indicating a gripper is configured and active |
| `gripper_position` | when a gripper is configured and the live read succeeds | Current gripper position as a float64 value |
| `gripper_error` | when a gripper is configured and the live read fails | Error message from the in-request gripper position read (does not stop the component) |
| `last_error` | when a background error occurred | Last error message from a failed joint read, gripper read, or playback move |
| `joints_error` | when Readings() joint read fails | Error from the in-request joint position read (does not stop the component) |

Example response during recording with a gripper:

```json
{
  "state": "recording",
  "session": "my-motion",
  "frame_count": 42,
  "joints": [0.0, -0.785, 1.571, 0.0, 0.0, 0.0],
  "joint_count": 6,
  "has_gripper": true,
  "gripper_position": 0.5
}
```

## DoCommand reference

All commands are sent as `{"command": "<verb>", ...args}` and return a `map[string]interface{}`.

### `start_recording`

Begin sampling the arm's joint positions (and gripper position, if configured) at `frequency_hz` into an in-memory buffer. The component must be in the `idle` state.

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

Load a saved session and replay it on the arm (and gripper, if the session has gripper data and a gripper is configured). The component must be in the `idle` state.

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

