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
	"crypto/ecdsa"
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
	"github.com/Aitbase/aitbase-vision-plugin-sdk/internal/bundled"
	"github.com/Aitbase/aitbase-vision-plugin-sdk/internal/check"
	"github.com/Aitbase/aitbase-vision-plugin-sdk/internal/pack"
	"github.com/Aitbase/aitbase-vision-plugin-sdk/internal/project"
	"github.com/Aitbase/aitbase-vision-plugin-sdk/internal/signing"
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
		{"pack", "with --wasm, write the signed dist/<id>-<version>.avp for the SD card: pack [dir] --wasm", runPack},
		{"key", "show your package signing key (created on first pack): key", runKey},
		{"toolchain", "download the WebAssembly toolchain (wasi-sdk) and print its path", runToolchain},
		{"sdk", "print the path of the SDK a project builds with: sdk [dir]", runSDK},
		{"version", "print the SDK version", runVersion},
		{"devices", "find devices on the network (not available yet)", notAvailable("devices")},
		{"pair", "pair with a device once, with its Device Console password: pair <address>", runPair},
		{"dev", "build, pack, upload to the paired device and wait until it runs: dev [dir]", runDev},
		{"logs", "print the plugin's log from the device: logs [dir] [--follow]", runLogs},
		{"unpair", "revoke this computer's token on the device and forget it: unpair", runUnpair},
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

// localSDK finds the nearest sdk/ holding this SDK in the project directory or one of its parents.
// A project from `init` has its own sdk/; a plugin in a repository of plugins may use the
// repository's.
func localSDK(dir string) (string, bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for d := abs; ; d = filepath.Dir(d) {
		candidate := filepath.Join(d, "sdk")
		if _, err := os.Stat(filepath.Join(candidate, "include", "aitbase_plugin.h")); err == nil {
			if d == abs {
				return filepath.Join(dir, "sdk"), true
			}
			return candidate, true
		}
		if filepath.Dir(d) == d {
			return "", false
		}
	}
}

// sdkFlag resolves the SDK directory: --sdk as given, else the nearest sdk/ (localSDK), else the
// SDK built into the CLI, unpacked in the user cache folder on first use.
func sdkFlag(dir, value string) (string, *check.Diagnostic) {
	if value != "" {
		return value, nil
	}
	if local, ok := localSDK(dir); ok {
		return local, nil
	}
	root, err := bundled.Dir()
	if err != nil {
		return "", &check.Diagnostic{Severity: check.Error, Code: "SDK_UNAVAILABLE",
			Message: "cannot unpack the SDK built into the CLI: " + err.Error(),
			Fix:     "make the user cache folder writable, or pass --sdk <dir>"}
	}
	return root, nil
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
	sdkDir := ""
	if !*skipUI {
		var d *check.Diagnostic
		if sdkDir, d = sdkFlag(dir, *sdk); d != nil {
			return e.finish(*asJSON, "check", false, []check.Diagnostic{*d}, nil, "check failed")
		}
	}
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

func runSDK(e env, args []string) int {
	fs, asJSON := newFlags(e, "sdk")
	pos, err := parse(fs, args)
	if err != nil {
		return exitUsage
	}
	dir, err := oneDir(pos)
	if err != nil {
		return usageError(e, *asJSON, "sdk", err.Error())
	}
	_, local := localSDK(dir)
	root, d := sdkFlag(dir, "")
	if d != nil {
		return e.finish(*asJSON, "sdk", false, []check.Diagnostic{*d}, nil, "")
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return e.finish(*asJSON, "sdk", true, nil, result{"sdk_dir": root, "bundled": !local}, root)
}

// signingKey loads or creates the author's key; a diagnostic when it can't.
func signingKey() (*ecdsa.PrivateKey, string, bool, *check.Diagnostic) {
	path, err := signing.DefaultPath()
	if err == nil {
		key, created, err := signing.LoadOrCreate(path)
		if err == nil {
			return key, path, created, nil
		}
	}
	return nil, path, false, &check.Diagnostic{Severity: check.Error, Code: "SIGNING_KEY", Path: path,
		Message: "cannot read or create the signing key: " + err.Error(),
		Fix:     "restore your backup of the key file, or set " + signing.EnvKey + " to its path"}
}

func runKey(e env, args []string) int {
	fs, asJSON := newFlags(e, "key")
	if _, err := parse(fs, args); err != nil {
		return exitUsage
	}
	key, path, created, d := signingKey()
	if d != nil {
		return e.finish(*asJSON, "key", false, []check.Diagnostic{*d}, nil, "")
	}
	fp, _ := signing.Fingerprint(key)
	return e.finish(*asJSON, "key", true, nil, result{"path": path, "fingerprint": fp, "created": created},
		fmt.Sprintf("signing key %s\nauthor %s (the Device Console shows %q)\nBack this file up: a device refuses updates to your plugins signed with another key.",
			path, fp, signing.Short(fp)))
}

func runBuild(e env, args []string) int {
	fs, asJSON := newFlags(e, "build")
	buildDir := fs.String("build-dir", "", "CMake build directory (default: <dir>/build, or <dir>/build-wasm with --wasm)")
	wasm := fs.Bool("wasm", false, "build WebAssembly for the device; downloads wasi-sdk on first use")
	sdk := fs.String("sdk", "", "SDK directory (default: the nearest sdk/ in <dir> or a parent, else the SDK built into the CLI)")
	pos, err := parse(fs, args)
	if err != nil {
		return exitUsage
	}
	dir, err := oneDir(pos)
	if err != nil {
		return usageError(e, *asJSON, "build", err.Error())
	}
	extra, d := buildProject(e, dir, *buildDir, *sdk, *wasm)
	if d != nil {
		return e.finish(*asJSON, "build", false, []check.Diagnostic{*d}, nil, "")
	}
	return e.finish(*asJSON, "build", true, nil, extra, "build finished in "+extra["build_dir"].(string))
}

// buildProject configures and builds dir with CMake; build output goes to e.stderr, so that --json
// keeps stdout to one object.
func buildProject(e env, dir, buildDir, sdk string, wasm bool) (result, *check.Diagnostic) {
	cmake, err := exec.LookPath("cmake")
	if err != nil {
		return nil, &check.Diagnostic{Severity: check.Error, Code: "CMAKE_NOT_FOUND",
			Message: "cmake is not installed", Fix: "install CMake 3.13 or later and a C compiler"}
	}
	sdkDir, d := sdkFlag(dir, sdk)
	if d != nil {
		return nil, d
	}
	if abs, err := filepath.Abs(sdkDir); err == nil {
		sdkDir = abs
	}
	// AITBASE_SDK_DIR tells a plugin's CMakeLists.txt where the SDK is when it has no sdk/ of its
	// own; a project that doesn't read it must not warn about it.
	configure := []string{"-S", dir, "--no-warn-unused-cli", "-DAITBASE_SDK_DIR=" + filepath.ToSlash(sdkDir)}
	out := pathFlag(dir, buildDir, "build")
	extra := result{"sdk_dir": sdkDir}
	if wasm {
		out = pathFlag(dir, buildDir, "build-wasm")
		root, d := ensureWasiSDK(e)
		if d != nil {
			return nil, d
		}
		// Forward slashes: CMake reads backslashes in these values as escapes (C:\Users -> \U).
		configure = append(configure, "-DCMAKE_TOOLCHAIN_FILE="+filepath.ToSlash(wasisdk.ToolchainFile(root)),
			"-DWASI_SDK_PREFIX="+filepath.ToSlash(root), "-DCMAKE_BUILD_TYPE=MinSizeRel")
		// Visual Studio generators can't use the wasi-sdk toolchain; Ninja can, on every system.
		if runtime.GOOS == "windows" {
			if _, err := exec.LookPath("ninja"); err != nil {
				return nil, &check.Diagnostic{Severity: check.Error, Code: "NINJA_NOT_FOUND",
					Message: "building WebAssembly on Windows needs Ninja", Fix: "install Ninja (winget install Ninja-build.Ninja) and retry"}
			}
			configure = append(configure, "-G", "Ninja")
		}
		extra["wasi_sdk"] = root
	}
	configure = append(configure, "-B", out)
	for _, step := range [][]string{configure, {"--build", out}} {
		cmd := exec.Command(cmake, step...)
		cmd.Stdout, cmd.Stderr = e.stderr, e.stderr
		if err := cmd.Run(); err != nil {
			fix := "read the compiler output above"
			if wasm {
				fix += "; if the build directory was configured natively before, delete it or pass another --build-dir"
			}
			return nil, &check.Diagnostic{Severity: check.Error, Code: "BUILD_FAILED",
				Message: "cmake " + strings.Join(step, " ") + " failed: " + err.Error(), Fix: fix}
		}
	}
	extra["build_dir"] = out
	extra["wasm"] = wasm
	return extra, nil
}

func runPack(e env, args []string) int {
	fs, asJSON := newFlags(e, "pack")
	buildDir := fs.String("build-dir", "", "build directory holding the program and <screen>.ui.json (default: <dir>/build, or <dir>/build-wasm with --wasm)")
	outDir := fs.String("out", "", "output directory (default: <dir>/dist)")
	wasm := fs.Bool("wasm", false, "package the WebAssembly build as the signed <id>-<version>.avp for the device's SD card")
	_, python, allowReserved, _ := checkFlags(fs)
	pos, err := parse(fs, args)
	if err != nil {
		return exitUsage
	}
	dir, err := oneDir(pos)
	if err != nil {
		return usageError(e, *asJSON, "pack", err.Error())
	}
	// Screens were compiled by the build, so pack needs no SDK.
	r := check.Project(dir, "", check.Options{AllowReservedID: *allowReserved, Python: *python, SkipUI: true})
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
	for _, snd := range m.Sounds {
		need = append(need, struct{ src, name, fix string }{filepath.Join(dir, "sounds", snd+".wav"), "sounds/" + snd + ".wav", "add the sound, or remove it from sounds"})
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
	out := filepath.Join(pathFlag(dir, *outDir, "dist"), pack.NativeFileName(m.ID, m.Version))
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err == nil {
		err = os.WriteFile(out, data, 0o644)
	}
	if err != nil {
		return e.finish(*asJSON, "pack", false, append(diags, check.Diagnostic{Severity: check.Error, Code: "WRITE_FAILED", Path: out, Message: err.Error()}), nil, "")
	}
	return e.finish(*asJSON, "pack", true, diags, result{"package": out, "sha256": digest, "bytes": len(data)},
		fmt.Sprintf("wrote %s (%d bytes, sha256 %s)", out, len(data), digest))
}

// packWasm writes <out>/<id>-<version>.avp: plugin.json, <entry>.wasm, the compiled screens and the
// sounds, signed with the author's key (package format 1, docs/package-format.md). The module is
// stripped of custom sections and checked against the device's host ABI first.
func packWasm(e env, asJSON bool, dir, build, outDir string, r *check.Result) int {
	extra, text, d := writeWasmPackage(dir, build, outDir, r)
	if d != nil {
		return e.finish(asJSON, "pack", false, append(r.Diagnostics, *d), nil, "pack failed")
	}
	return e.finish(asJSON, "pack", true, r.Diagnostics, extra, text)
}

// writeWasmPackage writes the signed .avp and returns its JSON fields and the text for people.
func writeWasmPackage(dir, build, outDir string, r *check.Result) (result, string, *check.Diagnostic) {
	m := r.Manifest
	fail := func(d check.Diagnostic) (result, string, *check.Diagnostic) {
		return nil, "", &d
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
	files := []pack.File{{Name: m.Entry + ".wasm", Data: module}, {Name: "plugin.json", Data: manifest}}
	if m.UI != nil {
		for _, s := range m.UI.Screens {
			p := filepath.Join(build, s+".ui.json")
			data, err := os.ReadFile(p)
			if err != nil {
				return fail(check.Diagnostic{Severity: check.Error, Code: "BUILD_OUTPUT_MISSING", Path: filepath.ToSlash(p),
					Message: "not found", Fix: "run `aitbase-plugin build --wasm` first"})
			}
			files = append(files, pack.File{Name: s + ".ui.json", Data: data})
		}
	}
	for _, snd := range m.Sounds {
		p := filepath.Join(dir, "sounds", snd+".wav")
		data, err := os.ReadFile(p)
		if err != nil {
			return fail(check.Diagnostic{Severity: check.Error, Code: "SOUND_MISSING", Path: filepath.ToSlash(p),
				Message: "not found", Fix: "add the sound, or remove it from sounds"})
		}
		files = append(files, pack.File{Name: "sounds/" + snd + ".wav", Data: data})
	}
	key, keyPath, created, d := signingKey()
	if d != nil {
		return fail(*d)
	}
	data, digest, err := pack.BuildAVP(files, key)
	if err != nil {
		code, msg, _ := strings.Cut(err.Error(), ": ")
		return fail(check.Diagnostic{Severity: check.Error, Code: code, Message: msg})
	}
	out := filepath.Join(outDir, pack.FileName(m.ID, m.Version))
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err == nil {
		err = os.WriteFile(out, data, 0o644)
	}
	if err != nil {
		return fail(check.Diagnostic{Severity: check.Error, Code: "WRITE_FAILED", Path: out, Message: err.Error()})
	}
	fp, _ := signing.Fingerprint(key)
	text := fmt.Sprintf("wrote %s (%d bytes, sha256 %s)\nsigned by %s\nCopy it into aitbase/plugins/ on the device's SD card.",
		out, len(data), digest, signing.Short(fp))
	if created {
		text += fmt.Sprintf("\nCreated your signing key at %s. Back it up: devices refuse updates to your plugins signed with another key.", keyPath)
	}
	return result{"package": out, "sha256": digest, "bytes": len(data), "module_bytes": len(module), "sd_dir": pack.SDPluginsDir,
		"author": fp, "signing_key": keyPath, "key_created": created}, text, nil
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
