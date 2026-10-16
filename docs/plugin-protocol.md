# Plugin protocol (version 1)

This is how the plugin runtime talks to the device. **Plugins do not use it directly.** They call `aitbase_plugin.h`, and the SDK runtime speaks this protocol.

## Who uses it

Two runtimes speak this protocol:

| Runtime | Connection | Status |
|---|---|---|
| Development | A native plugin process connects to `/tmp/aitbase_plugin_dev.sock`, mode `0600`, root only. Set `AITBASE_PLUGIN_ENDPOINT` to override the path. | Available on developer firmware (`CC_ENABLE_PLUGIN_UI=ON`, `CC_ENABLE_PLUGIN_DEV_BRIDGE=ON`). |
| Supervised (native) | The device's plugin supervisor starts the plugin in a sandbox and hands it a connected socket as an inherited descriptor, named by `AITBASE_PLUGIN_FD`. The SDK runtime uses it instead of the path and makes the process non-dumpable. The device binds the connection to the plugin identity it assigned. | Official native plugins on developer firmware with `CC_ENABLE_PLUGIN_SUPERVISOR=ON`. |
| Production (WASM) | The same supervisor, with a sandboxed WASM runner. The messages are the same; on top, the device adds permission checks and limits. | Planned. |

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
| `system` | `hello` | `protocol` (1), `api_version`, `plugin` (id from `plugin.json`) |
| `ui` | `open` | `screen`, `wait_ms` (0 to 30000), `document` (compiled screen) |
| `ui` | `update` | `node`, `key`, `type`, `value` |
| `ui` | `close` | none |

**`hello` must be the first request.** The device answers any other request sent before `hello` with `hello_required`. If the device doesn't support the protocol version, it answers `protocol_unsupported`.

Under the supervisor, the `plugin` field in `hello` must be the identity the supervisor assigned; otherwise the device answers `identity_mismatch` and stops the plugin. On the development socket it is taken as given.

## Replies (device → plugin)

```json
{"id": 7, "status": 0, "error": "", "result": {}}
```

Every request gets exactly one reply with the same `id`. `status` is `0` on success. Otherwise it is negative, and `error` holds a stable code:

| `status` | Meaning | Example codes |
|---|---|---|
| -1 | The request or its arguments are invalid. | `bad_request`, `unknown_op`, `hello_required`, `protocol_unsupported`, `identity_mismatch`, UI validation codes |
| -2 | Stale: no screen is open, or the screen is no longer current. | `not_open` |
| -3 | Busy: the update mailbox is full. | |
| -4 | Out of memory. | |

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
| `update_rejected` | An update was refused when the device applied it. | `node`, `detail` |

A message is a reply if it has an `id`, and an event if it has an `event`. Clients must ignore events and fields they don't know, so newer devices can add them.
