// Package aitbasesdk embeds the files the aitbase-plugin CLI writes into new plugin projects.
package aitbasesdk

import "embed"

// Version is the SDK release. It must match project(... VERSION) in CMakeLists.txt.
const Version = "0.11.0"

// Template is the plugin project that `aitbase-plugin init` creates.
//
//go:embed all:template
var Template embed.FS

// Runtime is the SDK copy vendored into each new project as sdk/, so a project builds without git
// or network access and stays pinned to the CLI's SDK version.
//
//go:embed CMakeLists.txt README.md CHANGELOG.md docs include src cmake schema third_party
//go:embed tools/plugin_ui_compile.py tools/plugin_ui_registry.py
var Runtime embed.FS

// CapabilityRegistry is the device's capability registry (D4), generated from the device app: the
// capabilities a plugin can request, the operations behind them and their limits.
//
//go:embed schema/plugin_capability_registry.json
var CapabilityRegistry []byte

// UIRegistry is the device's plugin UI registry: widgets, bindings and limits, the idle face
// bounds (D22) among them.
//
//go:embed schema/plugin_ui_registry.json
var UIRegistry []byte
