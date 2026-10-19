# Changelog

## 0.11.1 — 2026-10-20

Documentation only; no API or behavior change.

- **Docs:** `net` is granted to every firmware feature module, on every target (device decision D34). There is no kernel gate precondition any more.

## 0.11.0 — 2026-10-20

An open plugin model (device decision D32): fewer rules for plugin authors. Needs device firmware with D32; older devices still need `requested_capabilities`.

- **No permission list.** Every plugin may use every capability: screens, storage, HTTP, sounds and audio streams, alarms, idle faces and assistant tools. `requested_capabilities` is no longer needed. `check` warns `CAPABILITIES_IGNORED` when it is there, and an old list still works. The template and `examples/weather` drop it, and the schema no longer requires it.
- **What the device still enforces** is in each call's rules, not in a list: sound only while the plugin's screen shows, HTTP to public hosts only, sizes and rate limits, and LEFT long press always leaving.
- **Removed: `aitbase_granted()`** and the runtime's copy of `hello.grants`. Code that called it no longer builds. A firmware feature module doesn't need it: the device only sends it work it may do.
- **Alarms:** any plugin may set them. There is no owner "Allow alarms" any more, and `not_allowed` is no longer returned. The device rings a due alarm at once, once, and never rings one whose time passed while it was off. `aitbase_schedule_ring()` does nothing and sends nothing; it is kept so 0.10 code builds.
- **`check` codes retired:** `CAPABILITY_UNAVAILABLE`, `CAPABILITY_FIRMWARE_ONLY`, `CAPABILITY_UNKNOWN`, `CAPABILITY_UI_MISSING`, `CAPABILITY_AUDIO_MISSING` and `TOOLS_GRANT_MISSING`.
- **Errors:** devices with D32 answer `module_only` for firmware-only operations and the new `no_screen` for a module's `ui`. They never answer `no_grant`.

## 0.10.0 — 2026-10-20

For plugins that ship in the device firmware (feature modules, D23/D25), plus screen templates (D27), idle faces (D22), alarms (D26), audio streams (D28) and assistant tools (D31) for every plugin.

- **`aitbase_call(p, cap, op, args_json, timeout_ms, &reply)`:** sends any operation and waits for its reply: the result object as JSON in your buffer, or the device's status and error code. Messages that arrive meanwhile are delivered after the current callback returns, in order. New statuses `AITBASE_ERR_TIMEOUT` and `AITBASE_ERR_UNSUPPORTED`. **Native runtime only:** in WASM it returns `AITBASE_ERR_UNSUPPORTED` and sends nothing.
- **`AITBASE_EVENT_DEVICE`:** events of capabilities other than `ui` and `audio`, with `op` = `"<cap>.<event>"` (for example `schedule.due`, `module.action`) and `text` = their data as JSON. Before 0.10, a `module.action` event looked like a UI `ACTION`. The event struct is unchanged.
- **`aitbase_ui_template(p, &tpl, wait_ms)`:** shows one of the device's own screen layouts with your text, no compiled screen needed: `status` (title, emoji, OK), `alert` (icon, title, OK) or `menu` (title and up to 4 items). RIGHT gives `template.ok` or `template.itemN` as an `ACTION`. Works in WASM and native plugins. Needs device firmware with D27; older devices answer `unknown_op`.
- **Idle faces (D22):** `"idle_face": "<screen>"` in `plugin.json` offers one of `ui.screens` as the device's home screen face. The owner chooses it on the Device Console, and the device shows it in place of its clock, display only and under its own "Plugin" badge, without running the plugin. Labels bind to the new platform subjects `face.1` to `face.16`. **`aitbase_face_set(p, key, value)`** sets them, at most 64 bytes, and needs the new capability `idle.face`. The device keeps the values, so they show after a reboot. `check` validates the face: new codes `IDLE_FACE_SCREEN` and `IDLE_FACE_BOUNDS` (no buttons or actions, only `face.N` bindings, at most 24 widgets and 8 KiB compiled). Needs device firmware with D22; older devices skip the key and answer `face.set` with `unknown_op`.
- **Alarms for every plugin (D26):** `aitbase_schedule_set(p, id, due_epoch, rings, label)`, `aitbase_schedule_cancel`, `aitbase_schedule_list` and `aitbase_schedule_ring`, tracked like storage (RESULT/ERROR events), in WASM and native plugins. `schedule.due` arrives as `AITBASE_EVENT_DEVICE`. The device rings entries itself, on its alarm screen titled "<plugin name>: <label>", for at most 60 s, even when the plugin isn't running. A community plugin needs the owner's "Allow alarms" on the Device Console (otherwise `not_allowed`) and holds at most 4 entries, 5 minutes apart (`too_many`, `too_close`). `schedule` is no longer firmware-only in `check`. Needs device firmware with D26.
- **Audio streams (D28):** `aitbase_audio_stream_open(p, rate)`, `aitbase_audio_stream_write(p, pcm, samples)` and `aitbase_audio_stream_close(p)` play continuous 16-bit mono PCM (8 to 48 kHz, up to 4096 samples a write) while the plugin's screen is visible, in WASM and native plugins. Writes never block: 0 means the device's buffer is full, so send the same samples again later. The new `AITBASE_EVENT_STREAM_STATE` reports `paused` (an alarm, a call or the voice assistant took the speaker), `resumed` and `stopped`, with the new `event->reason`. New capability `audio.stream`; new WASM import `audio_write`; the samples travel on a second socket, descriptor 4 ([protocol](docs/plugin-protocol.md#audio-channel-d28)). Errors `speaker_busy`, `streaming` (sounds while a stream is open), `bad_rate` and `no_channel`. Needs device firmware with D28. **`event->op` grew to 24 bytes** to hold `audio.stream_open`: rebuild plugins.
- **Background audio (D29), feature modules only:** `aitbase_audio_stream_open_background(p, rate, title)` keeps the stream playing after the screen closes; the device shows it on its own screen, titled "<plugin name>: <title>", below the assistant and any plugin screen (`audio.background`).
- **`aitbase_granted(p, capability)`:** whether the device granted a capability to this run, from the `hello` reply's `grants`. A feature module checks `net` (D30) before it opens its own connections: the device refuses `net` where the target's kernel gate isn't installed, and a native process can't otherwise tell.
- **Assistant tools (D31):** a plugin answers the voice assistant through tools it declares in `plugin.json`'s new `assistant_tools` (at most 4 tools, each with at most 4 string, int or enum arguments, all required) and the new capability `assistant.tools`. The device sends `AITBASE_EVENT_TOOL_CALL` (`action` the tool, `request` the call id, `text` the arguments as JSON); **`aitbase_tool_arg_string()`** and **`aitbase_tool_arg_int()`** read the arguments, and **`aitbase_tool_reply(p, call, text)`** answers once within 5 s, at most 2048 bytes. A plugin that isn't running is started for the call without a screen, with `aitbase_trigger()` returning `"assistant"`. `check` validates the block: new codes `TOOLS_FORMAT`, `TOOLS_GRANT_MISSING`, `TOOL_NAME`, `TOOL_DESCRIPTION` and `TOOL_ARG`. The sample is `examples/weather`. Needs device firmware with D31; older devices ignore the block.
- **Capabilities:** the registry adds `schedule`, `settings` and `status` (D23, D25), with `schedule.*`, `settings.*` and `status.publish`. They are for firmware plugins only: `check` reports the new `CAPABILITY_FIRMWARE_ONLY` when a plugin requests them.

## 0.9.0 — 2026-10-18

Plugins that start by themselves, and know why they started. Needs device firmware with triggers (D21); older devices ignore `triggers` and don't say why.

- **Schedules:** `"triggers": [{"event": "schedule", "every_min": N}]` in `plugin.json` (15 to 1440 minutes, one schedule). The device starts the plugin that often while it is idle and no other plugin runs; a scheduled run without a screen is ended after 30 s. `check` validates it: new codes `TRIGGER_FORMAT`, `TRIGGER_UNKNOWN`, `TRIGGER_SCHEDULE`.
- **`aitbase_trigger(p)`:** why the device started the plugin: `"autorun"`, `"voice"`, `"shortcut"` (the owner's RIGHT long press on the home screen, set on the Plugins page) or `"schedule"`; `""` on older devices. The protocol's `hello` reply carries `trigger`.

## 0.8.0 — 2026-10-18

See and press the device's plugin screen from the computer. Needs device firmware with the developer routes' screenshot and press (D19 step 2).

- **`screenshot [-o file.png]`** saves the plugin's screen on the device as a PNG; **`press left|right [--long]`** presses its buttons. Both work only while a plugin's own screen shows, with nothing over it; otherwise `DEVICE_NOT_FOREGROUND`.
- New codes `DEVICE_NOT_FOREGROUND`, `DEVICE_BUSY` and `DEVICE_NO_DISPLAY`. `devices` and `pack-sd` still return `NOT_AVAILABLE`.
- **Button events (D20):** while its screen shows, a plugin receives `AITBASE_EVENT_KEY` for LEFT press, RIGHT press and RIGHT long press, with the new `event->key` (`AITBASE_KEY_LEFT`, `AITBASE_KEY_RIGHT`) and `event->gesture` (`AITBASE_GESTURE_PRESS`, `AITBASE_GESTURE_LONG_PRESS`). Each comes before the `ACTION` of the same press; a long press comes after a press of the same button; LEFT long press still always leaves and is never delivered. A screen without buttons is driven by these alone. Protocol event `key`. Needs device firmware with D20; older devices just don't send it, and older SDKs ignore it. Rebuild plugins: the event struct grew.

## 0.7.0 — 2026-10-18

Develop on a device over the network. Needs device firmware with the plugin developer routes (D19).

- **`pair <address>`:** pairs the computer with a device once. It asks for the Device Console password at a hidden prompt, never anywhere else, and keeps only the developer token the device returns, in `aitbase-plugin/devices.json` in the user configuration folder (or `$AITBASE_DEVICES`), owner-only. Devices refuse pairing while they use their default password.
- **`dev [dir]`:** builds WebAssembly, packs the signed `.avp`, uploads it to the device's SD card and waits until the device runs it.
- **`logs [dir] [--follow]`** prints the plugin's log from the device; **`unpair`** revokes the token.
- New codes `PASSWORD_NEEDS_TERMINAL` and `DEVICE_*` ([docs/cli.md](docs/cli.md#diagnostic-codes)). `devices`, `screenshot`, `press` and `pack-sd` still return `NOT_AVAILABLE`.
- New dependency: `golang.org/x/term`, for the hidden password prompt.

## 0.6.0 — 2026-10-18

- **The SDK comes with the CLI:** a project without an `sdk/` of its own builds with the SDK built into the CLI, unpacked once into the user cache folder. Repositories of plugins no longer need the SDK as a git submodule. `build` passes the SDK directory to CMake as `AITBASE_SDK_DIR` and accepts `--sdk`; new command `sdk` prints the directory (JSON `sdk_dir`, `bundled`); new code `SDK_UNAVAILABLE`. `pack` no longer reads an SDK. See [docs/cli.md](docs/cli.md#which-sdk).

Signed packages. Needs device firmware with the package loader (D12); devices with it no longer run unzipped plugin folders.

- **`pack --wasm` writes a signed `<id>-<version>.avp`**, to copy into `aitbase/plugins/` on the SD card instead of unzipping. It holds `plugin.json`, `<entry>.wasm`, the compiled screens and the sounds at its top level, all stored uncompressed so the device reads them in place, signed with ECDSA P-256 ([docs/package-format.md](docs/package-format.md)). The JSON `sd_dir` is now `aitbase/plugins`; new fields `author`, `signing_key`, `key_created`.
- **Signing key:** created on the first `pack --wasm` in the user configuration folder (or `$AITBASE_SIGNING_KEY`), owner-only. New command `key` shows its path and fingerprint; new code `SIGNING_KEY`. Devices pin the first key per plugin id: back the key up.
- **No Developer mode:** devices with this firmware run signed packages from the SD card without a switch, and show no DEV badge; docs and template updated.
- **Native `pack`** (without `--wasm`) now writes `<id>-<version>-native.zip`; devices never run native code from the SD card.

## 0.5.0 — not released separately; included in 0.6.0

Storage, HTTP and sound through the device's capability broker. Needs device firmware with the broker (D4); older firmware answers these requests with `unknown_op`.

- **Capabilities:** `storage`, `http` and `audio.output` join `ui`. Request them in `plugin.json`. While Developer mode is on, the device grants what a plugin requests. [docs/capabilities.md](docs/capabilities.md) lists every operation, limit and error code; it, `include/aitbase_capabilities.h` and `schema/plugin_capability_registry.json` are generated from the device's capability registry.
- **API:** `aitbase_storage_get`, `_set`, `_delete`, `_keys`; `aitbase_http_get`, `aitbase_http_request`; `aitbase_audio_tone`, `_play`, `_stop`. They return a request id. Results arrive as the new `AITBASE_EVENT_RESULT` and `AITBASE_EVENT_SOUND_DONE`, and errors as `AITBASE_EVENT_ERROR`, all with the id in the new `event->request`. New event fields: `request`, `text`, `text_len`, `code`, `truncated`, `location`. Rebuild plugins: the event struct grew.
- **Sounds:** `sounds/<name>.wav` (16 kHz, mono, 16-bit PCM, at most 10 s), listed in the manifest's new `sounds` field. `check` validates them (`SOUND_NAME`, `SOUND_MISSING`, `SOUND_FORMAT`, `CAPABILITY_AUDIO_MISSING`) and `pack` includes them.
- **Larger messages from the device:** the receive buffer is 16 KiB. `hello` sends `rx_max`, and WASM modules export `aitbase_rx_capacity`; devices without them keep sending at most 4 KiB.

## 0.4.0 — 2026-10-16

WebAssembly plugins that run on current device firmware, and a one-line install.

- **One-line install:** `install.sh` (Linux, macOS) and `install.ps1` (Windows) download the newest CLI for the system, check it against `SHA256SUMS`, put it on the PATH and download the WebAssembly toolchain ([docs/cli.md](docs/cli.md#install)).
- **Repositories of plugins:** without `--sdk`, `check` and `pack` use the nearest `sdk/` in the project directory or a parent.
- **Supervised plugins:** when the device's plugin supervisor sets `AITBASE_PLUGIN_FD`, the native runtime uses that inherited socket instead of connecting to the development socket, and makes the process non-dumpable (`PR_SET_DUMPABLE`). A value that isn't a connected socket is an error; the runtime never falls back to the path. See [docs/plugin-protocol.md](docs/plugin-protocol.md).
- **Protocol docs:** the supervised transport, and the `identity_mismatch` error for a `hello` that claims another plugin's identity.
- **WASM runtime (preview):** with wasi-sdk 34 or later (`CMAKE_SYSTEM_NAME` `WASI`), `aitbase::plugin` builds the plugin as a WebAssembly reactor that speaks the device's WASM host ABI ([docs/wasm-abi.md](docs/wasm-abi.md)). Plugin code is unchanged. The native and WASM runtimes now share one protocol core (`src/aitbase_plugin_core.c`). `aitbase-plugin build --wasm` and `pack --wasm` produce them (below).

- **`aitbase-plugin build --wasm` and `pack --wasm`:** build a plugin as WebAssembly and write `dist/<id>-<version>-wasm.zip`, which unzips at the root of a device's SD card. The first `build --wasm` downloads wasi-sdk 34.0 into the user cache and checks it against a pinned SHA-256; set `AITBASE_WASI_SDK` to use your own. `pack --wasm` strips debug sections and refuses modules the device would refuse. New command `toolchain` prints the wasi-sdk path. The project template and skills now use the WASM loop. See [docs/cli.md](docs/cli.md#webassembly).

## 0.3.0 — 2026-10-16

The first public release.

Tooling for customers who build plugins with AI coding agents. The plugin API, protocol and UI schema are unchanged from 0.2.0.

- **`aitbase-plugin` CLI** (Go, one binary for Windows, macOS and Linux):
  - `init` creates a plugin project with a copy of the SDK;
  - `check` validates the manifest, ids, file names and screens;
  - `build` runs CMake;
  - `pack` writes a deterministic `.avp`.

  Every command has `--json` output with stable diagnostic codes ([docs/cli.md](docs/cli.md)). The device commands (`devices`, `pair`, `dev`, `logs`, `screenshot`, `press`, `pack-sd`) are reserved and return `NOT_AVAILABLE`.
- **Agent-ready project template:** `AGENTS.md`, `CLAUDE.md`, and the `create-plugin`, `plugin-ui-xml` and `debug-on-device` skills.
- **`llms.txt`** documentation index.
- **`schema/plugin.schema.json`:** the draft manifest schema.
- **`docs/package-format.md`:** draft `.avp` package format 0 and the `aitbase/plugins/` SD layout.
- **Path options:** `--sdk`, `--build-dir` and `--out` take paths relative to the current directory, or absolute, so a build tree outside the project (for example a monorepo) can be packed.
- **Releases:** pushing a `v*` tag publishes the CLI binaries for six platforms with `SHA256SUMS` ([install](docs/cli.md#install)).
- **Fix:** plugins that don't define the optional `aitbase_on_stop` now link on macOS. The runtime used a weak declaration, which Apple's linker rejects when unresolved; a no-op default now lives in its own library object (`src/aitbase_plugin_defaults.c`).

## 0.2.0 — not released separately; included in 0.3.0

This release breaks compatibility with 0.1.0.

- **Plugin API version 1.** Plugins now implement callbacks (`aitbase_on_start`, `aitbase_on_event`, optional `aitbase_on_stop`) and declare `AITBASE_PLUGIN_ID`. The SDK owns the event loop and `main()`. This is the run-to-completion model the WASM runtime will use.
- **Screens open by name:** `aitbase_ui_open(p, "<screen>", wait_ms)`. `aitbase_ui_open_file()`, `aitbase_plugin_connect()` and `aitbase_plugin_next_event()` were removed.
- **New calls:** `aitbase_timer_set()`, `aitbase_plugin_stop()` and `aitbase_log()`.
- **Protocol version 1:**
  - a `system.hello` handshake with protocol and API versions;
  - request `id`s with matching replies;
  - a `{cap, op, args}` envelope;
  - events as `{cap, event, data}`.

  See `docs/plugin-protocol.md`.
- **UI XML now follows LVGL 9.4 conventions:**
  - `ui/screens/`, `ui/components/` and `globals.xml`;
  - the widgets `lv_obj`, `lv_label`, `lv_button` and `lv_image`;
  - `name=`, `style_bg_color`/`style_text_color`/`style_pad_all`, and `#token` colors;
  - `bind_text`, `<event_cb … callback="aitbase_action" user_data="<action>">`, and component `<api><prop>` with `$prop`.

  The compiled device document is unchanged (`aitbase.plugin-ui` v1).
- **Registry:**
  - named color tokens (`color_token_names`);
  - `tools/plugin_ui_registry.py --globals` generates `schema/aitbase_globals.xml`.
- **CMake:** `aitbase_plugin_add_ui(<target> SCREENS ...)` produces `<screen>.ui.json` next to the binary.


## 0.1.0 — not released separately; included in 0.3.0

The first public preview. It is experimental, and the API and UI schema will change before 1.0.

- Added `aitbase_plugin.h`, a C plugin API: connect, open, update and close a screen, and receive events (`AITBASE_PLUGIN_API_VERSION 0`).
- Added the v0 transport over a device's development bridge socket.
- Added `schema/plugin_ui_registry.json`, UI schema v1: widgets, properties, color tokens, actions, bindings and limits.
- Added `tools/plugin_ui_compile.py`, the XML → `aitbase.plugin-ui` JSON compiler, and `tools/plugin_ui_registry.py`, which generates the device validator header.
- Added the CMake helper `aitbase_plugin_add_ui()`.
