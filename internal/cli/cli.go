// Package cli implements the aitbase-plugin commands.
//
// Every command accepts --json. With it, the command prints exactly one JSON object on stdout:
//
//	{"ok": bool, "command": "...", "sdk_version": "...", "diagnostics": [...], ...}
//
// so that coding agents can act on the result. Exit codes: 0 success, 1 failure, 2 usage error,
// 3 not available in this SDK or device yet.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	aitbasesdk "github.com/Aitbase/aitbase-vision-plugin-sdk"
	"github.com/Aitbase/aitbase-vision-plugin-sdk/internal/check"
	"github.com/Aitbase/aitbase-vision-plugin-sdk/internal/pack"
	"github.com/Aitbase/aitbase-vision-plugin-sdk/internal/project"
	"github.com/Aitbase/aitbase-vision-plugin-sdk/internal/wasisdk"
)

const (
	exitOK           = 0
	exitFailed       = 1
	exitUsage        = 2
	exitNotAvailable = 3
)

type env struct {
	stdout, stderr io.Writer
}

type command struct {
	name    string
	summary string
	run     func(e env, args []string) int
}

func commands() []command {
	return []command{
		{"init", "create a plugin project: init <dir> --id <id>", runInit},
		{"check", "validate the plugin project: check [dir]", runCheck},
		{"build", "build with CMake: build [dir] [--wasm]", runBuild},
		{"pack", "write dist/<id>-<version>.avp, or with --wasm the SD card zip: pack [dir] [--wasm]", runPack},
		{"toolchain", "download the WebAssembly toolchain (wasi-sdk) and print its path", runToolchain},
		{"version", "print the SDK version", runVersion},
		{"devices", "find devices on the network (not available yet)", notAvailable("devices")},
		{"pair", "pair with a device (not available yet)", notAvailable("pair")},
		{"dev", "upload and reload on a device (not available yet)", notAvailable("dev")},
		{"logs", "stream plugin logs (not available yet)", notAvailable("logs")},
		{"screenshot", "capture the plugin screen (not available yet)", notAvailable("screenshot")},
		{"press", "press a device button (not available yet)", notAvailable("press")},
		{"pack-sd", "copy the package to an SD card (not available yet)", notAvailable("pack-sd")},
	}
}

// Main runs the CLI and returns the process exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	e := env{stdout, stderr}
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		usage(stderr)
		if len(args) == 0 {
			return exitUsage
		}
		return exitOK
	}
	for _, c := range commands() {
		if c.name == args[0] {
			return c.run(e, args[1:])
		}
	}
	fmt.Fprintf(stderr, "aitbase-plugin: unknown command %q\n\n", args[0])
	usage(stderr)
	return exitUsage
}

func usage(w io.Writer) {
	fmt.Fprintf(w, "aitbase-plugin %s: build AITBASE VISION plugins\n\nUsage: aitbase-plugin <command> [arguments] [--json]\n\nCommands:\n", aitbasesdk.Version)
	for _, c := range commands() {
		fmt.Fprintf(w, "  %-11s %s\n", c.name, c.summary)
	}
	fmt.Fprintln(w, "\nRun `aitbase-plugin <command> -h` for a command's options.")
}

// parse accepts flags before, between or after positional arguments, since agents and people
// write both `check --json .` and `check . --json`.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			positional = append(positional, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if strings.Contains(name, "=") {
			continue
		}
		if f := fs.Lookup(name); f != nil {
			if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
				continue
			}
			if i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		}
	}
	if err := fs.Parse(flags); err != nil {
		return nil, err
	}
	return positional, nil
}

func newFlags(e env, name string) (*flag.FlagSet, *bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	asJSON := fs.Bool("json", false, "print one JSON result object on stdout")
	return fs, asJSON
}

type result map[string]any

func (e env) finish(asJSON bool, cmd string, ok bool, diags []check.Diagnostic, extra result, human string) int {
	if diags == nil {
		diags = []check.Diagnostic{}
	}
	if asJSON {
		out := result{"ok": ok, "command": cmd, "sdk_version": aitbasesdk.Version, "diagnostics": diags}
		for k, v := range extra {
			out[k] = v
		}
		enc := json.NewEncoder(e.stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
	} else {
		printDiagnostics(e.stderr, diags)
		if human != "" {
			fmt.Fprintln(e.stdout, human)
		}
	}
	if ok {
		return exitOK
	}
	return exitFailed
}

func printDiagnostics(w io.Writer, diags []check.Diagnostic) {
	for _, d := range diags {
		where := d.Path
		if where != "" {
			where = " " + where
		}
		fmt.Fprintf(w, "%s %s%s: %s\n", d.Severity, d.Code, where, d.Message)
		if d.Fix != "" {
			fmt.Fprintf(w, "    fix: %s\n", d.Fix)
		}
	}
}

func usageError(e env, asJSON bool, cmd, msg string) int {
	if asJSON {
		e.finish(true, cmd, false, []check.Diagnostic{{Severity: check.Error, Code: "USAGE", Message: msg}}, nil, "")
	} else {
		fmt.Fprintf(e.stderr, "aitbase-plugin %s: %s\n", cmd, msg)
	}
	return exitUsage
}

// pathFlag resolves a path option. A value the user gave is used as written, relative to the
// current directory like any command-line path; an empty value means <dir>/<def>.
func pathFlag(dir, value, def string) string {
	if value != "" {
		return value
	}
	return filepath.Join(dir, def)
}

// sdkFlag resolves the SDK directory: --sdk as given, else the nearest sdk/ holding this SDK in
// the project directory or one of its parents. A project from `init` has its own sdk/; a plugin
// in a repository of plugins uses the repository's.
func sdkFlag(dir, value string) string {
	if value != "" {
		return value
	}
	abs, err := filepath.Abs(dir)
	if err == nil {
		for d := abs; ; d = filepath.Dir(d) {
			candidate := filepath.Join(d, "sdk")
			if _, err := os.Stat(filepath.Join(candidate, "include", "aitbase_plugin.h")); err == nil {
				if d == abs {
					return filepath.Join(dir, "sdk")
				}
				return candidate
			}
			if filepath.Dir(d) == d {
				break
			}
		}
	}
	return filepath.Join(dir, "sdk")
}

func oneDir(positional []string) (string, error) {
	switch len(positional) {
	case 0:
		return ".", nil
	case 1:
		return positional[0], nil
	}
	return "", errors.New("expected at most one project directory")
}

func runVersion(e env, args []string) int {
	fs, asJSON := newFlags(e, "version")
	if _, err := parse(fs, args); err != nil {
		return exitUsage
	}
	return e.finish(*asJSON, "version", true, nil, result{"api_version": check.SupportedAPIVersion}, aitbasesdk.Version)
}

func runInit(e env, args []string) int {
	fs, asJSON := newFlags(e, "init")
	id := fs.String("id", "", "plugin id, e.g. yourname.weather (required)")
	name := fs.String("name", "", "display name (default: from the id)")
	pos, err := parse(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(pos) != 1 || *id == "" {
		return usageError(e, *asJSON, "init", "usage: init <dir> --id <id> [--name <name>]")
	}
	p := project.Params{ID: *id, Name: *name}
	// Validate the id with the same rules as check, before writing anything.
	if d := check.IDProblem(*id, false); d != nil {
		return e.finish(*asJSON, "init", false, []check.Diagnostic{*d}, nil, "")
	}
	p.Target = project.DefaultTarget(*id)
	if p.Name == "" {
		p.Name = project.DefaultName(*id)
	}
	written, err := project.Create(pos[0], p)
	if err != nil {
		return e.finish(*asJSON, "init", false, []check.Diagnostic{{Severity: check.Error, Code: "INIT_FAILED", Path: pos[0], Message: err.Error()}}, nil, "")
	}
	sort.Strings(written)
	human := fmt.Sprintf("Created %s (%s).\nNext: cd %s, read AGENTS.md, then run `aitbase-plugin check`.", pos[0], *id, pos[0])
	return e.finish(*asJSON, "init", true, nil, result{"dir": pos[0], "id": *id, "files": written}, human)
}

func checkFlags(fs *flag.FlagSet) (sdk, python *string, allowReserved, skipUI *bool) {
	sdk = fs.String("sdk", "", "SDK directory with the UI compiler (default: the nearest sdk/ in <dir> or a parent)")
	python = fs.String("python", "", "Python 3 interpreter for the UI compiler (default: search PATH)")
	allowReserved = fs.Bool("allow-reserved-id", false, "allow the aitbase.* id namespace (official plugins only)")
	skipUI = fs.Bool("skip-ui", false, "do not compile screens")
	return
}

func runCheck(e env, args []string) int {
	fs, asJSON := newFlags(e, "check")
	sdk, python, allowReserved, skipUI := checkFlags(fs)
	pos, err := parse(fs, args)
	if err != nil {
		return exitUsage
	}
	dir, err := oneDir(pos)
	if err != nil {
		return usageError(e, *asJSON, "check", err.Error())
	}
	sdkDir := sdkFlag(dir, *sdk)
	r := check.Project(dir, sdkDir, check.Options{AllowReservedID: *allowReserved, Python: *python, SkipUI: *skipUI})
	ok := !r.HasErrors()
	human := "check passed"
	if !ok {
		human = "check failed"
	}
	return e.finish(*asJSON, "check", ok, r.Diagnostics, nil, human)
}

// ensureWasiSDK finds or downloads the pinned wasi-sdk; download progress goes to stderr.
func ensureWasiSDK(e env) (string, *check.Diagnostic) {
	root, err := wasisdk.Ensure(e.stderr)
	if err != nil {
		return "", &check.Diagnostic{Severity: check.Error, Code: "WASI_SDK_UNAVAILABLE", Message: err.Error(),
			Fix: "check the network connection and retry, or set " + wasisdk.EnvOverride + " to a wasi-sdk " + wasisdk.Version + " installation"}
	}
	return root, nil
}

func runToolchain(e env, args []string) int {
	fs, asJSON := newFlags(e, "toolchain")
	if _, err := parse(fs, args); err != nil {
		return exitUsage
	}
	root, d := ensureWasiSDK(e)
	if d != nil {
		return e.finish(*asJSON, "toolchain", false, []check.Diagnostic{*d}, nil, "")
	}
	return e.finish(*asJSON, "toolchain", true, nil,
		result{"wasi_sdk": root, "wasi_sdk_version": wasisdk.Version, "toolchain_file": wasisdk.ToolchainFile(root)}, root)
}

func runBuild(e env, args []string) int {
	fs, asJSON := newFlags(e, "build")
	buildDir := fs.String("build-dir", "", "CMake build directory (default: <dir>/build, or <dir>/build-wasm with --wasm)")
	wasm := fs.Bool("wasm", false, "build WebAssembly for the device; downloads wasi-sdk on first use")
	pos, err := parse(fs, args)
	if err != nil {
		return exitUsage
	}
	dir, err := oneDir(pos)
	if err != nil {
		return usageError(e, *asJSON, "build", err.Error())
	}
	cmake, err := exec.LookPath("cmake")
	if err != nil {
		return e.finish(*asJSON, "build", false, []check.Diagnostic{{Severity: check.Error, Code: "CMAKE_NOT_FOUND",
			Message: "cmake is not installed", Fix: "install CMake 3.13 or later and a C compiler"}}, nil, "")
	}
	configure := []string{"-S", dir}
	out := pathFlag(dir, *buildDir, "build")
	extra := result{}
	if *wasm {
		out = pathFlag(dir, *buildDir, "build-wasm")
		root, d := ensureWasiSDK(e)
		if d != nil {
			return e.finish(*asJSON, "build", false, []check.Diagnostic{*d}, nil, "")
		}
		// Forward slashes: CMake reads backslashes in these values as escapes (C:\Users -> \U).
		configure = append(configure, "-DCMAKE_TOOLCHAIN_FILE="+filepath.ToSlash(wasisdk.ToolchainFile(root)),
			"-DWASI_SDK_PREFIX="+filepath.ToSlash(root), "-DCMAKE_BUILD_TYPE=MinSizeRel")
		// Visual Studio generators can't use the wasi-sdk toolchain; Ninja can, on every system.
		if runtime.GOOS == "windows" {
			if _, err := exec.LookPath("ninja"); err != nil {
				return e.finish(*asJSON, "build", false, []check.Diagnostic{{Severity: check.Error, Code: "NINJA_NOT_FOUND",
					Message: "building WebAssembly on Windows needs Ninja", Fix: "install Ninja (winget install Ninja-build.Ninja) and retry"}}, nil, "")
			}
			configure = append(configure, "-G", "Ninja")
		}
		extra["wasi_sdk"] = root
	}
	configure = append(configure, "-B", out)
	// Build output goes to stderr so that --json keeps stdout to one object.
	for _, step := range [][]string{configure, {"--build", out}} {
		cmd := exec.Command(cmake, step...)
		cmd.Stdout, cmd.Stderr = e.stderr, e.stderr
		if err := cmd.Run(); err != nil {
			fix := "read the compiler output above"
			if *wasm {
				fix += "; if the build directory was configured natively before, delete it or pass another --build-dir"
			}
			return e.finish(*asJSON, "build", false, []check.Diagnostic{{Severity: check.Error, Code: "BUILD_FAILED",
				Message: "cmake " + strings.Join(step, " ") + " failed: " + err.Error(), Fix: fix}}, nil, "")
		}
	}
	extra["build_dir"] = out
	extra["wasm"] = *wasm
	return e.finish(*asJSON, "build", true, nil, extra, "build finished in "+out)
}

func runPack(e env, args []string) int {
	fs, asJSON := newFlags(e, "pack")
	buildDir := fs.String("build-dir", "", "build directory holding the program and <screen>.ui.json (default: <dir>/build, or <dir>/build-wasm with --wasm)")
	outDir := fs.String("out", "", "output directory (default: <dir>/dist)")
	wasm := fs.Bool("wasm", false, "package the WebAssembly build as <id>-<version>-wasm.zip for the device's SD card")
	sdk, python, allowReserved, _ := checkFlags(fs)
	pos, err := parse(fs, args)
	if err != nil {
		return exitUsage
	}
	dir, err := oneDir(pos)
	if err != nil {
		return usageError(e, *asJSON, "pack", err.Error())
	}
	sdkDir := sdkFlag(dir, *sdk)
	r := check.Project(dir, sdkDir, check.Options{AllowReservedID: *allowReserved, Python: *python, SkipUI: true})
	if r.HasErrors() {
		return e.finish(*asJSON, "pack", false, r.Diagnostics, nil, "pack failed: fix the check errors first")
	}
	m := r.Manifest
	if *wasm {
		return packWasm(e, *asJSON, dir, pathFlag(dir, *buildDir, "build-wasm"), pathFlag(dir, *outDir, "dist"), r)
	}
	build := pathFlag(dir, *buildDir, "build")
	manifest, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return e.finish(*asJSON, "pack", false, []check.Diagnostic{{Severity: check.Error, Code: "MANIFEST_MISSING", Message: err.Error()}}, nil, "")
	}
	files := []pack.File{{Name: "plugin.json", Data: manifest}}
	need := []struct{ src, name, fix string }{
		{filepath.Join(build, m.Entry), "bin/" + m.Entry, "run `aitbase-plugin build` first"},
	}
	if m.UI != nil {
		for _, s := range m.UI.Screens {
			need = append(need, struct{ src, name, fix string }{filepath.Join(build, s+".ui.json"), "ui/" + s + ".ui.json", "run `aitbase-plugin build` first"})
		}
	}
	diags := r.Diagnostics
	for _, n := range need {
		data, err := os.ReadFile(n.src)
		if err != nil {
			diags = append(diags, check.Diagnostic{Severity: check.Error, Code: "BUILD_OUTPUT_MISSING", Path: filepath.ToSlash(n.src), Message: "not found", Fix: n.fix})
			continue
		}
		files = append(files, pack.File{Name: n.name, Data: data})
	}
	if len(diags) > len(r.Diagnostics) {
		return e.finish(*asJSON, "pack", false, diags, nil, "pack failed")
	}
	data, digest, err := pack.Build(files)
	if err != nil {
		code, msg, _ := strings.Cut(err.Error(), ": ")
		return e.finish(*asJSON, "pack", false, append(diags, check.Diagnostic{Severity: check.Error, Code: code, Message: msg}), nil, "")
	}
	out := filepath.Join(pathFlag(dir, *outDir, "dist"), pack.FileName(m.ID, m.Version))
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err == nil {
		err = os.WriteFile(out, data, 0o644)
	}
	if err != nil {
		return e.finish(*asJSON, "pack", false, append(diags, check.Diagnostic{Severity: check.Error, Code: "WRITE_FAILED", Path: out, Message: err.Error()}), nil, "")
	}
	return e.finish(*asJSON, "pack", true, diags, result{"package": out, "sha256": digest, "bytes": len(data)},
		fmt.Sprintf("wrote %s (%d bytes, sha256 %s)", out, len(data), digest))
}

// packWasm writes <out>/<id>-<version>-wasm.zip: aitbase/plugins/<id>/ with <entry>.wasm, the
// compiled screens and plugin.json, which unzips at the root of the device's SD card. The module
// is stripped of custom sections and checked against the device's host ABI first.
func packWasm(e env, asJSON bool, dir, build, outDir string, r *check.Result) int {
	m := r.Manifest
	diags := r.Diagnostics
	fail := func(d check.Diagnostic) int {
		return e.finish(asJSON, "pack", false, append(diags, d), nil, "pack failed")
	}
	modulePath := filepath.Join(build, m.Entry)
	module, err := os.ReadFile(modulePath)
	if err != nil {
		return fail(check.Diagnostic{Severity: check.Error, Code: "BUILD_OUTPUT_MISSING", Path: filepath.ToSlash(modulePath),
			Message: "not found", Fix: "run `aitbase-plugin build --wasm` first"})
	}
	if module, err = pack.StripCustomSections(module); err != nil {
		return fail(check.Diagnostic{Severity: check.Error, Code: "WASM_INVALID", Path: filepath.ToSlash(modulePath),
			Message: err.Error() + "; is this a native build?", Fix: "run `aitbase-plugin build --wasm` first"})
	}
	if err := pack.CheckModule(module); err != nil {
		code, msg, _ := strings.Cut(err.Error(), ": ")
		return fail(check.Diagnostic{Severity: check.Error, Code: code, Path: filepath.ToSlash(modulePath), Message: msg})
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return fail(check.Diagnostic{Severity: check.Error, Code: "MANIFEST_MISSING", Message: err.Error()})
	}
	prefix := pack.SDDir(m.ID) + "/"
	files := []pack.File{{Name: prefix + m.Entry + ".wasm", Data: module}, {Name: prefix + "plugin.json", Data: manifest}}
	if m.UI != nil {
		for _, s := range m.UI.Screens {
			p := filepath.Join(build, s+".ui.json")
			data, err := os.ReadFile(p)
			if err != nil {
				return fail(check.Diagnostic{Severity: check.Error, Code: "BUILD_OUTPUT_MISSING", Path: filepath.ToSlash(p),
					Message: "not found", Fix: "run `aitbase-plugin build --wasm` first"})
			}
			files = append(files, pack.File{Name: prefix + s + ".ui.json", Data: data})
		}
	}
	data, digest, err := pack.Build(files)
	if err != nil {
		code, msg, _ := strings.Cut(err.Error(), ": ")
		return fail(check.Diagnostic{Severity: check.Error, Code: code, Message: msg})
	}
	out := filepath.Join(outDir, pack.SDFileName(m.ID, m.Version))
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err == nil {
		err = os.WriteFile(out, data, 0o644)
	}
	if err != nil {
		return fail(check.Diagnostic{Severity: check.Error, Code: "WRITE_FAILED", Path: out, Message: err.Error()})
	}
	return e.finish(asJSON, "pack", true, diags,
		result{"package": out, "sha256": digest, "bytes": len(data), "module_bytes": len(module), "sd_dir": pack.SDDir(m.ID)},
		fmt.Sprintf("wrote %s (%d bytes, sha256 %s)\nUnzip it at the root of the device's SD card: it runs from %s/.", out, len(data), digest, pack.SDDir(m.ID)))
}

func notAvailable(cmd string) func(e env, args []string) int {
	return func(e env, args []string) int {
		asJSON := false
		for _, a := range args {
			asJSON = asJSON || a == "--json" || a == "-json"
		}
		msg := "`" + cmd + "` needs device support that no released firmware has yet"
		fix := "run the plugin on a development device as described in sdk/README.md"
		if asJSON {
			e.finish(true, cmd, false, []check.Diagnostic{{Severity: check.Error, Code: "NOT_AVAILABLE", Message: msg, Fix: fix}}, nil, "")
		} else {
			fmt.Fprintf(e.stderr, "aitbase-plugin %s: %s\n    fix: %s\n", cmd, msg, fix)
		}
		return exitNotAvailable
	}
}
