---
name: create-plugin
description: Turn a user's idea into a working AITBASE VISION plugin in this project. Use when asked to build, change or extend the plugin's behavior or screens.
---

# Create or change a plugin

1. **Check that the idea fits this SDK.** Read the "What this SDK version supports" section of `AGENTS.md`. Screens, buttons, timers, saved values (`storage`), internet requests to public servers (`http`) and short sounds (`audio.output`) are available. If the idea needs the camera, microphone, devices on the user's home network or voice commands, tell the user that those are not available yet and offer the closest version that works.
2. **Design for two buttons.** Sketch the screen as a short list: what text is shown, which buttons exist and in what order (LEFT moves focus through them in document order), and what RIGHT does on each. A screen with no buttons can react to the buttons directly with `AITBASE_EVENT_KEY` (LEFT press, RIGHT press, RIGHT long press). Keep it to a few large elements on a 240×320 screen. Confirm the sketch with the user if the idea is vague.
3. **Write the screen** in `ui/screens/<name>.xml`. Follow the `plugin-ui-xml` skill. If you add a screen, add it to `ui.screens` in `plugin.json` and to `SCREENS` in `CMakeLists.txt`.
4. **Write the callbacks** in `src/main.c`:
   - `aitbase_on_start`: open the first screen with `AITBASE_UI_MAX_WAIT_MS`.
   - `aitbase_on_event`: on `AITBASE_EVENT_OPENED` set the initial values; on `AITBASE_EVENT_ACTION` compare `event->action` and update state; on `PREEMPTED`, `REJECTED` and `CLOSED` call `aitbase_plugin_stop`.
   - Keep state in `static` variables. Format text with `snprintf` into a fixed buffer; labels hold at most 80 bytes.
   - For storage, HTTP or sound: add the capability to `requested_capabilities` in `plugin.json`. The call returns a request id at once; handle the answer in `aitbase_on_event` as `AITBASE_EVENT_RESULT`, `AITBASE_EVENT_SOUND_DONE` or `AITBASE_EVENT_ERROR`, matching `event->request`. Show a "Loading…" text meanwhile. Sounds go in `sounds/<name>.wav`, listed under `"sounds"` in `plugin.json`, and play only while the screen is visible.
   - Update a label with `aitbase_ui_set_text(p, "<instance>.<label>", "title", text)`. The node name is the component instance name, a dot, then the label name.
5. **Check and build** with `aitbase-plugin check --json` and `aitbase-plugin build --wasm --json`. Fix each diagnostic using its `fix` field, and repeat until both pass.
6. **Package** with `aitbase-plugin pack --wasm --json`. The user copies `dist/<id>-<version>.avp` into `aitbase/plugins/` on the device's SD card to try it. If `key_created` is true, tell the user where their signing key is and to back it up.
7. **Tell the user** what you changed, what the screen should look like, which buttons do what, and what they need to check on the device. Be clear that a build passing is not the same as working on the device.

## Common mistakes

- `AITBASE_PLUGIN_ID` in `src/` not matching `id` in `plugin.json` (`SOURCE_ID_MISMATCH`).
- A screen listed in `plugin.json` but missing from `CMakeLists.txt`, or the reverse. The build then lacks `<screen>.ui.json`.
- Using an action other than `plugin.increment`. It is the only registered action in this version. Tell buttons apart with `event->node`, the button's `name`.
- Blocking in a callback (`sleep`, loops waiting for something). Use `aitbase_timer_set` instead, and wait for results as events.
- Using `event->text` after the callback returns. Copy it into your own buffer first.
- Requesting a local address such as `http://192.168.1.10/`. The device refuses it (`destination_blocked`); only public internet hosts work.
