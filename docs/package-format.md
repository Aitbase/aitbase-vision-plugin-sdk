# Plugin package (`.avp`), draft format 0

> **Draft.** No device installs packages yet. This format records the rules every package must follow so that tools and devices can be built against them. It will change before 1.0, at least to add signatures and to replace the native program with `plugin.wasm`.

A package is one file named `<id>-<version>.avp`, for example `alice.weather-1.2.0.avp`. It is the unit people share and copy to a device's SD card at `<SD root>/aitbase/plugins/`.

## Contents

A `.avp` is a zip archive:

| Entry | Contents |
|---|---|
| `plugin.json` | The manifest, as in the project ([schema](../schema/plugin.schema.json)). |
| `ui/<screen>.ui.json` | One compiled screen per entry in `ui.screens`. |
| `bin/<entry>` | The program. Format 0 only: a native development build. WASM packages will carry `plugin.wasm` instead. |

## Rules

The same plugin must produce the same bytes on every OS, so its SHA-256 can identify it:
- **Ordering:** entries are sorted by name.
- **Paths:** forward-slash and relative.
- **Timestamps:** fixed to 1980-01-01 00:00:00.
- **Mode:** `0644`.
- **Compression:** Deflate at best compression.

File names must survive Windows, macOS, Linux and FAT/exFAT SD cards:
- only ASCII letters, digits, `.`, `-` and `_`;
- no names that differ only by letter case;
- no Windows reserved names (`con`, `aux`, `nul`, `com1`…);
- no trailing `.` or space;
- no symbolic links.

A package is at most **4 MB**, because the device loads a whole package into memory and checks its digest before running it.

## How a device treats packages (planned)

- Finding a package on the SD card never runs it. The owner enables it in the Device Console.
- The approval is tied to the package's id, digest, author key and requested capabilities. A changed file needs approval again.
- The device reads the package into memory, verifies its digest, and runs only those verified bytes.
