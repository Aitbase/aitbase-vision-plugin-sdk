# Plugin package (`.avp`), format 1

A package is one file named `<id>-<version>.avp`, for example `alice.weather-1.2.0.avp`. It is what people share and copy into `aitbase/plugins/` on a device's SD card. `aitbase-plugin pack --wasm` writes it.

## Contents

An `.avp` is a zip archive holding exactly these files, at its top level:

| Entry | Contents |
|---|---|
| `plugin.json` | The manifest ([schema](../schema/plugin.schema.json)). |
| `<entry>.wasm` | The WebAssembly module, at most 4 MiB. |
| `<screen>.ui.json` | One compiled screen per entry in `ui.screens`, at most 16 KiB each. |
| `sounds/<name>.wav` | One sound per entry in `sounds`: 16 kHz, mono, 16-bit PCM, at most 10 s. |

Every entry is **stored** (not compressed), with its sizes in its local header, no extra field and no data descriptor. The device refuses a package with compressed entries, any other file, a listed file missing, or the same name twice.

The device never unpacks a package: it reads each file in place from the SD card, so a large package uses no more of the device's memory than a small one.

## Signature

The zip comment holds the signature and must end the file:

```text
AVP-SIG-1 <base64 public key> <base64 signature>
```

- **Key:** an ECDSA P-256 public key, DER SubjectPublicKeyInfo.
- **Signature:** ECDSA with SHA-256 (DER), over every byte of the file before the end-of-central-directory record's comment length field.
- **Author:** the SHA-256 of the public key. The Device Console shows its first 16 hex digits.

The device checks the signature by reading the file in small pieces and records each file's SHA-256. Whenever it reads a file later (the module to start the plugin, a sound to play it), it checks that hash, so a file changed after the check is refused.

A signature identifies who packed a plugin and shows it hasn't changed since. It doesn't make a plugin safe: every plugin runs in the same sandbox, with only the capabilities it requests.

## Authors

- **First key wins.** A device remembers the first author key it sees for a plugin id. A package with the same id signed by another key doesn't run: it would otherwise get the plugin's saved data. The owner can **forget** the plugin in the Device Console to allow a new author; that deletes the plugin's saved data.
- **Keep your key.** `pack --wasm` creates it on first use; `aitbase-plugin key` shows where. Back it up, and use the same key on every computer you build on.
- `aitbase.*` ids are reserved for official plugins, which ship in the device firmware; packages that use them are refused.

## Rules

The same plugin must produce the same contents on every OS:
- **Ordering:** entries are sorted by name.
- **Paths:** forward-slash and relative; only `sounds/` as a folder.
- **Timestamps:** fixed to 1980-01-01 00:00:00.
- **Mode:** `0644`.

The signed bytes' SHA-256, which `pack` prints, depends only on the contents. The signature itself differs on each pack.

File names must survive Windows, macOS, Linux and FAT/exFAT SD cards: lowercase ASCII letters, digits, `_`, and `.` in extensions; no names that differ only by letter case.

A package is at most **4 MB**.

## On the device

- The device runs the newest package in `aitbase/plugins/` by itself, with the capabilities it requests. There is no switch: to stop a plugin, remove its file.
- Packages that can't run are listed on the Device Console's Plugins page with the reason: not signed, damaged, another author, a missing file.
- Unzipped plugin folders, from SDK 0.5 and earlier, no longer run. Pack again with `pack --wasm`.
