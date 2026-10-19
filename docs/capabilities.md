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
| `schedule` | sensitive | 1/s, bursts of 5 | Ring the device at set times, with the entry's label and a built-in sound; the device shows its own alarm screen, titled with the plugin's name. A community plugin needs the owner's "Allow alarms" on the Device Console (D26) and holds at most 4 entries, 5 minutes apart. |
| `settings` | official | 10/s, bursts of 20 | Keep the module's settings on internal flash. Firmware feature modules only (D25). Not available to community plugins: `check` reports `CAPABILITY_FIRMWARE_ONLY`. |
| `status` | official | 10/s, bursts of 20 | Publish the module's status for the device and other modules. Firmware feature modules only (D25). Not available to community plugins: `check` reports `CAPABILITY_FIRMWARE_ONLY`. |
| `idle.face` | standard | 1/s, bursts of 5 | Fill in the plugin's idle face (D22), the home screen face the owner may choose on the Device Console. |
| `audio.stream` | sensitive | 5/s, bursts of 10 | Stream continuous audio to the speaker, such as a radio or a podcast, only while the plugin's screen is visible (D28). Alarms, calls and the voice assistant pause it. |

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

### `ui.template`

Needs `ui`. Opens one of the device's screen templates with the plugin's text (D27): status (title, optional emoji, an OK button), alert (an icon, title, OK) or menu (title and 1 to 4 items). RIGHT on OK gives the action template.ok; RIGHT on item N gives template.itemN. Like open, it replaces the plugin's screen, waits like open with wait_ms, and is preempted by the device.

| Argument | Type | Bounds |
|---|---|---|
| `template` | enum | `status`, `alert`, `menu` |
| `title` | string | at most 48 bytes; charset `label` |
| `emoji` | string | at most 8 bytes; charset `label`; optional |
| `icon` | enum | `bell`, `info`, `warning`; optional |
| `item1` | string | at most 24 bytes; charset `label`; optional |
| `item2` | string | at most 24 bytes; charset `label`; optional |
| `item3` | string | at most 24 bytes; charset `label`; optional |
| `item4` | string | at most 24 bytes; charset `label`; optional |
| `wait_ms` | int | 0 to 30000; optional |

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

### `audio.stream_open`

Needs `audio.stream`. Opens the plugin's audio stream at rate Hz (D28): 16-bit mono PCM frames then go on the audio channel (aitbase_audio_stream_write). Stops any sound playing. stream_state events follow.

| Argument | Type | Bounds |
|---|---|---|
| `rate` | int | 8000 to 48000 |

### `audio.stream_close`

Needs `audio.stream`. Plays out the frames already sent, then closes the stream. A stream_state event with state stopped and reason closed follows.

### `schedule.set`

Needs `schedule`. Adds or replaces the plugin's entry with this id. A ringing entry makes the device ring at due_epoch with its label, even if the plugin isn't running then (D23).

| Argument | Type | Bounds |
|---|---|---|
| `id` | string | at most 14 bytes; charset `name` |
| `due_epoch` | int | 1 to 2147483647 |
| `rings` | bool |  |
| `label` | string | at most 48 bytes; charset `label` |
| `sound` | enum | `alarm`; optional |

### `schedule.cancel`

Needs `schedule`. Removes the plugin's entry with this id. The result has found.

| Argument | Type | Bounds |
|---|---|---|
| `id` | string | at most 14 bytes; charset `name` |

### `schedule.list`

Needs `schedule`. The plugin's own entries: entries is a list of {id, due_epoch, rings, label}.

### `schedule.ring`

Needs `schedule`. Rings the entry that schedule.due just announced, with its label. Without it the device rings anyway after a short grace period (C14).

| Argument | Type | Bounds |
|---|---|---|
| `id` | string | at most 14 bytes; charset `name` |

### `settings.get`

Needs `settings`. The module's settings: found, and values, an object of string values.

### `settings.set`

Needs `settings`. Replaces the module's settings: at most 64 keys of [a-z0-9_] up to 32 bytes, string values up to 512 bytes.

| Argument | Type | Bounds |
|---|---|---|
| `values` | object |  |

### `settings.erase`

Needs `settings`. Removes the module's settings.

### `status.publish`

Needs `status`. Publishes a status value under the module's own prefix: key is <module>.<name>.

| Argument | Type | Bounds |
|---|---|---|
| `key` | string | at most 48 bytes; charset `key` |
| `value` | string | at most 256 bytes; charset `text` |

### `face.set`

Needs `idle.face`. Sets a value of the plugin's idle face (D22): the labels its face document binds to key. The device keeps the values, so the face shows them after a reboot without the plugin running. Works whether or not the owner has chosen the face.

| Argument | Type | Bounds |
|---|---|---|
| `key` | enum | `face.1`, `face.2`, `face.3`, `face.4`, `face.5`, `face.6`, `face.7`, `face.8`, `face.9`, `face.10`, `face.11`, `face.12`, `face.13`, `face.14`, `face.15`, `face.16` |
| `value` | string | at most 64 bytes; charset `label` |

Charsets: `name` is `[a-z][a-z0-9_]*`; `key` is letters, digits, `.`, `_` and `-`; `text` is
UTF-8 without control characters other than tab, newline and carriage return; `url` is printable
ASCII without spaces; `label` is one line of UTF-8 text without control, bidirectional-override
or zero-width characters.

## Events

| Capability | Event | Meaning |
|---|---|---|
| `ui` | `opened` | The screen is visible. |
| `ui` | `rejected` | The open was refused, cancelled by another device state, or timed out. data.detail says why. |
| `ui` | `closed` | The screen was closed after a close request. |
| `ui` | `preempted` | The device took the screen back: LEFT long press, voice assistant, settings. Any sound stops. |
| `ui` | `action` | The user activated a focusable widget with RIGHT. data.node and data.action. |
| `ui` | `key` | A button was pressed while the plugin's screen shows: LEFT press, RIGHT press or RIGHT long press (a long press comes after a press of the same button). data.key is left or right, data.gesture is press or long_press. LEFT long press is never delivered: it leaves the plugin. |
| `ui` | `update_rejected` | An update was refused when the device applied it. data.node and data.detail. |
| `audio` | `sound_done` | The sound started by data.request finished, was replaced or stopped. data.detail is finished, stopped or failed. |
| `audio` | `stream_state` | The audio stream changed (D28). data.state is paused (an alarm, a call or the assistant took the speaker; frames wait), resumed, or stopped, with data.reason: closed, preempted (the screen closed), replaced (music or another stream started), idle_timeout (no frame for 10 s), bad_stream (a malformed frame, or a frame while no stream is open; the channel stays closed for this run) or failed (the speaker failed). |
| `schedule` | `due` | An entry of this plugin is due. data.id names it; ring it with schedule.ring if it rings. |
| `module` | `action` | A declared action of the module's screen. data.action is snooze or stop. |

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
| `bad_rate` | -1 | The stream rate isn't one the speaker plays: 8000, 11025, 12000, 16000, 22050, 24000, 32000, 44100 or 48000 Hz. |
| `not_open` | -2 | No screen is open. |
| `in_flight` | -3 | An HTTP request is already running; wait for its result. |
| `speaker_busy` | -3 | An alarm, a call or the voice assistant holds the speaker, or another plugin is streaming (D28). |
| `streaming` | -3 | The plugin's audio stream is open; sounds wait until it closes. |
| `no_memory` | -4 | The device ran out of memory. |
| `no_grant` | -5 | plugin.json doesn't list the capability this operation needs in requested_capabilities. |
| `destination_blocked` | -5 | The URL isn't http or https, or its host resolves to a loopback, private, link-local or other non-public address. |
| `not_foreground` | -6 | Sound plays only while the plugin's screen is visible. |
| `no_channel` | -6 | The plugin has no working audio channel: it doesn't hold audio.stream, or an earlier malformed frame ended the channel for this run (D28). |
| `no_storage` | -6 | Plugin storage isn't available: no SD card, or it can't be written. |
| `rate_limited` | -7 | Too many requests of this kind; slow down. |
| `quota_exceeded` | -7 | Plugin storage is full. |
| `too_large` | -7 | The result is larger than the plugin can receive. |
| `timeout` | -8 | The HTTP request didn't finish in time. |
| `network_error` | -8 | DNS, connection or transfer failure. |
| `tls_error` | -8 | The server's TLS certificate couldn't be verified. |
| `module_only` | -5 | Only a firmware feature module (D25) may use this operation. |
| `too_many` | -7 | The plugin already holds the most scheduler entries it may: 8 for a firmware module, 4 for a community plugin. |
| `too_close` | -7 | Another entry of this plugin is due within 5 minutes of this one (community plugins, D26). |
| `not_allowed` | -5 | The owner hasn't turned on "Allow alarms" for this plugin on the Device Console (D26). |
| `not_due` | -2 | No entry of this plugin is due to ring now. |

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
| `audio_stream_frame_max` | 8192 |
| `audio_stream_queue_ms` | 200 |
| `audio_stream_idle_s` | 10 |
| `audio_stream_fd` | 4 |
| `schedule_entries_max` | 8 |
| `schedule_ahead_s_max` | 31622400 |
| `schedule_late_s_max` | 300 |
| `schedule_community_entries_max` | 4 |
| `schedule_community_spacing_s` | 300 |
| `schedule_community_ring_s` | 60 |
| `settings_keys_max` | 64 |
| `settings_key_max` | 32 |
| `settings_value_max` | 512 |
