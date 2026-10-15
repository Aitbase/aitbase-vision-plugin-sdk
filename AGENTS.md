# AITBASE VISION Plugin SDK: instructions for coding agents

## Are you building a plugin?

Do not work inside this repository. Create a plugin project and follow the `AGENTS.md` it contains:

```sh
aitbase-plugin init my-plugin --id yourname.my-plugin
```

That project includes the SDK in `sdk/`, task skills in `.agents/skills/` and `.claude/skills/`, and the commands to check, build and package. The same instructions live here in [`template/AGENTS.md`](template/AGENTS.md).

The rest of this file is for changing the SDK itself.

## What this repository is

The public SDK for AITBASE VISION plugins. Its readers are customers who own a device and often build with an AI coding agent. They have no access to the device firmware or app source, so nothing here may depend on or refer to private repositories, paths or headers.

| Path | Contents |
|---|---|
| `include/aitbase_plugin.h` | Plugin C API (API version 1). |
| `src/` | Development runtime: runs a plugin as a native process connected to a development device. POSIX only. |
| `schema/plugin_ui_registry.json` | UI schema v1. Source of truth for the UI compiler and the device validator. |
| `schema/plugin.schema.json` | Manifest schema (draft). |
| `tools/plugin_ui_compile.py` | UI XML compiler. |
| `cmd/aitbase-plugin/`, `internal/` | The `aitbase-plugin` CLI (Go, standard library only). |
| `template/` | The project `init` creates. `template/.agents/skills/` is the only copy of the plugin skills; `init` also writes them to `.claude/skills/`. |
| `sdkfiles.go` | Embeds `template/` and the files vendored into a project's `sdk/`. |
| `docs/` | Customer-facing reference: [CLI](docs/cli.md), [UI XML](docs/ui-xml.md), [protocol](docs/plugin-protocol.md), [package format](docs/package-format.md). |
| `llms.txt` | Index of the docs for agents. |

## Rules

- **Never document or template something that doesn't work.** Planned features are labeled planned. Agents follow these docs literally, so an invented function or widget becomes a broken plugin.
- **Keep generated and mirrored facts in sync:**
  - the UI schema comes from the device registry; don't hand-edit it to differ;
  - every diagnostic code must be in `docs/cli.md` (`TestCodesDocumented` enforces it);
  - `Version` in `sdkfiles.go` must match `project(... VERSION)` in `CMakeLists.txt` (`TestVersionMatchesCMake`);
  - when the API, UI schema or capabilities change, update `template/AGENTS.md` and the skills in the same change.
- **Windows must keep working** for everything that doesn't need the POSIX runtime:
  - no symlinks in the repository;
  - no shell scripts on the customer path;
  - use `filepath` for disk paths and slash paths inside packages.
- **The CLI uses only the Go standard library,** so it ships as one binary.
- **Diagnostic codes are stable.** Never reuse a code for a different meaning.
- **Never ask for, store or accept the device password** in the CLI or docs. Device pairing will use an interactive prompt and a scoped token.

## Validate

```sh
go vet ./... && go test ./...                       # CLI, template, docs consistency
cmake -S . -B build && cmake --build build && ctest --test-dir build --output-on-failure
```

After changing `template/`, also run the CLI end to end in a scratch directory:

```sh
go run ./cmd/aitbase-plugin init /tmp/p --id test.p
cd /tmp/p
go run <sdk>/cmd/aitbase-plugin check --json
```

Then run `build --json` and `pack --json` the same way.

Record changes in `CHANGELOG.md`.
