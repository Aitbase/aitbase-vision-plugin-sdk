# AITBASE VISION Plugin SDK

Build plugins that put their own screen on an AITBASE VISION device.

> **Status: 0.2.0 preview.** API version 1 and UI schema v1 may still change before 1.0. Running plugins currently needs a device on development firmware, and release firmware does not load plugins yet.

## What is in the SDK

| Path | Contents |
|---|---|
| `include/aitbase_plugin.h` | The C plugin API: callbacks (`aitbase_on_start`, `aitbase_on_event`, `aitbase_on_stop`) plus calls to open a screen, update bound values, set a timer and log. |
| `src/` | The development runtime. It runs a plugin as a native process connected to a developer device ([protocol](docs/plugin-protocol.md)). |
| `schema/plugin_ui_registry.json` | UI schema v1: allowed widgets, properties, colors, actions, bindings and limits. This file is the source of truth for both the compiler and the device validator. |
| `schema/aitbase_globals.xml` | Platform color tokens and subjects, in LVGL `globals.xml` form. |
| `tools/plugin_ui_compile.py` | The compiler from plugin UI XML (LVGL 9.4 conventions, [reference](docs/ui-xml.md)) to the device's `aitbase.plugin-ui` document. |
| `tools/plugin_ui_registry.py` | Generates the device validator header and `aitbase_globals.xml` from the schema. |
| `cmake/AitbasePluginUi.cmake` | `aitbase_plugin_add_ui()`, which compiles screens as part of a CMake build. |

## How a plugin works

A plugin is a set of callbacks. The runtime connects to the device, calls `aitbase_on_start()`, then delivers events one at a time until the plugin stops. Nothing blocks. This is the same model the planned WASM runtime uses, so plugin code doesn't change when plugins move to WebAssembly.

The device owns the screen:
- **Rendering:** it validates and draws the plugin's UI itself.
- **Buttons:** LEFT moves focus, RIGHT activates the focused widget, and a LEFT long press always leaves.
- **Taking the screen back:** this happens at any time, for example for the voice assistant.

A plugin never touches LVGL, files or drivers.

```c
#include <stdio.h>
#include "aitbase_plugin.h"

AITBASE_PLUGIN_ID("example.counter");

static int count = 0;

static void show(aitbase_plugin_t *p) {
    char text[32];
    snprintf(text, sizeof(text), "Count: %d", count);
    aitbase_ui_set_text(p, "status.title", "title", text);
}

int aitbase_on_start(aitbase_plugin_t *p) {
    return aitbase_ui_open(p, "counter", AITBASE_UI_MAX_WAIT_MS);   /* ui/screens/counter.xml */
}

void aitbase_on_event(aitbase_plugin_t *p, const aitbase_event_t *ev) {
    switch (ev->kind) {
        case AITBASE_EVENT_OPENED: show(p); break;
        case AITBASE_EVENT_ACTION: ++count; show(p); break;
        case AITBASE_EVENT_PREEMPTED:
        case AITBASE_EVENT_REJECTED:
        case AITBASE_EVENT_CLOSED: aitbase_plugin_stop(p); break;
        default: break;
    }
}
```

For complete plugins, see [aitbase-vision-plugins](https://github.com/Aitbase/aitbase-vision-plugins).

## Using the SDK in a plugin project

Add the SDK as a git submodule pinned to a release tag:

```cmake
add_subdirectory(sdk)                                  # this repository
add_executable(counter src/main.c)                     # no main(): the SDK provides it
target_link_libraries(counter PRIVATE aitbase::plugin)
aitbase_plugin_add_ui(counter SCREENS counter)         # ui/screens/counter.xml -> counter.ui.json
```

Requirements:
- CMake 3.13 or later;
- a C99 compiler;
- Python 3.8 or later (for the UI compiler; standard library only).

For a device build, pass your device's cross toolchain file (`-DCMAKE_TOOLCHAIN_FILE=...`). Copy the binary and its `*.ui.json` files to the device together.

## Building and testing the SDK

```bash
cmake -S . -B build
cmake --build build
ctest --test-dir build --output-on-failure
```

## Roadmap

These items are planned, not implemented:
- **WASM:** plugins compiled to WebAssembly, running sandboxed on the device, with the same callbacks and calls.
- **More capabilities:** camera, audio, HTTP, storage and MCP tools, each behind explicit user permission.
- **Packaging and installation:** a package format with a manifest, signing, installation from the device's web console, and the `aitbase-plugin` CLI (`init`, `build`, `check`, `dev`).
- **Richer UI:** a larger UI schema (more widgets, styles and bindings).

## License

To be decided.
