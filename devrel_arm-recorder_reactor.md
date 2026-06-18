# Model devrel:arm-recorder:reactor

The `reactor` is a Viam generic service that continuously polls a detector vision service and, when a mapped object label is detected above a configurable confidence threshold, triggers the `recorder` sensor to play the session paired with that label — providing vision-based feedback for manipulation tasks. It is designed as a proof-of-concept demonstration tool: the reactor watches what the camera sees and autonomously drives the arm through pre-recorded motions in response.

## Configuration

The following attribute template can be used to configure this model:

```json
{
  "services": [
    {
      "name": "my-reactor",
      "type": "generic",
      "model": "devrel:arm-recorder:reactor",
      "attributes": {
        "vision_service": "my-detector",
        "camera": "my-camera",
        "recorder": "my-recorder",
        "label_sessions": {
          "cup": "pick-cup",
          "bottle": "pick-bottle"
        },
        "poll_interval_ms": 500,
        "min_confidence": 0.6,
        "cooldown_sec": 5
      },
      "depends_on": ["my-detector", "my-camera", "my-recorder"]
    }
  ]
}
```

### Attributes

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `vision_service` | string | yes | — | Name of the detector vision service to query each poll cycle. Must also appear in `depends_on`. |
| `camera` | string | yes | — | Name of the camera component passed to `DetectionsFromCamera`. Must also appear in `depends_on`. |
| `recorder` | string | yes | — | Name of the `devrel:arm-recorder:recorder` sensor that receives `play` commands. Must also appear in `depends_on`. |
| `label_sessions` | object (map string → string) | yes | — | Mapping from detector label to session name. Must contain at least one entry. When a detection matches a key, the reactor plays the associated session. |
| `poll_interval_ms` | number | no | `500` | How often (in milliseconds) to call `DetectionsFromCamera`. Must not be negative; zero or omitted uses the default. |
| `min_confidence` | number | no | `0.5` | Minimum detection score required before a label is considered. Must be within `[0, 1]`. An explicit value of `0` is treated the same as omitting the field and uses the default of `0.5`. Must not be negative or greater than `1`. |
| `cooldown_sec` | number | no | `5` | Minimum seconds that must elapse between consecutive `play` calls. Prevents the reactor from re-triggering while the arm is still in motion or has just finished a playback. Must not be negative; zero or omitted uses the default. |

## Behavior and safety

**Default state: OFF.** The reactor starts idle and performs no vision queries or arm motion until `start_reacting` is sent. No autonomous motion occurs until the reactor is explicitly armed.

**Poll loop.** Once armed, the reactor fires a `DetectionsFromCamera` call against the configured camera every `poll_interval_ms` milliseconds. Each call is a `tick`:

1. Fetch detections from the vision service. On error, record the error in `last_error` and skip the tick.
2. Filter to detections whose score is at or above `min_confidence` and whose label exists in `label_sessions`. If no detection passes both filters, skip the tick.
3. Among the qualifying detections, select the one with the **highest confidence score**.
4. Check whether at least `cooldown_sec` seconds have elapsed since the last successful `play` call. If the cooldown has not elapsed, skip the tick.
5. Send `{"command": "play", "session": "<session>"}` to the recorder. If the recorder is busy or returns an error, record the error in `last_error` and skip without stamping the cooldown (the same session will be retried on the next qualifying tick). On success, update `last_label`, `last_session`, and `last_played_at`.

**Safety:** `stop_reacting` and `Close` both halt arm motion by sending `stop_playback` to the recorder immediately after the poll loop exits. A log message at debug level is emitted if the recorder is not playing at that point (benign). Note that cancelling the reactor loop does **not** by itself stop the arm — `stop_reacting` issues the explicit `stop_playback` to ensure this.

> **Warning:** once armed, the arm moves autonomously in response to whatever the camera sees. Ensure the workspace is clear and appropriate velocity/acceleration limits are set on the recorder before calling `start_reacting`.

**Motion details.** The actual arm motion (safe-entry move to the first recorded frame, followed by smooth interpolated playback through all waypoints) is performed entirely by the `recorder` service. See the [recorder documentation](./devrel_arm-recorder_recorder.md) for details on playback behavior, velocity/acceleration limits, and interpolation.

## DoCommand reference

All commands are sent as `{"command": "<verb>"}` and return a `map[string]interface{}`.

### `start_reacting`

Arm the reactor and start the poll loop. Returns an error if the reactor is already reacting.

**Request:**
```json
{"command": "start_reacting"}
```

**Response:**
```json
{"status": "reacting"}
```

---

### `stop_reacting`

Disarm the reactor, stop the poll loop, and send `stop_playback` to the recorder to halt any arm motion that may have been triggered. Returns an error if the reactor is not currently reacting.

**Request:**
```json
{"command": "stop_reacting"}
```

**Response:**
```json
{"status": "stopped"}
```

---

### `status`

Return the current reactor state. The `reacting` key is always present. The keys `last_label`, `last_session`, `last_played_at`, and `cooldown_remaining_sec` are present only after at least one successful `play` has been triggered in the current or a prior reacting session. The key `last_error` is present only when a background error (from a failed detection call or a failed `play` call) has been recorded and not yet cleared. It is cleared when the loop is re-armed with `start_reacting`; a later successful tick does not clear it.

**Request:**
```json
{"command": "status"}
```

**Response (idle, no prior plays):**
```json
{"reacting": false}
```

**Response (reacting, after a successful play, with an active cooldown):**
```json
{
  "reacting": true,
  "last_label": "cup",
  "last_session": "pick-cup",
  "last_played_at": "2026-06-17T14:23:01Z",
  "cooldown_remaining_sec": 3.2
}
```

**Response (with a background error):**
```json
{
  "reacting": true,
  "last_label": "cup",
  "last_session": "pick-cup",
  "last_played_at": "2026-06-17T14:23:01Z",
  "cooldown_remaining_sec": 0,
  "last_error": "recorder busy: already playing"
}
```

| Key | Always present | Description |
|---|---|---|
| `reacting` | yes | `true` when the poll loop is running |
| `last_label` | after first successful play | Detector label that triggered the most recent play |
| `last_session` | after first successful play | Session name that was played |
| `last_played_at` | after first successful play | RFC 3339 UTC timestamp of the most recent successful play call |
| `cooldown_remaining_sec` | after first successful play | Seconds remaining in the cooldown window; `0` when the cooldown has elapsed |
| `last_error` | when a background error has occurred | Most recent error from a failed detection call or failed `play` call |
