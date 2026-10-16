---
name: debug-on-device
description: Diagnose AITBASE VISION plugin problems from check/build diagnostics, device events and logs. Use when a check fails, a build fails, or the user reports the plugin misbehaving on the device.
---

# Debug a plugin

## 1. Static problems: check and build

Run `aitbase-plugin check --json`, then `aitbase-plugin build --wasm --json`, then `aitbase-plugin pack --wasm --json`. Each diagnostic has `severity`, `code`, `path`, `message` and `fix`. Apply the fix, then run the command again. The full code list is in `sdk/docs/cli.md`.

A command that returns `NOT_AVAILABLE` (exit code 3) needs device support that doesn't exist yet. Don't retry it or look for another way. Tell the user.

## 2. Runtime problems: events

The device reports problems as events. Make sure `aitbase_on_event` logs these, so the user can tell you what happened:

| Event | Meaning | What to do |
|---|---|---|
| `AITBASE_EVENT_REJECTED` | The screen could not open: busy, cancelled or timed out. `detail` says why. | Open with `AITBASE_UI_MAX_WAIT_MS`; stop if it still fails. |
| `AITBASE_EVENT_ERROR` | A request failed. `op` names it; `status` and `detail` say why. | `not_open`: you updated before `OPENED` or after the screen closed. UI codes: the screen or update is invalid. |
| `AITBASE_EVENT_UPDATE_REJECTED` | An update to `node` was refused. | Check the node name (`<instance>.<label>`), the subject (`title`) and the text length (80 bytes or less). |
| `AITBASE_EVENT_PREEMPTED` | The user left with a LEFT long press, or the device needed the screen. | Stop. This is normal. |

`AITBASE_ERR_NOT_FOUND` from `aitbase_ui_open` means there is no compiled `<screen>.ui.json` next to the program. Check `SCREENS` in `CMakeLists.txt`.

## 3. Nothing runs on the device

Ask the user to open the Device Console's **Advanced → Plugins** page:
- **Developer mode off:** they turn it on there; nothing on the SD card runs until then.
- **The package is listed as skipped:** the reason says why, for example no `<entry>.wasm` because the build was native. Run `aitbase-plugin build --wasm` and `pack --wasm` again.
- **"Last run ... ended":** the reason is also at the end of the log, for example `exit 121` (the device refused the module) or `exit 122` (a trap, or a callback ran too long).

## 4. Asking the user

There is no simulator, and this SDK version cannot read logs or take screenshots remotely. Ask specific questions:
- What is on the screen? Does the text change when you press RIGHT?
- Does LEFT move the highlight between buttons?
- What does `aitbase/logs/<id>.log` on the SD card say? It has the plugin's own lines, starting with `[<plugin id>]`, and the device's `started`/`ended` lines with the reason, for example a refused module or a trap.

Never ask for the device password.
