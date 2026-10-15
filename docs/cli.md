# `aitbase-plugin` CLI

`aitbase-plugin` creates, checks, builds and packages plugins. It is one program with no runtime dependencies, built for Windows, macOS and Linux.

> **Preview.** `init`, `check`, `build`, `pack` and `version` work today. `build` still needs CMake and a C compiler, and `check` uses Python 3.8+ to compile screens. The device commands (`devices`, `pair`, `dev`, `logs`, `screenshot`, `press`, `pack-sd`) are reserved names that return `NOT_AVAILABLE` until device firmware supports them.

## Install

Until releases are published, build it with Go 1.22 or later:

```sh
go install github.com/Aitbase/aitbase-vision-plugin-sdk/cmd/aitbase-plugin@latest
```

## Commands

| Command | What it does |
|---|---|
| `init <dir> --id <id> [--name <name>]` | Creates a plugin project: code, a screen, `AGENTS.md`, agent skills and a copy of the SDK in `sdk/`. |
| `check [dir]` | Validates `plugin.json`, ids, file names and screens. |
| `build [dir] [--build-dir build]` | Configures and builds with CMake. Build output goes to stderr. |
| `pack [dir] [--build-dir build] [--out dist]` | Writes `dist/<id>-<version>.avp` and prints its SHA-256. |
| `version` | Prints the SDK version. |

`check` and `pack` also accept:
- `--sdk <dir>`: the SDK holding the UI compiler (default `<dir>/sdk`);
- `--python <path>`: the Python 3 interpreter;
- `--allow-reserved-id`: allow the `aitbase.*` id namespace (official plugins only).

`check` also accepts `--skip-ui`, which skips compiling screens.

Flags may come before or after the directory.

## JSON output

With `--json`, a command prints exactly one JSON object on stdout, and nothing else goes to stdout:

```json
{
  "ok": false,
  "command": "check",
  "sdk_version": "0.3.0",
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

- `pack` adds `package`, `sha256` and `bytes`.
- `init` adds `dir`, `id` and `files`.
- `build` adds `build_dir`.

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
| `BUILD_OUTPUT_MISSING` | `pack` could not find the program or a compiled screen. Run `build` first. |
| `PACKAGE_TOO_LARGE` | The package is over the 4 MB limit. |
| `WRITE_FAILED` | The package could not be written. |
| `NOT_AVAILABLE` | The command needs device support that does not exist yet. |
