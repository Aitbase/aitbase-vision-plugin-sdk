# AITBASE VISION plugin: instructions for coding agents

This project is a plugin for an AITBASE VISION camera robot. The person you are helping owns the device and may not be a programmer. Keep changes small, run the checks below after every change, and explain results in plain language.

You do not have, and do not need, the device firmware source. Everything you need is in this project and in `sdk/` (SDK __SDK_VERSION__).

## What a plugin is

- A plugin is C code that implements callbacks, plus screens written in a restricted XML.
- The device draws the screens itself. The plugin only opens a screen, sets bound values and reacts to events.
- A plugin **cannot** use files, sockets, threads, `sleep`, `system()`, `/dev`, LVGL or any OS API. There is no workaround. The device does a few things for the plugin instead: keep small values (storage), make HTTP requests, play sounds. If the user asks for something else that needs the OS, say it is not possible with this SDK version.
- Callbacks must return quickly. Never block or loop waiting. For periodic work, use `aitbase_timer_set()` and handle `AITBASE_EVENT_TIMER`.
- Do not write `main()`. The SDK provides it.

## What this SDK version supports

Only what is listed here exists. Do not invent functions, widgets, colors, actions or capabilities.

- **Capabilities:** list what the plugin uses in `requested_capabilities` in `plugin.json`:
  - `"ui"`: its screens;
  - `"storage"`: small text values kept across runs (needs an SD card in the device);
  - `"http"`: HTTP(S) requests to public internet hosts, never local network addresses;
  - `"audio.output"`: tones and the package's own sounds (`sounds/<name>.wav`, listed under `"sounds"`), only while its screen is visible.

  Camera, microphone and MCP tools are planned but not available; `aitbase-plugin check` rejects them. Limits and every error code are in `sdk/docs/capabilities.md`.
- **API:** `sdk/include/aitbase_plugin.h`. Read it before writing code. Functions: `aitbase_ui_open`, `aitbase_ui_update`, `aitbase_ui_set_text`, `aitbase_ui_close`, `aitbase_storage_get`, `aitbase_storage_set`, `aitbase_storage_delete`, `aitbase_storage_keys`, `aitbase_http_get`, `aitbase_http_request`, `aitbase_audio_tone`, `aitbase_audio_play`, `aitbase_audio_stop`, `aitbase_timer_set`, `aitbase_plugin_stop`, `aitbase_log`, `aitbase_event_kind_name`. Don't use `aitbase_call`: it is for plugins that ship in the device firmware and returns `AITBASE_ERR_UNSUPPORTED` in a community (WASM) plugin.
- **Asynchronous results:** storage, HTTP and sound calls return a request id at once. The answer arrives later as `AITBASE_EVENT_RESULT` (or `AITBASE_EVENT_SOUND_DONE`, or `AITBASE_EVENT_ERROR`) with that id in `event->request`. Keep the id, and copy `event->text` if you need it after the callback. Never wait for a result in a loop.
- **UI:** widgets `lv_obj`, `lv_label`, `lv_button`, `lv_image`. Four colors: `#surface`, `#on_surface`, `#accent`, `#control`. One bindable subject: `title`. One action: `plugin.increment`. Full rules: the `plugin-ui-xml` skill and `sdk/schema/plugin_ui_registry.json`.
- **Screen:** 240×320 pixels.
- **Signing:** `pack --wasm` signs the package with the user's key, created on the first pack (`aitbase-plugin key` shows it). The device remembers the first key per plugin id and refuses updates signed with another, so tell the user to back the key file up. Never print, copy or commit the key file.
- **How it starts:** `aitbase_trigger(p)` in `aitbase_on_start` says why: `"autorun"` (copied to the card or `dev`), `"voice"`, `"shortcut"` (the owner's RIGHT long press on the home screen) or `"schedule"`. A plugin can run by itself on a schedule with `"triggers": [{"event": "schedule", "every_min": 60}]` in `plugin.json` (15 to 1440 minutes). A scheduled run happens only while the device is idle, nobody asked for it, and it is ended after 30 s unless it shows its screen: do the work (for example an HTTP request and `storage`), then stop, or open the screen only when there is something to show.
- **Voice start:** the user can open the plugin by asking the device's assistant ("open <name>"). The assistant matches the manifest's `name`, so keep it short and easy to say, and unlike other plugins' names. The screen appears when the assistant finishes speaking, which is why `aitbase_ui_open` should wait (`AITBASE_UI_MAX_WAIT_MS`).

## Input: two buttons, no touch

The device has two physical buttons and no touchscreen.
- LEFT moves focus to the next button on screen.
- RIGHT activates the focused button. The plugin gets `AITBASE_EVENT_ACTION`.
- A LEFT long press always leaves the plugin. The plugin gets `AITBASE_EVENT_PREEMPTED` and should stop.
- The plugin also hears the buttons themselves while its screen shows: `AITBASE_EVENT_KEY` with `ev->key` (`AITBASE_KEY_LEFT` or `AITBASE_KEY_RIGHT`) and `ev->gesture` (`AITBASE_GESTURE_PRESS` or `AITBASE_GESTURE_LONG_PRESS`). It comes before the `ACTION` of the same press. A long press always comes after a press of the same button. LEFT long press is never delivered.

Design every screen for this. With buttons on the screen: a few large ones in a sensible order. Without any: the screen is driven by `AITBASE_EVENT_KEY` alone, for a game or a stopwatch (RIGHT press, RIGHT long press and LEFT press). No text input, no scrolling lists.

## Project layout

| Path | What it is |
|---|---|
| `plugin.json` | Manifest. `id` must match `AITBASE_PLUGIN_ID(...)` in `src/`. `ui.screens` must match `SCREENS` in `CMakeLists.txt`. |
| `src/main.c` | Callbacks. |
| `ui/screens/<name>.xml` | One file per screen, opened with `aitbase_ui_open(p, "<name>", ...)`. |
| `ui/components/<name>.xml` | Reusable components. |
| `ui/globals.xml`, `ui/assets/` | Optional images (RGB565, at most 32×32). |
| `sdk/` | The SDK. Do not edit it. |

## The loop: change, check, build

Run these after every change, from the project directory. Use `--json` and read the `diagnostics`: each has a stable `code`, a `path` and a `fix`.

```sh
aitbase-plugin check --json          # manifest, ids, file names, screens
aitbase-plugin build --wasm --json   # compiles C to WebAssembly, and the screens
aitbase-plugin pack --wasm --json    # writes the signed dist/<id>-<version>.avp for the device
```

The first `build --wasm` downloads the WebAssembly toolchain (about 200 MB) and can take a few minutes; later builds reuse it. If the download fails, tell the user, and don't install another compiler instead. `pack --wasm` also refuses code the device can't run, for example files or sockets: use only the calls in `sdk/include/aitbase_plugin.h`.

Fix every `error` before continuing. Do not silence a check by removing the feature the user asked for without telling them.

## Testing on a device

There is no simulator, so a successful build does not prove the plugin works. Only the device shows that.

**When the user has paired a device** (`aitbase-plugin pair`, which they run themselves):
```sh
aitbase-plugin dev --json     # build --wasm, pack --wasm, upload, wait until the device runs it
aitbase-plugin logs --json    # the plugin's log from the device
aitbase-plugin screenshot --json -o screen.png   # what the plugin's screen shows; look at the PNG
aitbase-plugin press right --json                # RIGHT activates the focused widget; left moves focus
```
`screenshot` and `press` work only while the plugin's screen shows (`DEVICE_NOT_FOREGROUND` otherwise). After `dev`, wait for `AITBASE_EVENT_OPENED` in the log before taking a screenshot. A LEFT long press (`press left --long`) leaves the plugin.
`dev` failing with `DEVICE_NOT_PAIRED` means no device is paired, or its token expired: ask the user to run `aitbase-plugin pair <device address>` in their own terminal. Never run `pair` yourself, and never ask for the password.

**Otherwise:**
1. The user copies `dist/<id>-<version>.avp` into `aitbase/plugins/` on the device's SD card (creating the folders if needed). Don't unzip it.
2. Within a few seconds the device runs the newest plugin on the card. A newer copy replaces the running one.
3. The plugin's log is on the card, in `aitbase/logs/<id>.log`, and on the Device Console's Plugins page.

If nothing runs, ask the user what the Device Console's Plugins page shows: it lists packages it skipped, with the reason.

Without a paired device, ask the user what they see on the screen.

## Security rules

- Never ask the user for the device password, and never put it in a file, command or environment variable. The user runs `aitbase-plugin pair` themselves, in their own terminal; it asks for the password at a hidden prompt.
- Do not use an id starting with `aitbase.`. It is reserved for official plugins.

## Skills

Task guides are in `.agents/skills/` (also in `.claude/skills/`):
- `create-plugin`: turn a request into a working plugin.
- `plugin-ui-xml`: write screens that pass validation.
- `debug-on-device`: read errors and diagnose problems.
