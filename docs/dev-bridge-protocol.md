# Development bridge protocol (v0)

This is the transport behind `include/aitbase_plugin.h` in SDK v0. **Plugins should call the C API, not this protocol**, because the planned WASM runtime replaces this transport while keeping the API.

## Where it runs

The bridge only exists in device firmware built with the development options `CC_ENABLE_PLUGIN_UI=ON` and `CC_ENABLE_PLUGIN_DEV_BRIDGE=ON`. It listens on `/tmp/aitbase_plugin_dev.sock`, with mode `0600`, and serves one plugin at a time.

It is a development tool, not the production plugin boundary:
- there is no sandbox, per-plugin user or capability broker behind it;
- release firmware does not include it.

The client uses the `AITBASE_PLUGIN_ENDPOINT` environment variable, if set, as the socket path.

## Framing

The protocol is UTF-8 JSON, one object per line (`\n`). Each request line must be at most 20 KB.

## Requests (plugin → device)

```json
{"op":"open","wait_ms":30000,"ui":{ ...compiled aitbase.plugin-ui document... }}
{"op":"update","node":"status.title","key":"title","type":"string","value":"Count: 1"}
{"op":"close"}
```

## Replies and events (device → plugin)

Every request gets a reply:

```json
{"ev":"reply","op":"open","status":0,"gen":3,"error":""}
```

`status` is `0` on success. Otherwise it is negative, and `error` holds a stable code:
- `-1` invalid;
- `-2` stale or not open;
- `-3` busy (update mailbox full);
- `-4` out of memory.

The device also sends these events:

| `ev` | Meaning | Fields |
|---|---|---|
| `opened` | The screen is visible. | |
| `rejected` | The open was refused, cancelled by another device state, or timed out. | `detail` |
| `closed` | The screen was closed after a `close` request. | |
| `preempted` | The device took the screen back: LEFT long press, voice assistant, settings… | |
| `action` | The user activated a focusable widget with RIGHT. | `node`, `action` |
| `update_rejected` | An update was refused when applied. | `node`, `detail` |

The device assigns `gen` (the session generation) and only sends events for the current session. When the plugin disconnects, the device closes its screen.

Clients must ignore event names they don't know, so newer devices can add events.
