# AITBASE VISION Plugin SDK

Build plugins that put their own screen on an AITBASE VISION device.

> **Status: 0.1.0 preview, experimental.** The API, the UI schema and the transport will change before 1.0. Running plugins currently needs a device on development firmware, and release firmware does not load plugins yet.

## What is in the SDK

| Path | Contents |
|---|---|
| `include/aitbase_plugin.h` | The C plugin API: open a screen, update bound values, receive user actions and lifecycle events. |
| `src/` | The v0 transport: the device's development bridge ([protocol](docs/dev-bridge-protocol.md)). |
| `schema/plugin_ui_registry.json` | UI schema v1: allowed widgets, properties, colors, actions, bindings and limits. This file is the source of truth for both the compiler and the device validator. |
| `tools/plugin_ui_compile.py` | The plugin UI XML → `aitbase.plugin-ui` JSON compiler ([UI XML reference](docs/ui-xml.md)). |
| `tools/plugin_ui_registry.py` | Generates the device's C++ validator tables from the schema (used by firmware). |
| `cmake/AitbasePluginUi.cmake` | `aitbase_plugin_add_ui()`, which compiles UI XML as part of a CMake build. |

## How a plugin works

```
plugin process ── aitbase_plugin.h ──► device ── validates UI ──► LVGL screen
       ▲                                  │
       └──────── events (opened, action, preempted…) ◄──┘
```

The device owns the screen:
- **Rendering:** it validates and draws the plugin's UI itself.
- **Buttons:** LEFT moves focus, RIGHT activates the focused widget, and a LEFT long press always exits.
- **Taking the screen back:** this happens at any time, for example for the voice assistant.

A plugin reacts to events and updates the values its UI declares as bound. It never touches LVGL, files or drivers.

```c
#include "aitbase_plugin.h"

aitbase_plugin_t *p = aitbase_plugin_connect(NULL);
aitbase_ui_open_file(p, "ui.json", 30000);
aitbase_event_t ev;
int count = 0;
while (aitbase_plugin_next_event(p, &ev, -1) == 1) {
    if (ev.kind == AITBASE_EVENT_OPENED || ev.kind == AITBASE_EVENT_ACTION) {
        char text[32];
        snprintf(text, sizeof(text), "Count: %d", ev.kind == AITBASE_EVENT_ACTION ? ++count : count);
        aitbase_ui_set_text(p, "status.title", "title", text);
    } else if (ev.kind == AITBASE_EVENT_PREEMPTED || ev.kind == AITBASE_EVENT_CLOSED ||
               ev.kind == AITBASE_EVENT_REJECTED) {
        break;
    }
}
aitbase_plugin_disconnect(p);
```

For complete plugins, see [aitbase-vision-plugins](https://github.com/Aitbase/aitbase-vision-plugins).

## Using the SDK in a plugin project

Add the SDK as a git submodule pinned to a release tag:

```cmake
add_subdirectory(sdk)                       # this repository
add_executable(my_plugin src/main.c)
target_link_libraries(my_plugin PRIVATE aitbase::plugin)
aitbase_plugin_add_ui(my_plugin XML ui/ui.xml OUTPUT MY_PLUGIN_UI)
```

Requirements:
- CMake 3.13 or later;
- a C99 compiler;
- Python 3.8 or later (for the UI compiler; standard library only).

For a device build, pass your device's cross toolchain file (`-DCMAKE_TOOLCHAIN_FILE=...`).

## Building and testing the SDK

```bash
cmake -S . -B build
cmake --build build
ctest --test-dir build --output-on-failure
```

## Roadmap

These items are planned, not implemented:
- **WASM:** plugins compiled to WebAssembly, running sandboxed on the device. The same `aitbase_plugin.h` API will be used, with a WASM transport.
- **More capabilities:** camera, audio, HTTP, storage and MCP tools, each behind explicit user permission.
- **Packaging and installation:** a package format with a manifest, signing, installation from the device's web console, and the `aitbase-plugin` CLI (`init`, `build`, `check`, `dev`).
- **Richer UI:** a larger UI schema (more widgets, styles and bindings).

## License

To be decided.
