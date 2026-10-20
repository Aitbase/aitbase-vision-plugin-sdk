package check

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
"entry":"counter","ui":{"screens":["main"]}}`
const goodSource = `AITBASE_PLUGIN_ID("alice.counter");`

func codes(r *Result) map[string]bool {
	m := map[string]bool{}
	for _, d := range r.Diagnostics {
		m[d.Code] = true
	}
	return m
}

// D32: requested_capabilities is no longer needed; an old list is a warning, not an error.
func TestRequestedCapabilitiesIgnored(t *testing.T) {
	m := `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui","settings","shell"]}`
	r := Project(writeProject(t, m, goodSource), "", Options{SkipUI: true})
	if r.HasErrors() || !codes(r)["CAPABILITIES_IGNORED"] {
		t.Fatalf("want only the CAPABILITIES_IGNORED warning: %+v", r.Diagnostics)
	}
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
		{"missing sound", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","sounds":["beep"],"requested_capabilities":["ui","audio.output"]}`, goodSource, "SOUND_MISSING"},
		{"bad sound name", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","sounds":["../x"],"requested_capabilities":["ui","audio.output"]}`, goodSource, "SOUND_NAME"},
		{"missing screen", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","ui":{"screens":["other"]},"requested_capabilities":["ui"]}`, goodSource, "SCREEN_MISSING"},
		{"source id", goodManifest, `AITBASE_PLUGIN_ID("alice.other");`, "SOURCE_ID_MISMATCH"},
		{"no source id", goodManifest, `int x;`, "SOURCE_ID_MISSING"},
		{"unknown trigger", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui"],"triggers":[{"event":"boot"}]}`, goodSource, "TRIGGER_UNKNOWN"},
		{"schedule too often", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui"],"triggers":[{"event":"schedule","every_min":5}]}`, goodSource, "TRIGGER_SCHEDULE"},
		{"schedule not whole", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui"],"triggers":[{"event":"schedule","every_min":15.5}]}`, goodSource, "TRIGGER_SCHEDULE"},
		{"schedule missing minutes", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui"],"triggers":[{"event":"schedule"}]}`, goodSource, "TRIGGER_SCHEDULE"},
		{"two schedules", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui"],"triggers":[{"event":"schedule","every_min":15},{"event":"schedule","every_min":30}]}`, goodSource, "TRIGGER_SCHEDULE"},
		{"idle face not listed", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","ui":{"screens":["main"]},"idle_face":"face","requested_capabilities":["ui"]}`, goodSource, "IDLE_FACE_SCREEN"},
		{"idle face without screens", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","idle_face":"main","requested_capabilities":["idle.face"]}`, goodSource, "IDLE_FACE_SCREEN"},
		{"too many triggers", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui"],"triggers":[{"event":"schedule","every_min":15},{},{},{},{}]}`, goodSource, "TRIGGER_FORMAT"},
		{"tools not a list", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui","assistant.tools"],"assistant_tools":{}}`, goodSource, "TOOLS_FORMAT"},
		{"too many tools", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui","assistant.tools"],"assistant_tools":[{"name":"a","description":"x"},{"name":"b","description":"x"},{"name":"c","description":"x"},{"name":"d","description":"x"},{"name":"e","description":"x"}]}`, goodSource, "TOOLS_FORMAT"},
		{"tool extra field", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui","assistant.tools"],"assistant_tools":[{"name":"a","description":"x","shell":"ls"}]}`, goodSource, "TOOLS_FORMAT"},
		{"tool name", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui","assistant.tools"],"assistant_tools":[{"name":"Today","description":"x"}]}`, goodSource, "TOOL_NAME"},
		{"tool twice", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui","assistant.tools"],"assistant_tools":[{"name":"a","description":"x"},{"name":"a","description":"y"}]}`, goodSource, "TOOL_NAME"},
		{"tool description", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui","assistant.tools"],"assistant_tools":[{"name":"a","description":"two\nlines"}]}`, goodSource, "TOOL_DESCRIPTION"},
		{"tool bidi description", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui","assistant.tools"],"assistant_tools":[{"name":"a","description":"a\u202eb"}]}`, goodSource, "TOOL_DESCRIPTION"},
		{"tool arg type", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui","assistant.tools"],"assistant_tools":[{"name":"a","description":"x","args":{"q":{"type":"object"}}}]}`, goodSource, "TOOL_ARG"},
		{"tool string max", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui","assistant.tools"],"assistant_tools":[{"name":"a","description":"x","args":{"q":{"type":"string","max":121}}}]}`, goodSource, "TOOL_ARG"},
		{"tool int range", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui","assistant.tools"],"assistant_tools":[{"name":"a","description":"x","args":{"q":{"type":"int","min":5,"max":1}}}]}`, goodSource, "TOOL_ARG"},
		{"tool enum empty", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui","assistant.tools"],"assistant_tools":[{"name":"a","description":"x","args":{"q":{"type":"enum","values":[]}}}]}`, goodSource, "TOOL_ARG"},
		{"tool arg name", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui","assistant.tools"],"assistant_tools":[{"name":"a","description":"x","args":{"Q":{"type":"string"}}}]}`, goodSource, "TOOL_ARG"},
		{"tool too many args", `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui","assistant.tools"],"assistant_tools":[{"name":"a","description":"x","args":{"a":{"type":"string"},"b":{"type":"string"},"c":{"type":"string"},"d":{"type":"string"},"e":{"type":"string"}}}]}`, goodSource, "TOOL_ARG"},
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

func TestScheduleTrigger(t *testing.T) {
	m := `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui"],"triggers":[{"event":"schedule","every_min":60}]}`
	r := Project(writeProject(t, m, goodSource), "", Options{SkipUI: true})
	if r.HasErrors() || len(r.Manifest.Triggers) != 1 || *r.Manifest.Triggers[0].EveryMin != 60 {
		t.Fatalf("a valid schedule: %+v", r.Diagnostics)
	}
	for _, d := range r.Diagnostics {
		if d.Code == "MANIFEST_UNKNOWN_FIELD" {
			t.Fatal("triggers is a known field")
		}
	}
}

func TestAssistantTools(t *testing.T) {
	m := `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","assistant_tools":[{"name":"today","description":"Today's weather for a city","args":{"city":{"type":"string","max":40},
"days":{"type":"int","min":1,"max":7},"unit":{"type":"enum","values":["c","f"]}}},{"name":"now","description":"The time"}]}`
	r := Project(writeProject(t, m, goodSource), "", Options{SkipUI: true})
	if len(r.Diagnostics) != 0 {
		t.Fatalf("a valid assistant_tools block: %+v", r.Diagnostics)
	}
}

// D38: settings the owner fills in on the Device Console.
func TestSettings(t *testing.T) {
	head := `{"id":"alice.counter","name":"H","version":"1.0.0","api_version":1,"entry":"c","settings":`
	good := head + `[{"key":"ha_url","label":"Address","type":"url","placeholder":"http://192.168.1.10:8123"},{"key":"ha_token","label":"Token","type":"secret"}]}`
	if r := Project(writeProject(t, good, goodSource), "", Options{SkipUI: true}); len(r.Diagnostics) != 0 {
		t.Fatalf("a valid settings block: %+v", r.Diagnostics)
	}
	nine := "["
	for i := 0; i < 9; i++ {
		if i > 0 {
			nine += ","
		}
		nine += `{"key":"k` + strconv.Itoa(i) + `","label":"L","type":"text"}`
	}
	nine += "]"
	cases := map[string]string{
		`{}`: "SETTINGS_FORMAT",
		nine: "SETTINGS_FORMAT",
		`[{"key":"a","label":"L","type":"text","default":"x"}]`:                        "SETTINGS_FORMAT",
		`[{"key":"Bad","label":"L","type":"text"}]`:                                    "SETTING_FIELD",
		`[{"key":"a","label":"L","type":"text"},{"key":"a","label":"M","type":"url"}]`: "SETTING_FIELD",
		`[{"key":"a","label":"","type":"text"}]`:                                       "SETTING_FIELD",
		`[{"key":"a","label":"L","type":"password"}]`:                                  "SETTING_FIELD",
		`[{"key":"a","label":"` + strings.Repeat("x", 49) + `","type":"text"}]`:        "SETTING_FIELD",
	}
	for block, want := range cases {
		r := Project(writeProject(t, head+block+"}", goodSource), "", Options{SkipUI: true})
		found := false
		for _, d := range r.Diagnostics {
			found = found || d.Code == want
		}
		if !found {
			t.Errorf("%s: want %s, got %+v", block, want, r.Diagnostics)
		}
	}
}

func TestWeatherExample(t *testing.T) {
	r := Project(filepath.Join("..", "..", "examples", "weather"), "", Options{SkipUI: true})
	if len(r.Diagnostics) != 0 {
		t.Fatalf("examples/weather: %+v", r.Diagnostics)
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

func wav(rate uint32, channels, bits uint16, samples int) []byte {
	data := uint32(samples * int(bits) / 8 * int(channels))
	b := []byte("RIFF\x00\x00\x00\x00WAVEfmt ")
	le32 := func(v uint32) { b = append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24)) }
	le16 := func(v uint16) { b = append(b, byte(v), byte(v>>8)) }
	le32(16)
	le16(1)
	le16(channels)
	le32(rate)
	le32(rate * uint32(channels) * uint32(bits) / 8)
	le16(channels * bits / 8)
	le16(bits)
	b = append(b, "data"...)
	le32(data)
	return append(b, make([]byte, data)...)
}

func TestWavProblem(t *testing.T) {
	if p := WavProblem(wav(16000, 1, 16, 1600)); p != "" {
		t.Errorf("a valid sound was refused: %s", p)
	}
	for name, data := range map[string][]byte{
		"8 kHz":    wav(8000, 1, 16, 1600),
		"stereo":   wav(16000, 2, 16, 1600),
		"8-bit":    wav(16000, 1, 8, 1600),
		"too long": wav(16000, 1, 16, 16000*11),
		"not wav":  []byte("ID3\x04 an mp3"),
		"cut":      wav(16000, 1, 16, 1600)[:60],
	} {
		if WavProblem(data) == "" {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestIdleFace(t *testing.T) {
	m := `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","ui":{"screens":["main"]},"idle_face":"main","requested_capabilities":["ui","idle.face"]}`
	r := Project(writeProject(t, m, goodSource), "", Options{SkipUI: true})
	if r.HasErrors() || r.Manifest.IdleFace != "main" {
		t.Fatalf("a listed idle face with idle.face: %+v", r.Diagnostics)
	}
	for _, d := range r.Diagnostics {
		if d.Code == "MANIFEST_UNKNOWN_FIELD" {
			t.Fatal("idle_face is a known field")
		}
	}
}

func TestFaceProblem(t *testing.T) {
	doc := func(children string, nodes int) []byte {
		return []byte(`{"format":"aitbase.plugin-ui","version":1,"screen":"face","tree":{"id":"root","type":"container","props":{},"children":[` +
			children + `]},"assets":{},"limits":{"nodes":` + strconv.Itoa(nodes) + `,"max_depth":6}}`)
	}
	label := `{"id":"t","type":"label","props":{"text":"-"},"children":[],"binding":{"property":"text","key":"face.1","type":"string"}}`
	if p := FaceProblem(doc(label, 2)); p != "" {
		t.Fatalf("a display-only face: %s", p)
	}
	button := `{"id":"go","type":"button","props":{},"children":[],"action":{"event":"activate","id":"plugin.increment"}}`
	title := `{"id":"t","type":"label","props":{"text":"-"},"children":[],"binding":{"property":"text","key":"title","type":"string"}}`
	big := append(doc(label, 2), bytes.Repeat([]byte(" "), 12300)...)
	// D36: a chart binds several properties; every one must be a face key.
	chart := `{"id":"h","type":"chart","props":{"series_count":2},"children":[],"bindings":[{"property":"series_1","key":"face.2","type":"string"},{"property":"series_2","key":"value.1","type":"string"}]}`
	gauge := `{"id":"g","type":"bar","props":{},"children":[],"binding":{"property":"value","key":"face.3","type":"string"}}`
	if p := FaceProblem(doc(gauge, 2)); p != "" {
		t.Fatalf("a gauge bound to a face key: %s", p)
	}
	for name, compiled := range map[string][]byte{
		"action":         doc(button, 2),
		"title key":      doc(title, 2),
		"nodes":          doc(label, 41),
		"size":           big,
		"chart bindings": doc(chart, 2),
		// D36 19c: an input widget, even unbound.
		"input widget": doc(`{"id":"s","type":"switch","props":{},"children":[]}`, 2),
	} {
		if FaceProblem(compiled) == "" {
			t.Errorf("%s: want a problem", name)
		}
	}
}

func TestScheduleIsCommunity(t *testing.T) {
	m := `{"id":"alice.counter","name":"C","version":"1.0.0","api_version":1,"entry":"c","requested_capabilities":["ui","schedule"]}`
	r := Project(writeProject(t, m, goodSource), "", Options{SkipUI: true})
	if r.HasErrors() {
		t.Fatalf("schedule is open to community plugins with the owner's approval (D26): %+v", r.Diagnostics)
	}
}
