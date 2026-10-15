package check

import (
	"os"
	"path/filepath"
	"testing"
)

func writeProject(t *testing.T, manifest, source string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"plugin.json":         manifest,
		"src/main.c":          source,
		"ui/screens/main.xml": "<screen/>",
	}
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const goodManifest = `{"id":"alice.counter","name":"Counter","version":"1.0.0","api_version":1,
"entry":"counter","ui":{"screens":["main"]},"requested_capabilities":["ui"]}`
const goodSource = `AITBASE_PLUGIN_ID("alice.counter");`

func codes(r *Result) map[string]bool {
	m := map[string]bool{}
	for _, d := range r.Diagnostics {
		m[d.Code] = true
	}
	return m
}

func TestValidProject(t *testing.T) {
	r := Project(writeProject(t, goodManifest, goodSource), "", Options{SkipUI: true})
	if len(r.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", r.Diagnostics)
	}
}

func TestManifestErrors(t *testing.T) {
	cases := []struct {
		name, manifest, source, want string
	}{
		{"missing", "", goodSource, "MANIFEST_JSON"},
		{"bad id", `{"id":"Counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui"]}`, goodSource, "ID_FORMAT"},
		{"reserved id", `{"id":"aitbase.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui"]}`, `AITBASE_PLUGIN_ID("aitbase.counter");`, "ID_RESERVED"},
		{"version", `{"id":"alice.counter","name":"C","version":"1.0","api_version":1,"entry":"c","requested_capabilities":["ui"]}`, goodSource, "VERSION_FORMAT"},
		{"api", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":2,"entry":"c","requested_capabilities":["ui"]}`, goodSource, "API_VERSION_UNSUPPORTED"},
		{"planned capability", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui","http"]}`, goodSource, "CAPABILITY_UNAVAILABLE"},
		{"unknown capability", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui","shell"]}`, goodSource, "CAPABILITY_UNKNOWN"},
		{"no ui capability", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","ui":{"screens":["main"]},"requested_capabilities":[]}`, goodSource, "CAPABILITY_UI_MISSING"},
		{"missing screen", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","ui":{"screens":["other"]},"requested_capabilities":["ui"]}`, goodSource, "SCREEN_MISSING"},
		{"source id", goodManifest, `AITBASE_PLUGIN_ID("alice.other");`, "SOURCE_ID_MISMATCH"},
		{"no source id", goodManifest, `int x;`, "SOURCE_ID_MISSING"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Project(writeProject(t, c.manifest, c.source), "", Options{SkipUI: true})
			if !codes(r)[c.want] || !r.HasErrors() {
				t.Fatalf("want error %s, got %+v", c.want, r.Diagnostics)
			}
		})
	}
}

func TestReservedIDAllowedForOfficial(t *testing.T) {
	m := `{"id":"aitbase.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui"]}`
	r := Project(writeProject(t, m, `AITBASE_PLUGIN_ID("aitbase.counter");`), "", Options{SkipUI: true, AllowReservedID: true})
	if r.HasErrors() {
		t.Fatalf("unexpected: %+v", r.Diagnostics)
	}
}

func TestPackagePath(t *testing.T) {
	cases := map[string]string{
		"ui/main.ui.json": "",
		"ui/a b.json":     "PATH_CHARS",
		"ui/x?.json":      "PATH_CHARS",
		"ui/../x":         "PATH_CHARS",
		"ui/Ünï.json":     "PATH_CHARS",
		"ui/x.":           "PATH_TRAILING",
		"ui/CON.json":     "PATH_RESERVED_NAME",
		"ui/lpt1":         "PATH_RESERVED_NAME",
		"ui/console.json": "",
		"bin/plugin":      "",
	}
	for p, want := range cases {
		if got, _ := CheckPackagePath(p); got != want {
			t.Errorf("%s: got %q, want %q", p, got, want)
		}
	}
}

func TestCaseCollision(t *testing.T) {
	dir := writeProject(t, goodManifest, goodSource)
	for _, n := range []string{"ui/assets/Logo.rgb565", "ui/assets/logo.rgb565"} {
		p := filepath.Join(dir, filepath.FromSlash(n))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Skip("filesystem is case-insensitive")
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "ui", "assets")); len(entries) < 2 {
		t.Skip("filesystem is case-insensitive")
	}
	if r := Project(dir, "", Options{SkipUI: true}); !codes(r)["PATH_CASE_COLLISION"] {
		t.Fatalf("want PATH_CASE_COLLISION, got %+v", r.Diagnostics)
	}
}
