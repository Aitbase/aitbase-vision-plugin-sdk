# AITBASE VISION Plugin SDK

Build plugins that put their own screen on an AITBASE VISION device.

> **Status: 0.6.0 preview.** API version 1 and UI schema v1 may still change before 1.0. Current device firmware runs signed WebAssembly plugins from the SD card. Plugins can show screens, keep small values, make HTTP requests and play sounds ([capabilities](docs/capabilities.md)). Packages are signed `.avp` files; store distribution is not available yet.

## Quick start

```sh
# Install the CLI and the WebAssembly toolchain (Windows: see docs/cli.md#install)
curl -fsSL https://raw.githubusercontent.com/Aitbase/aitbase-vision-plugin-sdk/main/install.sh | sh

aitbase-plugin init my-plugin --id yourname.my-plugin
cd my-plugin
aitbase-plugin check
aitbase-plugin build --wasm   # downloads the WebAssembly toolchain the first time
aitbase-plugin pack --wasm    # dist/yourname.my-plugin-0.1.0.avp, signed
```

Copy the `.avp` into `aitbase/plugins/` on the device's SD card, and the device runs it. The first `pack --wasm` creates your signing key: back it up. See [WebAssembly](docs/cli.md#webassembly).

The new project is ready for AI coding agents:
- **`AGENTS.md` and `CLAUDE.md`:** what plugins can and cannot do, and the check/build loop.
- **Task skills** in `.agents/skills/` and `.claude/skills/`.
- **`--json` output** on every command, with stable [diagnostic codes](docs/cli.md#diagnostic-codes).

The project also includes a copy of this SDK in `sdk/`, so it builds without git or network access.

**Platforms:**
- `init`, `check` and `pack` run on Windows, macOS and Linux.
- `build --wasm` needs CMake and Python 3.8+ (and Ninja on Windows). It downloads its compiler, [wasi-sdk](https://github.com/WebAssembly/wasi-sdk), the first time.
- A native `build`, for the development bridge, needs CMake, a C compiler and Python 3.8+. The native runtime is POSIX only.

## What is in the SDK

| Path | Contents |
|---|---|
| `include/aitbase_plugin.h` | The C plugin API: callbacks (`aitbase_on_start`, `aitbase_on_event`, `aitbase_on_stop`) plus calls to open a screen, update bound values, set a timer and log. |
| `src/` | The runtimes. The development runtime runs a plugin as a native process connected to a developer device ([protocol](docs/plugin-protocol.md)). The WASM runtime (preview) builds the same plugin as WebAssembly for the device's sandbox ([WASM host ABI](docs/wasm-abi.md)). |
| `schema/plugin_ui_registry.json` | UI schema v1: allowed widgets, properties, colors, actions, bindings and limits. This file is the source of truth for both the compiler and the device validator. |
| `schema/aitbase_globals.xml` | Platform color tokens and subjects, in LVGL `globals.xml` form. |
| `tools/plugin_ui_compile.py` | The compiler from plugin UI XML (LVGL 9.4 conventions, [reference](docs/ui-xml.md)) to the device's `aitbase.plugin-ui` document. |
| `tools/plugin_ui_registry.py` | Generates the device validator header and `aitbase_globals.xml` from the schema. |
| `cmake/AitbasePluginUi.cmake` | `aitbase_plugin_add_ui()`, which compiles screens as part of a CMake build. |
| `cmd/aitbase-plugin/` | The `aitbase-plugin` CLI ([reference](docs/cli.md)). |
| `template/` | The project that `aitbase-plugin init` creates, including its agent instructions and skills. |
| `schema/plugin.schema.json` | The `plugin.json` manifest schema (draft). |
| `docs/package-format.md` | The `.avp` package format (draft) and SD card layout. |
| `llms.txt` | An index of these docs for AI agents. |

## How a plugin works

A plugin is a set of callbacks. The runtime connects to the device, calls `aitbase_on_start()`, then delivers events one at a time until the plugin stops. Nothing blocks. The WASM runtime uses the same model, so the same plugin code builds natively or as WebAssembly.

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
go vet ./... && go test ./...
cmake -S . -B build
cmake --build build
ctest --test-dir build --output-on-failure
```

Contributors: see [AGENTS.md](AGENTS.md).

## Roadmap

These items are planned, not implemented:
- **More capabilities:** camera, microphone and MCP tools, each behind explicit user permission. Storage, HTTP and sound are available ([docs/capabilities.md](docs/capabilities.md)); later they will also need the owner's approval.
- **Installation and device tools:** signed packages, installation from the SD card (`aitbase/plugins/`) and the device's web console, and the CLI device commands (`pair`, `dev`, `logs`, `screenshot`, `press`).
- **No Python or C toolchain to install:** the UI compiler built into the CLI, and the WASM toolchain downloaded automatically.
- **Richer UI:** a larger UI schema (more widgets, styles and bindings).

## License

To be decided.
