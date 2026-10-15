---
name: create-plugin
description: Turn a user's idea into a working AITBASE VISION plugin in this project. Use when asked to build, change or extend the plugin's behavior or screens.
---

# Create or change a plugin

1. **Check that the idea fits this SDK.** Read the "What this SDK version supports" section of `AGENTS.md`. If the idea needs the camera, microphone, sound, internet, saved data or voice commands, tell the user that those are not available yet and offer the closest version that works with screens, buttons and timers.
2. **Design for two buttons.** Sketch the screen as a short list: what text is shown, which buttons exist and in what order (LEFT moves focus through them in document order), and what RIGHT does on each. Keep it to a few large elements on a 240×320 screen. Confirm the sketch with the user if the idea is vague.
3. **Write the screen** in `ui/screens/<name>.xml`. Follow the `plugin-ui-xml` skill. If you add a screen, add it to `ui.screens` in `plugin.json` and to `SCREENS` in `CMakeLists.txt`.
4. **Write the callbacks** in `src/main.c`:
   - `aitbase_on_start`: open the first screen with `AITBASE_UI_MAX_WAIT_MS`.
   - `aitbase_on_event`: on `AITBASE_EVENT_OPENED` set the initial values; on `AITBASE_EVENT_ACTION` compare `event->action` and update state; on `PREEMPTED`, `REJECTED` and `CLOSED` call `aitbase_plugin_stop`.
   - Keep state in `static` variables. Format text with `snprintf` into a fixed buffer; labels hold at most 80 bytes.
   - Update a label with `aitbase_ui_set_text(p, "<instance>.<label>", "title", text)`. The node name is the component instance name, a dot, then the label name.
5. **Check and build** with `aitbase-plugin check --json` and `aitbase-plugin build --json`. Fix each diagnostic using its `fix` field, and repeat until both pass.
6. **Package** with `aitbase-plugin pack --json` when the user wants a file to share.
7. **Tell the user** what you changed, what the screen should look like, which buttons do what, and what they need to check on the device. Be clear that a build passing is not the same as working on the device.

## Common mistakes

- `AITBASE_PLUGIN_ID` in `src/` not matching `id` in `plugin.json` (`SOURCE_ID_MISMATCH`).
- A screen listed in `plugin.json` but missing from `CMakeLists.txt`, or the reverse. The build then lacks `<screen>.ui.json`.
- Using an action other than `plugin.increment`. It is the only registered action in this version. Tell buttons apart with `event->node`, the button's `name`.
- Blocking in a callback (`sleep`, loops waiting for something). Use `aitbase_timer_set` instead.
