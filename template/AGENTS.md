# AITBASE VISION plugin: instructions for coding agents

This project is a plugin for an AITBASE VISION camera robot. The person you are helping owns the device and may not be a programmer. Keep changes small, run the checks below after every change, and explain results in plain language.

You do not have, and do not need, the device firmware source. Everything you need is in this project and in `sdk/` (SDK __SDK_VERSION__).

## What a plugin is

- A plugin is C code that implements callbacks, plus screens written in a restricted XML.
- The device draws the screens itself. The plugin only opens a screen, sets bound values and reacts to events.
- A plugin **cannot** use files, sockets, threads, `sleep`, `system()`, `/dev`, LVGL or any OS API. There is no workaround. If the user asks for something that needs one of these, say it is not possible with this SDK version.
- Callbacks must return quickly. Never block or loop waiting. For periodic work, use `aitbase_timer_set()` and handle `AITBASE_EVENT_TIMER`.
- Do not write `main()`. The SDK provides it.

## What this SDK version supports

Only what is listed here exists. Do not invent functions, widgets, colors, actions or capabilities.

- **Capability:** only `"ui"`. Camera, microphone, audio, HTTP, storage and MCP tools are planned but not available. `aitbase-plugin check` rejects them.
- **API:** `sdk/include/aitbase_plugin.h`. Read it before writing code. Functions: `aitbase_ui_open`, `aitbase_ui_update`, `aitbase_ui_set_text`, `aitbase_ui_close`, `aitbase_timer_set`, `aitbase_plugin_stop`, `aitbase_log`, `aitbase_event_kind_name`.
- **UI:** widgets `lv_obj`, `lv_label`, `lv_button`, `lv_image`. Four colors: `#surface`, `#on_surface`, `#accent`, `#control`. One bindable subject: `title`. One action: `plugin.increment`. Full rules: the `plugin-ui-xml` skill and `sdk/schema/plugin_ui_registry.json`.
- **Screen:** 240×320 pixels.

## Input: two buttons, no touch

The device has two physical buttons and no touchscreen.
- LEFT moves focus to the next button on screen.
- RIGHT activates the focused button. The plugin gets `AITBASE_EVENT_ACTION`.
- A LEFT long press always leaves the plugin. The plugin gets `AITBASE_EVENT_PREEMPTED` and should stop.

Design every screen for this: a few large buttons in a sensible order, no text input, no scrolling lists.

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
aitbase-plugin pack --wasm --json    # writes dist/<id>-<version>-wasm.zip for the device
```

The first `build --wasm` downloads the WebAssembly toolchain (about 200 MB) and can take a few minutes; later builds reuse it. If the download fails, tell the user, and don't install another compiler instead. `pack --wasm` also refuses code the device can't run, for example files or sockets: use only the calls in `sdk/include/aitbase_plugin.h`.

Fix every `error` before continuing. Do not silence a check by removing the feature the user asked for without telling them.

## Testing on a device

There is no simulator, so a successful build does not prove the plugin works. Only the device shows that.

To try it on the user's device:
1. **Once:** the user turns on **Developer mode** in the device's Device Console (**Advanced → Plugins**), with their Device Console password. It is off by default. Only the user can do this: never ask for the password.
2. The user unzips `dist/<id>-<version>-wasm.zip` at the root of the device's SD card.
3. Within a few seconds the device runs the newest plugin on the card, with a **DEV** badge on its screen. A newer copy replaces the running one.
4. The plugin's log is on the card, in `aitbase/logs/<id>.log`, and on the Device Console's Plugins page.

If nothing runs, ask the user whether Developer mode is on and what the Plugins page shows: it lists packages it skipped, with the reason.

The commands `dev`, `logs`, `screenshot` and `press` are planned and currently return `NOT_AVAILABLE`. Do not try to work around this. Tell the user what to check on the device, and ask them what they see and what the log says.

## Security rules

- Never ask the user for the device password, and never put it in a file, command or environment variable. When device pairing exists, the user runs `aitbase-plugin pair` themselves.
- Do not use an id starting with `aitbase.`. It is reserved for official plugins.

## Skills

Task guides are in `.agents/skills/` (also in `.claude/skills/`):
- `create-plugin`: turn a request into a working plugin.
- `plugin-ui-xml`: write screens that pass validation.
- `debug-on-device`: read errors and diagnose problems.
