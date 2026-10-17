# Capabilities

<!-- Generated from the device's capability registry. Do not edit. -->

What a plugin can ask the device to do. Plugins call `aitbase_plugin.h`; this page lists the
protocol operations behind it, the limits the device enforces, and every error code.

## Grants

List the capabilities your plugin needs in `requested_capabilities` in `plugin.json`. An
operation whose capability isn't listed fails with `no_grant`. During the preview the device
grants a plugin everything it requests; owner approval of sensitive ones comes later.

| Capability | Class | Rate limit | What it allows |
|---|---|---|---|
| `ui` | standard | none | Show the plugin's own screens and update them. Always granted. |
| `storage` | standard | 20/s, bursts of 40 | Keep small text values across runs, in a store only this plugin can reach. Needs an SD card. |
| `http` | sensitive | 1/s, bursts of 5 | Make HTTP and HTTPS requests to public internet hosts. Local network, loopback and reserved addresses are refused. |
| `audio.output` | sensitive | 5/s, bursts of 10 | Play tones and the package's own sounds on the speaker, only while the plugin's screen is visible. |

## Operations

### `ui.open`

Needs `ui`. Opens a compiled screen. With wait_ms, waits up to that long for the device to become idle (voice start, D11).

| Argument | Type | Bounds |
|---|---|---|
| `screen` | string | at most 32 bytes; charset `name`; optional |
| `wait_ms` | int | 0 to 30000; optional |
| `document` | object |  |

### `ui.update`

Needs `ui`. Sets a bound value on the open screen.

| Argument | Type | Bounds |
|---|---|---|
| `node` | string | at most 72 bytes |
| `key` | string | at most 32 bytes |
| `type` | string | at most 16 bytes |
| `value` | string | at most 1024 bytes |

### `ui.close`

Needs `ui`. Closes the open screen.

### `storage.get`

Needs `storage`. Reads a value. The result has found (true or false) and value.

| Argument | Type | Bounds |
|---|---|---|
| `key` | string | at most 64 bytes; charset `key` |

### `storage.set`

Needs `storage`. Stores a text value under a key, replacing any earlier value. Values may contain tabs and newlines, but no other control characters.

| Argument | Type | Bounds |
|---|---|---|
| `key` | string | at most 64 bytes; charset `key` |
| `value` | string | at most 4096 bytes; charset `text` |

### `storage.delete`

Needs `storage`. Removes a key. Removing a missing key succeeds.

| Argument | Type | Bounds |
|---|---|---|
| `key` | string | at most 64 bytes; charset `key` |

### `storage.keys`

Needs `storage`. Lists the stored keys. The result has keys, an array of strings.

### `http.request`

Needs `http`. Makes one HTTP request and returns status, content_type, body (text, cut to fit) and truncated. Redirects are returned, not followed. One request at a time.

| Argument | Type | Bounds |
|---|---|---|
| `method` | enum | `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD` |
| `url` | string | at most 1024 bytes; charset `url` |
| `headers` | object | optional |
| `body` | string | at most 8192 bytes; optional |
| `timeout_ms` | int | 1000 to 15000; optional |

### `audio.tone`

Needs `audio.output`. Plays a sine tone, replacing any sound playing. A sound_done event follows.

| Argument | Type | Bounds |
|---|---|---|
| `freq_hz` | int | 100 to 4000 |
| `duration_ms` | int | 10 to 2000 |
| `volume` | int | 0 to 100; optional |

### `audio.play`

Needs `audio.output`. Plays sounds/<sound>.wav from the package (16 kHz, mono, 16-bit PCM, at most 10 s), replacing any sound playing. A sound_done event follows.

| Argument | Type | Bounds |
|---|---|---|
| `sound` | string | at most 32 bytes; charset `name` |
| `volume` | int | 0 to 100; optional |

### `audio.stop`

Needs `audio.output`. Stops the sound playing, if any.

Charsets: `name` is `[a-z][a-z0-9_]*`; `key` is letters, digits, `.`, `_` and `-`; `text` is
UTF-8 without control characters other than tab, newline and carriage return; `url` is printable
ASCII without spaces.

## Events

| Capability | Event | Meaning |
|---|---|---|
| `ui` | `opened` | The screen is visible. |
| `ui` | `rejected` | The open was refused, cancelled by another device state, or timed out. data.detail says why. |
| `ui` | `closed` | The screen was closed after a close request. |
| `ui` | `preempted` | The device took the screen back: LEFT long press, voice assistant, settings. Any sound stops. |
| `ui` | `action` | The user activated a focusable widget with RIGHT. data.node and data.action. |
| `ui` | `update_rejected` | An update was refused when the device applied it. data.node and data.detail. |
| `audio` | `sound_done` | The sound started by data.request finished, was replaced or stopped. data.detail is finished, stopped or failed. |

## Status codes

| `status` | Name | Meaning |
|---|---|---|
| -1 | `invalid` | The request or its arguments are invalid. |
| -2 | `stale` | No screen is open, or the screen is no longer current. |
| -3 | `busy` | The device can't take this request now; try again later. |
| -4 | `no_memory` | The device ran out of memory for this request. |
| -5 | `denied` | The plugin may not do this: a missing grant, or a destination the device refuses. |
| -6 | `unavailable` | Not available now or on this device: no SD card, or the screen isn't visible. |
| -7 | `limit` | A rate, size or storage limit was reached. |
| -8 | `failed` | The device tried and failed: network error, timeout, TLS error. |

## Error codes

| Code | Status | Meaning |
|---|---|---|
| `bad_request` | -1 | Malformed request, or an argument is missing, of the wrong type, out of range or too long. |
| `unknown_op` | -1 | The device doesn't know this capability or operation. |
| `hello_required` | -1 | system.hello must be the first request. |
| `protocol_unsupported` | -1 | The device doesn't support this protocol or API version. |
| `identity_mismatch` | -1 | hello claimed an id other than the one the device started; the plugin is stopped. |
| `bad_header` | -1 | An HTTP header is malformed, or one the device sets itself (Host, Content-Length, Connection, Transfer-Encoding, Proxy-*). |
| `sound_not_found` | -1 | The package has no sounds/<name>.wav. |
| `bad_sound` | -1 | The sound isn't a 16 kHz, mono, 16-bit PCM WAV file within the size limit. |
| `not_open` | -2 | No screen is open. |
| `in_flight` | -3 | An HTTP request is already running; wait for its result. |
| `no_memory` | -4 | The device ran out of memory. |
| `no_grant` | -5 | plugin.json doesn't list the capability this operation needs in requested_capabilities. |
| `destination_blocked` | -5 | The URL isn't http or https, or its host resolves to a loopback, private, link-local or other non-public address. |
| `not_foreground` | -6 | Sound plays only while the plugin's screen is visible. |
| `no_storage` | -6 | Plugin storage isn't available: no SD card, or it can't be written. |
| `rate_limited` | -7 | Too many requests of this kind; slow down. |
| `quota_exceeded` | -7 | Plugin storage is full. |
| `too_large` | -7 | The result is larger than the plugin can receive. |
| `timeout` | -8 | The HTTP request didn't finish in time. |
| `network_error` | -8 | DNS, connection or transfer failure. |
| `tls_error` | -8 | The server's TLS certificate couldn't be verified. |

## Limits

| Limit | Value |
|---|---|
| `rx_default` | 4096 |
| `rx_max` | 16384 |
| `storage_key_max` | 64 |
| `storage_value_max` | 4096 |
| `storage_keys_max` | 128 |
| `storage_total_max` | 65536 |
| `http_url_max` | 1024 |
| `http_headers_max` | 16 |
| `http_header_max` | 512 |
| `http_body_max` | 8192 |
| `http_response_max` | 65536 |
| `http_timeout_ms_default` | 10000 |
| `http_timeout_ms_max` | 15000 |
| `audio_sound_bytes_max` | 327680 |
| `audio_sample_rate` | 16000 |
