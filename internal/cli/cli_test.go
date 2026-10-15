package cli

import (
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
	// pack.go reports codes inside error strings.
	src, _ := os.ReadFile("../pack/pack.go")
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
