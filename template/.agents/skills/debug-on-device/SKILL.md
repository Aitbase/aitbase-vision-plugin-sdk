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
| `AITBASE_EVENT_ERROR` | A request failed. `op` names it; `status` and `detail` say why; `request` is the id the call returned. | `not_open`: you updated before `OPENED` or after the screen closed. `module_only`: only firmware plugins may do this. `destination_blocked`: the URL is not a public http(s) host. `not_foreground`: play sounds only after `OPENED`. `rate_limited`: slow down. `no_storage`: no SD card. Every code: `sdk/docs/capabilities.md`. UI codes: the screen or update is invalid. |
| `AITBASE_EVENT_SOUND_DONE` with `detail` `failed` | The speaker was busy or unavailable. | Normal now and then; don't retry in a loop. |
| `AITBASE_EVENT_UPDATE_REJECTED` | An update to `node` was refused. | Check the node name (`<instance>.<label>`), the subject (`title`) and the text length (80 bytes or less). |
| `AITBASE_EVENT_PREEMPTED` | The user left with a LEFT long press, or the device needed the screen. | Stop. This is normal. |

`AITBASE_ERR_NOT_FOUND` from `aitbase_ui_open` means there is no compiled `<screen>.ui.json` next to the program. Check `SCREENS` in `CMakeLists.txt`.

## 3. Nothing runs on the device

With a paired device, `aitbase-plugin dev --json` reports what happened: `DEVICE_PACKAGE_SKIPPED` and `DEVICE_PLUGIN_ENDED` carry the device's reason in `message`, and `aitbase-plugin logs --json` returns the log. Every `DEVICE_*` code has a `fix`. For `DEVICE_NOT_PAIRED`, ask the user to run `aitbase-plugin pair <device address>` themselves.

Without one, ask the user to open the Device Console's **Advanced → Plugins** page:
- **The package is listed as skipped:** the reason says why:
  - "unzipped plugin folders don't run": the user unzipped it. Copy the `.avp` file itself into `aitbase/plugins/`.
  - "signature does not match": the file was damaged or only partly copied. Copy it again.
  - "signed by another author": the plugin was packed with a different signing key than before, for example on another computer. Use the original key (`aitbase-plugin key` shows where it is). If it is lost, the owner can forget the plugin on that page, which deletes its saved data.
  - Anything else: run `aitbase-plugin build --wasm` and `pack --wasm` again.
- **"Last run ... ended":** the reason is also at the end of the log, for example `exit 121` (the device refused the module) or `exit 122` (a trap, or a callback ran too long).

## 4. Asking the user

With a paired device, check the screen yourself: `aitbase-plugin screenshot --json -o screen.png`, look at the PNG, then `aitbase-plugin press right --json` and take another screenshot to see what changed. Without one, ask specific questions:
- What is on the screen? Does the text change when you press RIGHT?
- Does LEFT move the highlight between buttons?
- What does `aitbase/logs/<id>.log` on the SD card say? It has the plugin's own lines, starting with `[<plugin id>]`, and the device's `started`/`ended` lines with the reason, for example a refused module or a trap.

Never ask for the device password.
