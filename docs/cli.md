# `aitbase-plugin` CLI

`aitbase-plugin` creates, checks, builds and packages plugins. It is one program with no runtime dependencies, built for Windows, macOS and Linux.

> **Preview.** `init`, `check`, `build`, `pack`, `key`, `toolchain`, `sdk`, `version`, and the device commands `pair`, `dev`, `logs`, `screenshot`, `press` and `unpair` work today. The device commands need device firmware with the plugin developer routes. `build` needs CMake. A native build also needs a C compiler; `build --wasm` downloads its own. `check` uses Python 3.8+ to compile screens. `devices` and `pack-sd` are reserved names that return `NOT_AVAILABLE`.

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
| `build [dir] [--wasm] [--build-dir <path>] [--sdk <dir>]` | Configures and builds with CMake, passing the SDK directory as `AITBASE_SDK_DIR` (see [Which SDK](#which-sdk)). Build output goes to stderr. Without `--wasm`: a native build for the development bridge, in `<dir>/build`. With `--wasm`: WebAssembly for the device, in `<dir>/build-wasm`; see [WebAssembly](#webassembly). |
| `pack [dir] [--wasm] [--build-dir <path>] [--out <path>]` | With `--wasm`: reads `<dir>/build-wasm` and writes the signed `<id>-<version>.avp` for the device's SD card. Without it: reads a native build from `<dir>/build` and writes `<id>-<version>-native.zip`, which devices don't run. Both go to the output directory (default `<dir>/dist`) and print their SHA-256. |
| `key` | Shows your signing key's path and author fingerprint, creating the key if it doesn't exist yet. |
| `toolchain` | Downloads wasi-sdk if needed and prints its path, for running CMake yourself or setting up an editor. |
| `sdk [dir]` | Prints the SDK directory the project builds with (see [Which SDK](#which-sdk)), for running CMake yourself. |
| `version` | Prints the SDK version. |
| `pair <address> [--name <label>]` | Pairs this computer with a device, once. Asks for the Device Console password at a hidden prompt; see [On a device](#on-a-device). |
| `dev [dir] [--device <address>]` | Builds WebAssembly, packs the signed `.avp`, uploads it to the device's SD card and waits until the device runs it. |
| `logs [dir] [--follow] [--id <id>] [--device <address>]` | Prints the plugin's log from the device; `--follow` keeps printing new lines. |
| `screenshot [-o <file.png>] [--device <address>]` | Saves the plugin's screen on the device as a PNG (default `screenshot.png`). |
| `press left\|right [--long] [--device <address>]` | Presses a button on the device, as the hardware would. |
| `unpair [--device <address>]` | Revokes this computer's token on the device and forgets the device. |

`check` and `pack` also accept:
- `--sdk <dir>`: the SDK holding the UI compiler (see [Which SDK](#which-sdk)); `pack` doesn't need one and ignores it;
- `--python <path>`: the Python 3 interpreter;
- `--allow-reserved-id`: allow the `aitbase.*` id namespace (official plugins only).

`check` also accepts `--skip-ui`, which skips compiling screens.

Flags may come before or after the directory. Paths given to `--sdk`, `--build-dir` and `--out` are relative to the current directory, or absolute; only the defaults are inside the project.

## Which SDK

`check`, `build` and `sdk` use, in order:
1. `--sdk <dir>`, when given;
2. the nearest `sdk/` in the project directory or a parent, such as the copy `init` writes;
3. the SDK built into the CLI. The first time, the CLI unpacks it into your user cache folder (`aitbase-plugin/sdk-<version>-<digest>` there); later commands reuse it.

So a project needs no copy of the SDK and no git submodule: the CLI's version is the SDK version. `build` passes the directory to CMake as `AITBASE_SDK_DIR`, which a plugin without its own `sdk/` adds:

```cmake
add_subdirectory(${AITBASE_SDK_DIR} ${CMAKE_BINARY_DIR}/aitbase-sdk)
```

To run CMake yourself, pass `-DAITBASE_SDK_DIR="$(aitbase-plugin sdk)"`.

## On a device

Instead of copying the `.avp` to the SD card yourself, pair the computer with the device once, then let `dev` build, upload and start the plugin:

```sh
aitbase-plugin pair 192.168.1.50   # you type the Device Console password, once
aitbase-plugin dev                  # build --wasm, pack --wasm, upload, wait until it runs
aitbase-plugin logs --follow
```

- **The address** is the device's IP address on your network, as the Device Console's address bar shows it. The computer and the device must be on the same network.
- **Pairing asks for the password at a hidden prompt, and nowhere else:** not as a flag, an environment variable or a file. If you use an AI coding agent, run `pair` yourself; the agent can then run `dev` and `logs`. Without a terminal, `pair` fails with `PASSWORD_NEEDS_TERMINAL`.
- **The device refuses pairing while it uses its default password.** Change the password on the Device Console once.
- **The token** that pairing returns is kept in your user configuration folder (`aitbase-plugin/devices.json` there, or `$AITBASE_DEVICES`), readable only by you. It can upload plugins and read their logs, nothing else. It expires after 30 days. The device's Plugins page lists paired computers and revokes them; changing the Device Console password revokes them all.
- **What `dev` does on the device:** the upload goes into `aitbase/plugins/` on the SD card, like a copied file, so the card must be in and stay there. The device checks the signature and runs it within a few seconds, replacing the plugin that runs. It refuses `aitbase.*` ids and packages signed by another key than the one the plugin was first signed with.
- **See and press:** `screenshot` saves the device's screen as a PNG and `press left`, `press right` or `press left --long` press its buttons, so you or an agent can check the plugin without looking at the device. Both work only while a plugin's own screen shows, with nothing over it; on any other screen they fail with `DEVICE_NOT_FOREGROUND`. On the plugin screen, LEFT moves the focus, RIGHT activates the focused widget, and a LEFT long press leaves the plugin.
- **Several devices:** the last one paired is the default; pass `--device <address>` for another.
- **Plain HTTP:** devices serve the local network over plain HTTP. Don't pair over a network you don't trust.

## WebAssembly

Devices run community plugins as WebAssembly. Two commands build one and package it:

```sh
aitbase-plugin build --wasm
aitbase-plugin pack --wasm     # dist/<id>-<version>.avp, signed
```

- **Toolchain:** `build --wasm` uses [wasi-sdk](https://github.com/WebAssembly/wasi-sdk) 34.0. The first time, it downloads it, about 200 MB, into your user cache folder (`aitbase-plugin/` there). It checks the download against the SHA-256 pinned in the SDK before using it; a mismatch stops the build. Every later build reuses it. To use a wasi-sdk you already have, set `AITBASE_WASI_SDK` to its directory.
- **Systems:** wasi-sdk is published for Linux, macOS and Windows on x86-64 and ARM64. On Windows, `build --wasm` also needs [Ninja](https://ninja-build.org/).
- **What `pack --wasm` checks:** it strips debug information from the module, then refuses it if the device would: imports the device does not provide (for example files or sockets through WASI), missing exports, a start section, more than 2 MiB of initial memory, or more than 4 MiB.
- **On the device:** copy the `.avp` into `aitbase/plugins/` on the SD card (create the folders if needed). The device checks the signature and runs the newest package by itself. The log is in `aitbase/logs/<id>.log` on the card and on the Plugins page.
- **Signing:** the first `pack --wasm` creates your signing key (see `key`) in your user configuration folder, or at `$AITBASE_SIGNING_KEY`. A device remembers the first key it sees for a plugin id and refuses packages of that id signed with another key, so **back the key up** and use it on every computer you build on. If you lose it, the device owner has to forget the plugin in the Device Console, which deletes its saved data.

The `.avp` format is described in [package format](package-format.md). Devices no longer run unzipped plugin folders.

## JSON output

With `--json`, a command prints exactly one JSON object on stdout, and nothing else goes to stdout:

```json
{
  "ok": false,
  "command": "check",
  "sdk_version": "0.11.1",
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

- `pack` adds `package`, `sha256` and `bytes`; with `--wasm` also `module_bytes`, `sd_dir`, `author` (the key's fingerprint), `signing_key` and `key_created`.
- `key` adds `path`, `fingerprint` and `created`.
- `toolchain` adds `wasi_sdk`, `wasi_sdk_version` and `toolchain_file`.
- `sdk` adds `sdk_dir` and `bundled` (true for the SDK built into the CLI).
- `init` adds `dir`, `id` and `files`.
- `build` adds `build_dir`, `sdk_dir` and `wasm`, and with `--wasm` also `wasi_sdk`.
- `pair` adds `device`, `token_id`, `expires_at` and `devices_file`; never the token.
- `dev` adds `device`, `package`, `sha256`, `author`, `plugin_id`, `version`, `file` and `running`.
- `logs` adds `device`, `plugin_id`, `text` and `next_offset`.
- `screenshot` adds `device`, `file`, `width` and `height`.
- `press` adds `device`, `button` and `long`.
- `unpair` adds `device`.

Exit codes:

| Code | Meaning |
|---|---|
| 0 | Success. |
| 1 | Failed; see `diagnostics`. |
| 2 | Usage error. |
| 3 | Not available in this SDK or on current devices. |

## Diagnostic codes

Retired in SDK 0.11, when devices stopped needing `requested_capabilities`: `CAPABILITY_UNAVAILABLE`, `CAPABILITY_FIRMWARE_ONLY`, `CAPABILITY_UNKNOWN`, `CAPABILITY_UI_MISSING`, `CAPABILITY_AUDIO_MISSING` and `TOOLS_GRANT_MISSING`. Codes are never reused.

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
| `CAPABILITIES_IGNORED` | Warning: `plugin.json` has `requested_capabilities`, which devices no longer need (SDK 0.11, device decision D32). Remove it. |
| `TRIGGER_FORMAT` | `triggers` is not a list of at most 4. |
| `TRIGGER_UNKNOWN` | A trigger's `event` is not one devices support (`schedule`). |
| `TRIGGER_SCHEDULE` | A schedule needs `every_min`, a whole number from 15 to 1440, and only one schedule is allowed. |
| `IDLE_FACE_SCREEN` | `idle_face` doesn't name one of `ui.screens`. |
| `TOOLS_FORMAT` | `assistant_tools` isn't a list of at most 4 tools, or a tool has fields other than `name`, `description` and `args`. |
| `TOOL_NAME` | A tool's name isn't lowercase letters, digits and `_` starting with a letter (at most 24), or is declared twice. |
| `TOOL_DESCRIPTION` | A tool has no description, or it isn't one line of at most 120 bytes without control or invisible characters. |
| `TOOL_ARG` | A tool has more than 4 arguments, or an argument isn't a `string` (`max` 1 to 120), an `int` (`min` and `max`) or an `enum` (1 to 8 values). |
| `IDLE_FACE_BOUNDS` | The `idle_face` screen can't be a face: it has a button or action, binds a key other than `face.1` to `face.16`, or has more than 40 widgets or 12 KiB compiled. |
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
| `SDK_UNAVAILABLE` | The CLI could not unpack its built-in SDK into the user cache folder. |
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
| `SIGNING_KEY` | The signing key could not be read or created. Restore your backup, or point `AITBASE_SIGNING_KEY` at it. |
| `NOT_AVAILABLE` | The command needs device support that does not exist yet. |
| `PASSWORD_NEEDS_TERMINAL` | `pair` runs only in an interactive terminal, where it asks for the Device Console password. |
| `DEVICE_UNREACHABLE` | Nothing answers at the device address. |
| `DEVICE_NOT_SUPPORTED` | The device's firmware has no plugin developer routes. |
| `DEVICE_NOT_PAIRED` | No device is paired, the address isn't, or its token expired or was revoked: pair again. |
| `DEVICE_STORE` | The paired devices file could not be read or written. |
| `DEVICE_DEFAULT_PASSWORD` | The device refuses pairing while it uses its default password. |
| `DEVICE_PASSWORD_INCORRECT` | The Device Console password is not correct. |
| `DEVICE_RATE_LIMITED` | Too many wrong passwords; `fix` says how long to wait. |
| `DEVICE_NO_SD_CARD` | The device has no SD card with an `aitbase/` folder. |
| `DEVICE_UPLOAD_FAILED` | The upload didn't arrive intact, or the SD card couldn't be written. |
| `DEVICE_PACKAGE_REFUSED` | The device refused the package: its signature, contents or id. |
| `DEVICE_AUTHOR_CHANGED` | The plugin id is pinned to another signing key on the device. |
| `DEVICE_PACKAGE_SKIPPED` | The package is on the SD card, but the device skipped it; `message` says why. |
| `DEVICE_PLUGIN_ENDED` | The plugin started and ended at once; read `aitbase-plugin logs`. |
| `DEVICE_TIMEOUT` | The package is on the SD card, but the device didn't start it in time. |
| `DEVICE_NOT_FOREGROUND` | `screenshot` and `press` work only while a plugin's own screen shows on the device. |
| `DEVICE_BUSY` | The previous press is still being handled. |
| `DEVICE_NO_DISPLAY` | The device's display is off or not working. |
| `DEVICE_ERROR` | Another error from the device; `message` has its words. |
