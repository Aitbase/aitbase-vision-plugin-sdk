# Plugin protocol (version 1)

This is how the plugin runtime talks to the device. **Plugins do not use it directly.** They call `aitbase_plugin.h`, and the SDK runtime speaks this protocol.

## Who uses it

Two runtimes speak this protocol:

| Runtime | Connection | Status |
|---|---|---|
| Development socket | A native plugin process connects to `/tmp/aitbase_plugin_dev.sock`. Set `AITBASE_PLUGIN_ENDPOINT` to override the path. | Removed from device firmware; the SDK's native runtime still supports it for older developer firmware. |
| Supervised (native) | The device's plugin supervisor starts the plugin in a sandbox and hands it a connected socket as an inherited descriptor, named by `AITBASE_PLUGIN_FD`. The SDK runtime uses it instead of the path and makes the process non-dumpable. The device binds the connection to the plugin identity it assigned. | Official native plugins, shipped in firmware. |
| Supervised (WASM) | The same supervisor starts a sandboxed WASM runner, which hosts the plugin module and passes the same messages through the [WASM host ABI](wasm-abi.md). | Preview, in current firmware. Community plugins are signed packages on the SD card; they hold every capability they request. |
| Production (WASM) | As supervised WASM, from signed `.avp` packages the device verifies ([package format](package-format.md)), plus owner approval of sensitive capabilities. | Signed packages: current firmware. Approval: planned. |

The device serves one plugin connection at a time. When a plugin disconnects, the device closes that plugin's screen.

## Framing

Each message is one UTF-8 JSON object per line (`\n`). A message from the plugin may be at most 20 KB.

## Requests (plugin → device)

```json
{"id": 7, "cap": "ui", "op": "update", "args": {"node": "status.title", "key": "title", "type": "string", "value": "Count: 1"}}
```

- `id` is a positive integer, chosen by the plugin and unique per connection.
- `cap` names the capability; `op` and `args` depend on it.

| `cap` | `op` | `args` |
|---|---|---|
| `system` | `hello` | `protocol` (1), `api_version`, `plugin` (id from `plugin.json`), `rx_max` (optional: the longest message the plugin can receive, 4096 to 16384; default 4096) |
| `ui` | `open` | `screen`, `wait_ms` (0 to 30000), `document` (compiled screen) |
| `ui` | `update` | `node`, `key`, `type`, `value` |
| `ui` | `close` | none |
| `storage` | `get`, `set`, `delete`, `keys` | `key`, and `value` for `set` |
| `http` | `request` | `method`, `url`, `headers`, `body`, `timeout_ms` |
| `audio` | `tone`, `play`, `stop` | `freq_hz`, `duration_ms`, `volume`; `sound`, `volume` |
| `tool` | `reply` | `call` (the `tool.call` event's id), `text` (at most 2048 bytes) |

Every operation, its arguments and limits, and the capability it needs are in [capabilities](capabilities.md), generated from the device's capability registry. The device checks each request there before doing anything: unknown arguments, wrong types and values out of range are `bad_request`, and an operation only firmware feature modules may use is `module_only` (`no_screen` for a module's `ui`). Since device decision D32 nothing is requested: devices before it answered `no_grant` for an operation whose capability wasn't in `requested_capabilities`.

**`hello` must be the first request.** The device answers any other request sent before `hello` with `hello_required`. If the device doesn't support the protocol version, it answers `protocol_unsupported`.

Under the supervisor, the `plugin` field in `hello` must be the identity the supervisor assigned; otherwise the device answers `identity_mismatch` and stops the plugin. On the development socket it is taken as given.

The `hello` reply's result has `protocol`, `api_version`, the `rx_max` the device will respect, `grants` (what the session can use; SDK 0.11 ignores it, and devices may stop sending it), and `trigger`, why the device started the plugin: `autorun`, `voice`, `shortcut`, `schedule` or `assistant` (absent on older devices).

## Replies (device → plugin)

```json
{"id": 7, "status": 0, "error": "", "result": {}}
```

Every request gets exactly one reply with the same `id`. Most replies come at once; an `http.request` reply comes when the request finishes, and other replies may arrive in between. `status` is `0` on success, and `result` holds what the operation returns. Otherwise `status` is negative, and `error` holds a stable code:

| `status` | Meaning | Example codes |
|---|---|---|
| -1 | The request or its arguments are invalid. | `bad_request`, `unknown_op`, `hello_required`, `protocol_unsupported`, `identity_mismatch`, `bad_header`, UI validation codes |
| -2 | Stale: no screen is open, or the screen is no longer current. | `not_open` |
| -3 | Busy: the update mailbox is full, or a request of the same kind is running. | `in_flight` |
| -4 | Out of memory. | |
| -5 | Denied. | `module_only`, `destination_blocked` |
| -6 | Unavailable now or on this device. | `not_foreground`, `no_storage` |
| -7 | A rate, size or storage limit. | `rate_limited`, `quota_exceeded`, `too_large` |
| -8 | The device tried and failed. | `timeout`, `network_error`, `tls_error` |

The device never sends a message longer than the plugin's `rx_max`. A result that wouldn't fit is cut where the operation allows it (an HTTP body, with `truncated`), or replaced by `too_large`.

## Events (device → plugin)

```json
{"cap": "ui", "event": "action", "data": {"node": "increment", "action": "plugin.increment", "detail": ""}}
```

| `event` | Meaning | `data` |
|---|---|---|
| `opened` | The screen is visible. | |
| `rejected` | The open was refused, cancelled by another device state, or timed out. | `detail` |
| `closed` | The screen was closed after a `close` request. | |
| `preempted` | The device took the screen back: LEFT long press, voice assistant, settings… | |
| `action` | The user activated a focusable widget with RIGHT. | `node`, `action` |
| `key` | A button was pressed while the plugin's screen shows: LEFT press, RIGHT press or RIGHT long press, before the `action` of the same press. A long press comes after a press of the same button; LEFT long press is never sent. | `key`: `left` or `right`; `gesture`: `press` or `long_press` |
| `update_rejected` | An update was refused when the device applied it. | `node`, `detail` |

`cap` `audio`:

| `event` | Meaning | `data` |
|---|---|---|
| `sound_done` | The sound started by request `request` ended. | `request`, `detail`: `finished`, `stopped` or `failed` |
| `stream_state` | The audio stream changed (D28, since SDK 0.10). | `state`: `paused`, `resumed` or `stopped`; for `stopped`, `reason`: `closed`, `preempted`, `replaced`, `idle_timeout`, `bad_stream` or `failed` |

`cap` `schedule` and `module` (firmware feature modules only; community plugins never receive them):

| `event` | Meaning | `data` |
|---|---|---|
| `schedule.due` | An entry the plugin set with `schedule.set` is due. Ring it with `schedule.ring`, or the device rings it itself after a short grace period. | `id` |
| `module.action` | A declared action of the module's screen. | `action`: `snooze` or `stop` |

`cap` `tool` (assistant tools, D31, since SDK 0.10; plugins that request `assistant.tools`):

| `event` | Meaning | `data` |
|---|---|---|
| `call` | The voice assistant calls one of the tools in the manifest's `assistant_tools`. Answer once with `tool.reply` within 5 s; a second reply, a late one or one to another call is `tool_unknown`. The C runtime delivers it as `AITBASE_EVENT_TOOL_CALL`. | `call` (a number), `tool`, `args` (an object, already checked against the declaration) |

A message is a reply if it has an `id`, and an event if it has an `event`. Clients must ignore events and fields they don't know, so newer devices can add them. Since SDK 0.10, the C runtime delivers events of capabilities other than `ui`, `audio` and `tool` as `AITBASE_EVENT_DEVICE`: `op` is `"<cap>.<event>"` and `text` is `data` as JSON.

Since SDK 0.10, `aitbase_call()` sends a request and waits for its reply in the native runtime; messages that arrive meanwhile are delivered afterwards, in order. The WASM runtime returns `AITBASE_ERR_UNSUPPORTED`, because the [WASM host ABI](wasm-abi.md) has no blocking receive.

## Audio channel (D28)

Audio samples don't travel in the messages above. A plugin whose `plugin.json` requests `audio.stream` gets a second socket from the device's plugin supervisor, on **descriptor 4** (`AITBASE_LIMIT_AUDIO_STREAM_FD`), next to the message channel. After `audio.stream_open {rate}` succeeds, the plugin sends frames on it:

- 4 bytes: the payload length, little-endian;
- the payload: 16-bit little-endian mono PCM at the stream's rate, 2 to 8192 bytes, an even count.

The device reads only as fast as the speaker plays, so a plugin that sends faster finds the socket full (`EAGAIN`, or `aitbase_audio_stream_write` returning 0). While the stream is `paused` nothing is read. A malformed frame, or a frame while no stream is open, ends the stream with `bad_stream` and closes the channel for the rest of the run. Frames already in flight when the device stops a stream (`preempted`, `replaced`, `idle_timeout`) are dropped. `audio.stream_close` plays out the frames already sent, then reports `stopped` with `closed`.

WASM plugins don't see the descriptor: the runner's `audio_write` import ([WASM ABI](wasm-abi.md)) writes the frames. A plugin without the capability has no descriptor 4.
