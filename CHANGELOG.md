# Changelog

## 0.1.0 — unreleased

The first public preview. It is experimental, and the API and UI schema will change before 1.0.

- Added `aitbase_plugin.h`, a C plugin API: connect, open, update and close a screen, and receive events (`AITBASE_PLUGIN_API_VERSION 0`).
- Added the v0 transport over a device's development bridge socket.
- Added `schema/plugin_ui_registry.json`, UI schema v1: widgets, properties, color tokens, actions, bindings and limits.
- Added `tools/plugin_ui_compile.py`, the XML → `aitbase.plugin-ui` JSON compiler, and `tools/plugin_ui_registry.py`, which generates the device validator header.
- Added the CMake helper `aitbase_plugin_add_ui()`.
