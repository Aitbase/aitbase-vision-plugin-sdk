# `aitbase-plugin` CLI

`aitbase-plugin` creates, checks, builds and packages plugins. It is one program with no runtime dependencies, built for Windows, macOS and Linux.

> **Preview.** `init`, `check`, `build`, `pack`, `toolchain` and `version` work today. `build` needs CMake. A native build also needs a C compiler; `build --wasm` downloads its own. `check` uses Python 3.8+ to compile screens. The device commands (`devices`, `pair`, `dev`, `logs`, `screenshot`, `press`, `pack-sd`) are reserved names that return `NOT_AVAILABLE` until device firmware supports them.

## Install

One command installs the CLI and the WebAssembly toolchain it builds plugins with:

```sh
# Linux and macOS
curl -fsSL https://raw.githubusercontent.com/Aitbase/aitbase-vision-plugin-sdk/main/install.sh | sh
```

```powershell
# Windows (PowerShell)
irm https://raw.githubusercontent.com/Aitbase/aitbase-vision-plugin-sdk/main/install.ps1 | iex
```

The installer:
- downloads the newest release for your system;
- checks it against the release's `SHA256SUMS`;
- installs it in `~/.local/bin`, or `%LOCALAPPDATA%\aitbase\bin` on Windows, which it adds to your PATH;
- runs `aitbase-plugin toolchain`, which downloads wasi-sdk, about 200 MB, once.

It then lists anything else you need: CMake and Python 3, plus Ninja on Windows.

To change what it does, set these environment variables:
- `AITBASE_VERSION=v0.4.0` pins a release;
- `AITBASE_INSTALL_DIR` picks the folder;
- `AITBASE_SKIP_TOOLCHAIN=1` leaves the toolchain for the first `build --wasm`.

Read the script before you pipe it to a shell if you prefer: [install.sh](../install.sh), [install.ps1](../install.ps1).

### Manual install

Download the file for your system from the [latest release](https://github.com/Aitbase/aitbase-vision-plugin-sdk/releases):

| System | File |
|---|---|
| Windows | `aitbase-plugin-windows-amd64.exe` (`-arm64.exe` on ARM) |
| macOS, Apple silicon | `aitbase-plugin-darwin-arm64` |
| macOS, Intel | `aitbase-plugin-darwin-amd64` |
| Linux | `aitbase-plugin-linux-amd64` (`-arm64` on ARM) |

Then:
1. **Check the download.** Compare its SHA-256 with the line in `SHA256SUMS`:
   - Windows: `Get-FileHash <file>`;
   - macOS: `shasum -a 256 <file>`;
   - Linux: `sha256sum <file>`.
2. **Rename it** to `aitbase-plugin` (`aitbase-plugin.exe` on Windows) and put it in a folder on your `PATH`.
3. **macOS and Linux:** make it executable with `chmod +x aitbase-plugin`.
4. **Allow the unsigned binary:**
   - macOS blocks downloaded unsigned programs. Remove the quarantine flag with `xattr -d com.apple.quarantine aitbase-plugin`.
   - Windows SmartScreen may warn on first run.

   The binaries are not code-signed yet.
5. **Confirm it works** with `aitbase-plugin version`.

With Go 1.22 or later, you can instead build it from source:

```sh
go install github.com/Aitbase/aitbase-vision-plugin-sdk/cmd/aitbase-plugin@latest
```

## Commands

| Command | What it does |
|---|---|
| `init <dir> --id <id> [--name <name>]` | Creates a plugin project: code, a screen, `AGENTS.md`, agent skills and a copy of the SDK in `sdk/`. |
| `check [dir]` | Validates `plugin.json`, ids, file names and screens. |
| `build [dir] [--wasm] [--build-dir <path>]` | Configures and builds with CMake. Build output goes to stderr. Without `--wasm`: a native build for the development bridge, in `<dir>/build`. With `--wasm`: WebAssembly for the device, in `<dir>/build-wasm`; see [WebAssembly](#webassembly). |
| `pack [dir] [--wasm] [--build-dir <path>] [--out <path>]` | Without `--wasm`: reads the program and compiled screens from `<dir>/build` and writes `<id>-<version>.avp`. With `--wasm`: reads `<dir>/build-wasm` and writes `<id>-<version>-wasm.zip` for the device's SD card. Both go to the output directory (default `<dir>/dist`) and print their SHA-256. |
| `toolchain` | Downloads wasi-sdk if needed and prints its path, for running CMake yourself or setting up an editor. |
| `version` | Prints the SDK version. |

`check` and `pack` also accept:
- `--sdk <dir>`: the SDK holding the UI compiler. The default is the nearest `sdk/` in the project directory or a parent, so a plugin in a repository of plugins uses the repository's SDK;
- `--python <path>`: the Python 3 interpreter;
- `--allow-reserved-id`: allow the `aitbase.*` id namespace (official plugins only).

`check` also accepts `--skip-ui`, which skips compiling screens.

Flags may come before or after the directory. Paths given to `--sdk`, `--build-dir` and `--out` are relative to the current directory, or absolute; only the defaults are inside the project.

## WebAssembly

Devices run community plugins as WebAssembly. Two commands build one and package it:

```sh
aitbase-plugin build --wasm
aitbase-plugin pack --wasm     # dist/<id>-<version>-wasm.zip
```

- **Toolchain:** `build --wasm` uses [wasi-sdk](https://github.com/WebAssembly/wasi-sdk) 34.0. The first time, it downloads it, about 200 MB, into your user cache folder (`aitbase-plugin/` there). It checks the download against the SHA-256 pinned in the SDK before using it; a mismatch stops the build. Every later build reuses it. To use a wasi-sdk you already have, set `AITBASE_WASI_SDK` to its directory.
- **Systems:** wasi-sdk is published for Linux, macOS and Windows on x86-64 and ARM64. On Windows, `build --wasm` also needs [Ninja](https://ninja-build.org/).
- **What `pack --wasm` checks:** it strips debug information from the module, then refuses it if the device would: imports the device does not provide (for example files or sockets through WASI), missing exports, a start section, more than 2 MiB of initial memory, or more than 4 MiB.
- **On the device:** unzip the package at the root of the SD card. This needs **Developer mode**, which is on by default; an owner who turned it off turns it back on in the Device Console under **Advanced → Plugins**. It creates `aitbase/plugins/<id>/` with `<entry>.wasm`, the compiled screens and `plugin.json`, and the device runs the newest plugin there by itself. The log is in `aitbase/logs/<id>.log` on the card and on the Plugins page.

The zip is an interim format: the planned community package is a signed `.avp` that the device verifies before running it ([package format](package-format.md)).

## JSON output

With `--json`, a command prints exactly one JSON object on stdout, and nothing else goes to stdout:

```json
{
  "ok": false,
  "command": "check",
  "sdk_version": "0.5.0",
  "diagnostics": [
    {
      "severity": "error",
      "code": "SOURCE_ID_MISMATCH",
      "path": "src/main.c",
      "message": "AITBASE_PLUGIN_ID(\"alice.other\") does not match id \"alice.counter\" in plugin.json",
      "fix": "make them the same"
    }
  ]
}
```

- `pack` adds `package`, `sha256` and `bytes`; with `--wasm` also `module_bytes` and `sd_dir`.
- `toolchain` adds `wasi_sdk`, `wasi_sdk_version` and `toolchain_file`.
- `init` adds `dir`, `id` and `files`.
- `build` adds `build_dir` and `wasm`, and with `--wasm` also `wasi_sdk`.

Exit codes:

| Code | Meaning |
|---|---|
| 0 | Success. |
| 1 | Failed; see `diagnostics`. |
| 2 | Usage error. |
| 3 | Not available in this SDK or on current devices. |

## Diagnostic codes

Codes are stable: a code keeps its meaning once released. `path` is relative to the project. `plugin.json#field` points at a manifest field.

| Code | Meaning |
|---|---|
| `MANIFEST_MISSING` | There is no `plugin.json` in the directory. |
| `MANIFEST_JSON` | `plugin.json` is not a JSON object. |
| `MANIFEST_FIELD` | A required field is missing, too long or the wrong type. |
| `MANIFEST_UNKNOWN_FIELD` | Warning: a field is not part of the manifest, often a typo. |
| `ID_FORMAT` | The id is not two or more dot-separated lowercase parts, at most 64 characters. |
| `ID_RESERVED` | The id uses the `aitbase.*` namespace, which is reserved for official plugins. |
| `VERSION_FORMAT` | The version is not `MAJOR.MINOR.PATCH`. |
| `API_VERSION_UNSUPPORTED` | This SDK does not support that `api_version`. |
| `ENTRY_FORMAT` | `entry` is not a valid build target name. |
| `SCREEN_NAME` | A screen name is invalid or listed twice. |
| `SCREEN_MISSING` | A listed screen has no `ui/screens/<name>.xml`. |
| `CAPABILITY_UNAVAILABLE` | The capability is planned but no device supports it yet. |
| `CAPABILITY_UNKNOWN` | The capability does not exist. |
| `CAPABILITY_UI_MISSING` | The plugin has screens but does not request `ui`. |
| `CAPABILITY_AUDIO_MISSING` | The plugin lists sounds but does not request `audio.output`. |
| `SOUND_NAME` | A sound name is not lowercase letters, digits and `_` (at most 32), or is listed twice. |
| `SOUND_MISSING` | A listed sound has no `sounds/<name>.wav`. |
| `SOUND_FORMAT` | The sound is not a 16 kHz, mono, 16-bit PCM WAV of at most 10 seconds. |
| `PATH_CHARS` | A file name uses characters other than ASCII letters, digits, `.`, `-` and `_`, or has an empty, `.` or `..` part. |
| `PATH_TRAILING` | A file name ends with `.` or a space. |
| `PATH_RESERVED_NAME` | A file name is reserved on Windows (`con`, `aux`, `nul`, `com1`…). |
| `PATH_CASE_COLLISION` | Two files differ only by letter case. |
| `FILE_SYMLINK` | A symbolic link is in the plugin. |
| `SOURCE_ID_MISMATCH` | `AITBASE_PLUGIN_ID(...)` in `src/` differs from `id` in `plugin.json`. |
| `SOURCE_ID_MISSING` | No `AITBASE_PLUGIN_ID(...)` in `src/`. |
| `SOURCE_ID_DUPLICATE` | `AITBASE_PLUGIN_ID(...)` appears more than once. |
| `UI_COMPILER_UNAVAILABLE` | Warning: Python 3 or the UI compiler was not found, so screens were not compiled. |
| `UI_COMPILE` | The UI compiler failed without a specific code. |
| `UI_*` | Any other `UI_` code comes from the UI compiler; see [UI XML](ui-xml.md). |
| `USAGE` | The command line is wrong. |
| `INIT_FAILED` | `init` could not create the project, for example because the directory is not empty. |
| `CMAKE_NOT_FOUND` | CMake is not installed. |
| `BUILD_FAILED` | CMake configure or build failed; the compiler output is on stderr. |
| `BUILD_OUTPUT_MISSING` | `pack` could not find the program or a compiled screen. Run `build` first (`build --wasm` for `pack --wasm`). |
| `WASI_SDK_UNAVAILABLE` | `build --wasm` or `toolchain` could not download or find wasi-sdk, or the download did not match its pinned SHA-256. |
| `NINJA_NOT_FOUND` | `build --wasm` on Windows needs Ninja. |
| `WASM_INVALID` | `pack --wasm` found no valid WebAssembly module: a native build, or a damaged file. |
| `WASM_TOO_LARGE` | The module is over the device's 4 MiB limit. |
| `WASM_MEMORY_TOO_LARGE` | The module starts with more than the device's 2 MiB (32 pages) of memory, usually because of large static arrays. Make them smaller, or allocate them with `malloc`. |
| `WASM_IMPORT_REFUSED` | The module imports something the device does not provide, for example a file or socket call through WASI. |
| `WASM_EXPORT_MISSING` | The module lacks the exports the device calls; it was not linked with `aitbase::plugin` for WebAssembly. |
| `WASM_START_SECTION` | The module has a start section, which the device refuses; build it with the SDK's CMake. |
| `PACKAGE_TOO_LARGE` | The package is over the 4 MB limit. |
| `WRITE_FAILED` | The package could not be written. |
| `NOT_AVAILABLE` | The command needs device support that does not exist yet. |
