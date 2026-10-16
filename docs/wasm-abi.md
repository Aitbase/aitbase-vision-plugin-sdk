# WASM host ABI (version 1, preview)

How a plugin compiled to WebAssembly talks to the device's plugin runner. **Plugins do not use it directly.** They call `aitbase_plugin.h`, and the SDK's WASM runtime (`src/aitbase_plugin_wasm.c`) implements this ABI. It is written down for SDK maintainers and for other language SDKs.

> **Preview.** Device firmware with WASM support runs it on development builds only (`CC_ENABLE_PLUGIN_SUPERVISOR=ON`). It may change before SDK 1.0.

## Model

The device owns the loop and calls the plugin's exports one at a time, never concurrently. Each call runs under an instruction budget. A plugin that exceeds it, traps or breaks these rules is stopped, and its screen is closed. Nothing in the ABI blocks.

Inside the plugin the messages are the same JSON lines as [protocol v1](plugin-protocol.md), without the newline. The runner only moves them between the plugin and the device. So the device sees the same traffic from a native plugin and a WASM plugin.

## Building

Build with [wasi-sdk](https://github.com/WebAssembly/wasi-sdk) 34 or later as a **reactor** (no `main`); the SDK's CMake does this when `CMAKE_SYSTEM_NAME` is `WASI`:

```sh
cmake -S . -B build-wasm \
  -DCMAKE_TOOLCHAIN_FILE=<wasi-sdk>/share/cmake/wasi-sdk-p1.cmake -DWASI_SDK_PREFIX=<wasi-sdk> \
  -DCMAKE_BUILD_TYPE=MinSizeRel
cmake --build build-wasm
```

The module is written as `build-wasm/<entry>`. A device package names it `<entry>.wasm`. Not yet supported:
- `aitbase-plugin build` and `pack` for WASM;
- `.avp` packages with a module.

## Exports (plugin → device)

The module must export its linear memory as `memory`, and these functions. All integers are `i32`.

| Export | Signature | Called |
|---|---|---|
| `_initialize` | `() -> ()` | Optional; first, once. wasi-sdk reactors run C constructors here. |
| `aitbase_abi_version` | `() -> i32` | Once; must return `1`. |
| `aitbase_rx_buffer` | `() -> i32` | Once; the address of a 4096-byte buffer in linear memory. |
| `aitbase_start` | `() -> i32` | Once. The SDK sends `system.hello` here. |
| `aitbase_message` | `(len) -> i32` | For each message from the device. The runner has copied it, without its newline, to the receive buffer; `len` is at most 4096. |
| `aitbase_timer` | `() -> i32` | When the interval set with `timer_set` elapses. |
| `aitbase_stop` | `() -> ()` | When the device ends the plugin (shutdown, disconnect). The SDK calls `aitbase_on_stop` here. |

`aitbase_start`, `aitbase_message` and `aitbase_timer` return:
- `0` to keep running;
- `1` when the plugin has stopped. The plugin has already closed its screen, and the device ends it normally;
- anything else for an error. The device ends the plugin as failed.

## Imports (device → plugin)

Module `aitbase`:

| Import | Signature | Does |
|---|---|---|
| `send` | `(ptr, len) -> i32` | Sends one protocol message. Returns `0`, `-1` if `len` is 0, over 20480, or the message contains `\n`, or `-3` if the connection is gone. |
| `log` | `(level, ptr, len)` | Writes one line to the plugin log. Levels are `aitbase_log_level_t`. Lines are cut at 255 bytes, control characters are replaced, and the device allows 20 lines a second with bursts of 50. |
| `timer_set` | `(interval_ms) -> i32` | Calls `aitbase_timer` every `interval_ms`; `0` stops. Intervals under 10 ms become 10 ms. Returns `0`, or `-1` over 24 hours. |
| `package_read` | `(name_ptr, name_len, buf_ptr, cap) -> i32` | Copies a compiled screen, `<screen>.ui.json`, from the plugin's package. Returns its size, `-6` if there is no such file, or `-1` if it is larger than `cap`. Screens are at most 16 KiB. |

Module `wasi_snapshot_preview1`: wasi-libc links these into any program that uses stdio, so the device provides them. It provides nothing else from WASI.

| Import | Does |
|---|---|
| `fd_write` | Descriptors 1 and 2 only: complete lines go to the plugin log, stdout as `info` and stderr as `error`. Others fail with `EBADF`. |
| `fd_close`, `fd_seek` | Always fail (`EBADF`, or `ESPIPE` for descriptors 0–2). |
| `clock_time_get` | Realtime (0) and monotonic (1) clocks. |
| `proc_exit` | Ends the plugin: normally with code 0, as failed otherwise. |

**Any other import makes the device refuse the module**, and so does a `start` section (a reactor has none). That includes files, sockets, environment variables, arguments and randomness. Call `aitbase_log`, not `printf`, for logging; `printf` works but is meant for libraries that already use it.

## Limits

| | |
|---|---|
| Module size | 4 MiB |
| Linear memory | 4 MiB (64 pages); `memory.grow` past it returns -1 |
| Instructions per call | Provisional: 50 million counted WebAssembly instructions, to be calibrated per device |
| Threads | None |
| Message to the device | 20480 bytes |
| Message from the device | 4096 bytes |

WebAssembly features: WebAssembly 2.0 features that current clang uses by default, including bulk memory, reference types, multi-value, sign extension and non-trapping float-to-int. No SIMD, threads, exceptions, GC or memory64.
