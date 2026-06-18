# Reactor Service — Design

**Date:** 2026-06-17
**Model:** `devrel:arm-recorder:reactor` (Viam `generic` service, Go) — second model in the existing arm-recorder module
**Status:** Approved — proof of concept
**Builds on:** the `devrel:arm-recorder:recorder` sensor (v0.1.0)

## Goal

Close a vision→action loop: a detector vision service reports object labels, and
the reactor triggers the `recorder` sensor to `play` the pre-recorded session
mapped to a detected label. This proves out vision-based feedback for
manipulation tasks — *see object X → replay the motion recorded for X.*

## Why a generic service

The reactor exposes only control verbs (arm/disarm/status), so the `generic`
service API (DoCommand-only) fits. It is registered against `generic.API` as a
second model in the same module binary.

## Configuration

```go
type Config struct {
    VisionService  string            `json:"vision_service"`   // detector — required dep
    Camera         string            `json:"camera"`           // passed to DetectionsFromCamera — required dep
    Recorder       string            `json:"recorder"`         // the v0.1.0 sensor — required dep
    LabelSessions  map[string]string `json:"label_sessions"`   // label -> session name — required, non-empty
    PollIntervalMs int               `json:"poll_interval_ms"` // default 500
    MinConfidence  float64           `json:"min_confidence"`   // default 0.5
    CooldownSec    float64           `json:"cooldown_sec"`     // default 5
}
```

`Validate` requires `vision_service`, `camera`, `recorder`, and a non-empty
`label_sessions`; rejects a negative `min_confidence`/`cooldown_sec`/interval;
returns `[vision_service, camera, recorder]` as required dependencies. Helpers
apply defaults for the three tuning fields.

Dependencies are resolved in the constructor via `vision.FromProvider(deps,
name)` and `sensor.FromProvider(deps, name)` (the recorder is a sensor; we call
only its `DoCommand`). The camera is referenced by name and passed to
`DetectionsFromCamera`; it is a required dep for startup ordering.

## Control (DoCommands) — default off

- `start_reacting` — arms the loop; rejects if already reacting.
- `stop_reacting` — cancels the loop **and sends `{"command":"stop_playback"}` to
  the recorder** to halt the arm immediately (safety).
- `status` — returns `{reacting, last_label, last_session, last_played_at,
  cooldown_remaining_sec}`. (Generic services have no `Readings`.)

## The loop (when armed)

A background goroutine ticks at `poll_interval_ms`, reusing the recorder's
concurrency discipline: a cancelable context + `done` channel, mutex never held
across a vision or recorder call. Each tick:

1. `visionSvc.DetectionsFromCamera(ctx, camera, nil)`.
2. Keep detections with `Score() >= min_confidence` whose `Label()` is a key in
   `label_sessions`.
3. If any match **and** `cooldown_sec` has elapsed since the last successful
   play: pick the **highest-confidence** match, call
   `recorder.DoCommand(ctx, {"command":"play","session": mapped})`, and stamp
   `last_played_at` (starting the cooldown).
4. If the recorder is busy (`play` errors because it is already playing) or the
   camera/vision call fails: log and skip — no cooldown stamp, so it retries on
   the next tick.

### Match selection (pure, testable)

Factor steps 2–3's selection into a pure helper:

```go
func selectSession(dets []objectdetection.Detection,
    labelSessions map[string]string, minConfidence float64) (label, session string, ok bool)
```

Returns the session for the highest-confidence detection that clears the
threshold and exists in the map. Unit-tested independent of hardware.

## Safety

- Default off; the arm never moves autonomously until `start_reacting`.
- `stop_reacting` and `Close` both cancel the loop and send `stop_playback` to
  the recorder, halting the arm.
- Global cooldown prevents replay spam; confidence threshold filters weak
  detections.
- The motion safety itself (safe-entry move, abort-stops-arm, interpolation)
  already lives in the recorder — the reactor only decides *when* to call `play`.

## Decisions (with rationale)

- **Global cooldown** (not per-label) — matches the chosen cooldown-only
  re-trigger rule; simplest observable behavior.
- **Highest-confidence wins** when several mapped labels appear in one frame.
- **`stop_reacting` halts the arm** — disarming should also stop any motion it
  may have started.

## Testing

- Unit tests: `Config.Validate` (required fields, non-empty map, defaults,
  negative rejection) and `selectSession` (confidence filter, label-map
  membership, highest-score tiebreak, no-match).
- The loop and live vision/recorder calls are validated by build + manual
  hardware testing, consistent with the recorder POC (no fake vision/sensor in
  this POC; an injectable interface + fakes is a noted future enhancement).

## Open implementation detail

Generic service registration uses `resource.RegisterService(generic.API,
Reactor, ...)` and `cmd/module/main.go` adds the second `resource.APIModel`.
Exact signatures pinned against the installed RDK (v0.131.0) during
implementation.
