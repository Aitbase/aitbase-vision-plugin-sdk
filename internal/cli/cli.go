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
	"sort"
	"strings"

	aitbasesdk "github.com/Aitbase/aitbase-vision-plugin-sdk"
	"github.com/Aitbase/aitbase-vision-plugin-sdk/internal/check"
	"github.com/Aitbase/aitbase-vision-plugin-sdk/internal/pack"
	"github.com/Aitbase/aitbase-vision-plugin-sdk/internal/project"
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
		{"build", "build with CMake: build [dir]", runBuild},
		{"pack", "write dist/<id>-<version>.avp: pack [dir]", runPack},
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
	sdk = fs.String("sdk", "", "SDK directory with the UI compiler (default: <dir>/sdk)")
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
	sdkDir := *sdk
	if sdkDir == "" {
		sdkDir = filepath.Join(dir, "sdk")
	}
	r := check.Project(dir, sdkDir, check.Options{AllowReservedID: *allowReserved, Python: *python, SkipUI: *skipUI})
	ok := !r.HasErrors()
	human := "check passed"
	if !ok {
		human = "check failed"
	}
	return e.finish(*asJSON, "check", ok, r.Diagnostics, nil, human)
}

func runBuild(e env, args []string) int {
	fs, asJSON := newFlags(e, "build")
	buildDir := fs.String("build-dir", "build", "CMake build directory, relative to the project")
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
	out := filepath.Join(dir, *buildDir)
	// Build output goes to stderr so that --json keeps stdout to one object.
	for _, step := range [][]string{{"-S", dir, "-B", out}, {"--build", out}} {
		cmd := exec.Command(cmake, step...)
		cmd.Stdout, cmd.Stderr = e.stderr, e.stderr
		if err := cmd.Run(); err != nil {
			return e.finish(*asJSON, "build", false, []check.Diagnostic{{Severity: check.Error, Code: "BUILD_FAILED",
				Message: "cmake " + strings.Join(step, " ") + " failed: " + err.Error(), Fix: "read the compiler output above"}}, nil, "")
		}
	}
	return e.finish(*asJSON, "build", true, nil, result{"build_dir": out}, "build finished in "+out)
}

func runPack(e env, args []string) int {
	fs, asJSON := newFlags(e, "pack")
	buildDir := fs.String("build-dir", "build", "build directory holding the program and <screen>.ui.json")
	outDir := fs.String("out", "dist", "output directory, relative to the project")
	sdk, python, allowReserved, _ := checkFlags(fs)
	pos, err := parse(fs, args)
	if err != nil {
		return exitUsage
	}
	dir, err := oneDir(pos)
	if err != nil {
		return usageError(e, *asJSON, "pack", err.Error())
	}
	sdkDir := *sdk
	if sdkDir == "" {
		sdkDir = filepath.Join(dir, "sdk")
	}
	r := check.Project(dir, sdkDir, check.Options{AllowReservedID: *allowReserved, Python: *python, SkipUI: true})
	if r.HasErrors() {
		return e.finish(*asJSON, "pack", false, r.Diagnostics, nil, "pack failed: fix the check errors first")
	}
	m := r.Manifest
	build := filepath.Join(dir, *buildDir)
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
	out := filepath.Join(dir, *outDir, pack.FileName(m.ID, m.Version))
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err == nil {
		err = os.WriteFile(out, data, 0o644)
	}
	if err != nil {
		return e.finish(*asJSON, "pack", false, append(diags, check.Diagnostic{Severity: check.Error, Code: "WRITE_FAILED", Path: out, Message: err.Error()}), nil, "")
	}
	return e.finish(*asJSON, "pack", true, diags, result{"package": out, "sha256": digest, "bytes": len(data)},
		fmt.Sprintf("wrote %s (%d bytes, sha256 %s)", out, len(data), digest))
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
