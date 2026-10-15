# Changelog

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
