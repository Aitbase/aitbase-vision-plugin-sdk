// Package aitbasesdk embeds the files the aitbase-plugin CLI writes into new plugin projects.
package aitbasesdk

import "embed"

// Version is the SDK release. It must match project(... VERSION) in CMakeLists.txt.
const Version = "0.4.0"

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
