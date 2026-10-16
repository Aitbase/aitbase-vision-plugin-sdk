package cli

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	aitbasesdk "github.com/Aitbase/aitbase-vision-plugin-sdk"
	"github.com/Aitbase/aitbase-vision-plugin-sdk/internal/check"
)

func run(t *testing.T, args ...string) (int, map[string]any) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Main(append(args, "--json"), &out, &errb)
	var res map[string]any
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("%v: stdout is not one JSON object: %v\n%s\nstderr: %s", args, err, out.String(), errb.String())
	}
	return code, res
}

// The template that init writes must pass check, including UI compilation when Python exists.
func TestInitThenCheck(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "p")
	if code, res := run(t, "init", dir, "--id", "alice.my-counter"); code != 0 {
		t.Fatalf("init: %d %v", code, res)
	}
	for _, f := range []string{"AGENTS.md", "CLAUDE.md", ".gitignore", "sdk/VERSION", "sdk/include/aitbase_plugin.h",
		"sdk/README.md", "sdk/docs/ui-xml.md", "sdk/docs/cli.md",
		".agents/skills/create-plugin/SKILL.md", ".claude/skills/create-plugin/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if strings.Contains(string(data), "__") {
		t.Errorf("unreplaced placeholder in plugin.json: %s", data)
	}
	if check.FindPython() == "" {
		t.Log("Python not found; UI compilation not covered")
	}
	if code, res := run(t, "check", dir); code != 0 || res["ok"] != true {
		t.Fatalf("check of a fresh project failed: %v", res)
	}
	if code, _ := run(t, "init", dir, "--id", "alice.again"); code == 0 {
		t.Fatal("init into a non-empty directory succeeded")
	}
}

// Explicit --build-dir and --out are used as given, e.g. a monorepo build tree outside the project.
func TestPackWithExternalBuildDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "plugins", "p")
	if code, res := run(t, "init", dir, "--id", "alice.p"); code != 0 {
		t.Fatalf("init: %v", res)
	}
	build := filepath.Join(root, "out", "plugins", "p")
	if err := os.MkdirAll(build, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"p", "main.ui.json"} {
		if err := os.WriteFile(filepath.Join(build, f), []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dist := filepath.Join(root, "dist")
	code, res := run(t, "pack", dir, "--build-dir", build, "--out", dist)
	if code != 0 {
		t.Fatalf("pack: %v", res)
	}
	if res["package"] != filepath.Join(dist, "alice.p-0.1.0.avp") {
		t.Fatalf("package written to %v", res["package"])
	}
}

func TestNotAvailableCommands(t *testing.T) {
	for _, c := range []string{"devices", "pair", "dev", "logs", "screenshot", "press", "pack-sd"} {
		code, res := run(t, c)
		if code != exitNotAvailable || res["ok"] != false {
			t.Errorf("%s: code %d %v", c, code, res)
		}
	}
}

func TestFlagsAfterPositional(t *testing.T) {
	var out, errb bytes.Buffer
	if code := Main([]string{"check", t.TempDir(), "--json", "--skip-ui"}, &out, &errb); code != exitFailed {
		t.Fatalf("code %d, stderr %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "MANIFEST_MISSING") {
		t.Fatalf("got %s", out.String())
	}
}

// Version must match project(... VERSION) in CMakeLists.txt.
func TestVersionMatchesCMake(t *testing.T) {
	data, err := os.ReadFile("../../CMakeLists.txt")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`project\(\S+ VERSION (\S+)`).FindSubmatch(data)
	if m == nil || string(m[1]) != aitbasesdk.Version {
		t.Fatalf("CMakeLists.txt version %q, Go version %q", m, aitbasesdk.Version)
	}
}

// Every diagnostic code used in the code must be documented in docs/cli.md, and the reverse.
func TestCodesDocumented(t *testing.T) {
	doc, err := os.ReadFile("../../docs/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	documented := map[string]bool{}
	for _, m := range regexp.MustCompile("(?m)^\\| `([A-Z_]+)` \\|").FindAllSubmatch(doc, -1) {
		documented[string(m[1])] = true
	}
	used := map[string]bool{}
	codeRe := regexp.MustCompile(`"([A-Z][A-Z_]{3,})"`)
	for _, f := range []string{"../check/check.go", "../pack/pack.go", "cli.go"} {
		src, _ := os.ReadFile(f)
		for _, m := range codeRe.FindAllSubmatch(src, -1) {
			used[string(m[1])] = true
		}
	}
	// pack.go and wasm.go report codes inside error strings.
	src, _ := os.ReadFile("../pack/pack.go")
	wasmSrc, _ := os.ReadFile("../pack/wasm.go")
	src = append(src, wasmSrc...)
	for _, m := range regexp.MustCompile(`"([A-Z_]{4,}):`).FindAllSubmatch(src, -1) {
		used[string(m[1])] = true
	}
	for c := range used {
		if !documented[c] {
			t.Errorf("code %s is not documented in docs/cli.md", c)
		}
	}
	for c := range documented {
		if !used[c] && !strings.HasPrefix(c, "UI_") {
			t.Errorf("docs/cli.md documents %s, which the code never reports", c)
		}
	}
}

// wasmModule is the smallest module pack --wasm accepts: the required exports, plus imports.
func wasmModule(imports ...string) []byte {
	str := func(s string) []byte { return append([]byte{byte(len(s))}, s...) }
	sec := func(id byte, body []byte) []byte { return append([]byte{id, byte(len(body))}, body...) }
	out := []byte{0, 'a', 's', 'm', 1, 0, 0, 0}
	if len(imports) > 0 {
		body := []byte{byte(len(imports))}
		for _, imp := range imports {
			m, f, _ := strings.Cut(imp, ".")
			body = append(append(append(body, str(m)...), str(f)...), 0, 0)
		}
		out = append(out, sec(2, body)...)
	}
	exports := []string{"memory", "aitbase_abi_version", "aitbase_rx_buffer", "aitbase_start", "aitbase_message", "aitbase_timer", "aitbase_stop"}
	body := []byte{byte(len(exports))}
	for i, e := range exports {
		body = append(append(body, str(e)...), 0, byte(i))
	}
	out = append(out, sec(7, body)...)
	return append(out, sec(0, append(str("producers"), "clang"...))...)
}

func TestPackWasm(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "p")
	if code, res := run(t, "init", dir, "--id", "alice.p"); code != 0 {
		t.Fatalf("init: %v", res)
	}
	build := filepath.Join(dir, "build-wasm")
	os.MkdirAll(build, 0o755)
	os.WriteFile(filepath.Join(build, "main.ui.json"), []byte(`{"screen":"main"}`), 0o644)

	os.WriteFile(filepath.Join(build, "p"), []byte("\x7fELF native"), 0o755)
	if code, res := run(t, "pack", dir, "--wasm", "--skip-ui"); code == 0 || !strings.Contains(fmtDiags(res), "WASM_INVALID") {
		t.Fatalf("a native program was packed: %v", res)
	}
	os.WriteFile(filepath.Join(build, "p"), wasmModule("aitbase.send", "wasi_snapshot_preview1.path_open"), 0o644)
	if code, res := run(t, "pack", dir, "--wasm"); code == 0 || !strings.Contains(fmtDiags(res), "WASM_IMPORT_REFUSED") {
		t.Fatalf("a module importing path_open was packed: %v", res)
	}
	os.WriteFile(filepath.Join(build, "p"), wasmModule("aitbase.send"), 0o644)
	code, res := run(t, "pack", dir, "--wasm")
	if code != 0 {
		t.Fatalf("pack --wasm: %v", res)
	}
	pkg, _ := res["package"].(string)
	if filepath.Base(pkg) != "alice.p-0.1.0-wasm.zip" || res["sd_dir"] != "aitbase/plugins/alice.p" {
		t.Fatalf("unexpected result: %v", res)
	}
	zr, err := zip.OpenReader(pkg)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	want := []string{"aitbase/plugins/alice.p/main.ui.json", "aitbase/plugins/alice.p/p.wasm", "aitbase/plugins/alice.p/plugin.json"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("zip holds %v, want %v", names, want)
	}
	if int(res["module_bytes"].(float64)) >= len(wasmModule("aitbase.send")) {
		t.Fatal("custom sections were not stripped")
	}
}

func fmtDiags(res map[string]any) string {
	data, _ := json.Marshal(res["diagnostics"])
	return string(data)
}
