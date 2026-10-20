// Package check validates a plugin project before it is built or packaged.
//
// Every finding has a stable Code so that people and coding agents can look it up in
// docs/cli.md and fix it. Codes are never reused for a different meaning.
package check

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	aitbasesdk "github.com/Aitbase/aitbase-vision-plugin-sdk"
)

type Severity string

const (
	Error   Severity = "error"
	Warning Severity = "warning"
)

type Diagnostic struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Path     string   `json:"path,omitempty"`
	Message  string   `json:"message"`
	Fix      string   `json:"fix,omitempty"`
}

// Manifest is the draft plugin.json. Fields are optional here so that a missing field is a
// diagnostic, not a decode failure.
type Manifest struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	APIVersion  *int      `json:"api_version"`
	Description string    `json:"description"`
	Entry       string    `json:"entry"`
	UI          *UI       `json:"ui"`
	Sounds      []string  `json:"sounds"`
	Triggers    []Trigger `json:"triggers"`
	IdleFace    string    `json:"idle_face"`
}

// Trigger starts the plugin without the user opening it (D21 in the device app). Devices support
// one kind: {"event": "schedule", "every_min": N}.
type Trigger struct {
	Event    string   `json:"event"`
	EveryMin *float64 `json:"every_min"`
}

// The schedule limits the device enforces; a package outside them is skipped.
const (
	ScheduleMinMinutes = 15
	ScheduleMaxMinutes = 1440
)

type UI struct {
	Screens []string `json:"screens"`
}

type Options struct {
	// AllowReservedID permits the aitbase.* namespace; only official plugins use it (D12).
	AllowReservedID bool
	// Python is the interpreter used to run the UI compiler. Empty means search PATH.
	Python string
	// SkipUI skips compiling screens.
	SkipUI bool
}

type Result struct {
	Manifest    *Manifest    `json:"-"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

func (r *Result) HasErrors() bool {
	for _, d := range r.Diagnostics {
		if d.Severity == Error {
			return true
		}
	}
	return false
}

func (r *Result) add(sev Severity, code, p, msg, fix string) {
	r.Diagnostics = append(r.Diagnostics, Diagnostic{sev, code, p, msg, fix})
}

const SupportedAPIVersion = 1

// Limits from the registry that check enforces before a package reaches a device.
var registryLimits = struct {
	SoundBytesMax      int `json:"audio_sound_bytes_max"`
	SampleRate         int `json:"audio_sample_rate"`
	ToolsMax           int `json:"assistant_tools_max"`
	ToolArgsMax        int `json:"assistant_tool_args_max"`
	ToolNameMax        int `json:"assistant_tool_name_max"`
	ToolDescriptionMax int `json:"assistant_tool_description_max"`
	ToolStringMax      int `json:"assistant_tool_string_max"`
	ToolEnumValuesMax  int `json:"assistant_tool_enum_values_max"`
	SettingsMax        int `json:"plugin_settings_max"`
	SettingKeyMax      int `json:"plugin_setting_key_max"`
	SettingLabelMax    int `json:"plugin_setting_label_max"`
}{}

// The registry's limits, read once.
func init() {
	var reg struct {
		Limits json.RawMessage `json:"limits"`
	}
	if err := json.Unmarshal(aitbasesdk.CapabilityRegistry, &reg); err != nil {
		panic("schema/plugin_capability_registry.json: " + err.Error())
	}
	if err := json.Unmarshal(reg.Limits, &registryLimits); err != nil {
		panic("schema/plugin_capability_registry.json limits: " + err.Error())
	}
}

var (
	idPattern      = regexp.MustCompile(`^[a-z][a-z0-9_-]*(\.[a-z][a-z0-9_-]*)+$`)
	versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$`)
	namePattern    = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	sourceIDRegexp = regexp.MustCompile(`AITBASE_PLUGIN_ID\(\s*"([^"]*)"\s*\)`)
	uiErrorPattern = regexp.MustCompile(`^(UI_[A-Z_]+) at (\S+): (.*)$`)
	knownFields    = map[string]bool{"id": true, "name": true, "version": true, "api_version": true,
		"description": true, "entry": true, "ui": true, "requested_capabilities": true, "sounds": true, "triggers": true,
		"idle_face": true, "assistant_tools": true, "settings": true}
)

// Project checks the plugin project rooted at dir. sdkDir is the SDK copy that holds the UI
// compiler (tools/plugin_ui_compile.py).
func Project(dir, sdkDir string, opt Options) *Result {
	r := &Result{}
	m := checkManifest(r, dir, opt)
	checkPaths(r, dir, "ui")
	if m != nil {
		r.Manifest = m
		checkSourceID(r, dir, m.ID)
		if !opt.SkipUI && m.UI != nil {
			compileScreens(r, dir, sdkDir, m.UI.Screens, m.IdleFace, opt.Python)
		}
	}
	return r
}

func checkManifest(r *Result, dir string, opt Options) *Manifest {
	const file = "plugin.json"
	data, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		r.add(Error, "MANIFEST_MISSING", file, "plugin.json not found", "run the command in the plugin project directory, or create one with `aitbase-plugin init`")
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		r.add(Error, "MANIFEST_JSON", file, "plugin.json is not a JSON object: "+err.Error(), "fix the JSON syntax")
		return nil
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		r.add(Error, "MANIFEST_FIELD", file, "a field has the wrong type: "+err.Error(), "compare with schema/plugin.schema.json")
		return nil
	}
	unknown := make([]string, 0)
	for k := range raw {
		if !knownFields[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	for _, k := range unknown {
		r.add(Warning, "MANIFEST_UNKNOWN_FIELD", file+"#"+k, "unknown field "+k+" is ignored", "remove it, or check the spelling against schema/plugin.schema.json")
	}

	if m.ID == "" {
		r.add(Error, "MANIFEST_FIELD", file+"#id", "id is required", `set "id" to a reverse-DNS name such as "yourname.weather"`)
	} else if d := IDProblem(m.ID, opt.AllowReservedID); d != nil {
		d.Path = file + "#id"
		r.Diagnostics = append(r.Diagnostics, *d)
	}
	if m.Name == "" || len(m.Name) > 40 {
		r.add(Error, "MANIFEST_FIELD", file+"#name", "name is required and must be at most 40 characters", `set "name" to a short display name`)
	}
	if !versionPattern.MatchString(m.Version) {
		r.add(Error, "VERSION_FORMAT", file+"#version", fmt.Sprintf("version %q is not MAJOR.MINOR.PATCH", m.Version), `use semantic versioning, e.g. "1.0.0"`)
	}
	if m.APIVersion == nil {
		r.add(Error, "MANIFEST_FIELD", file+"#api_version", "api_version is required", fmt.Sprintf(`set "api_version": %d`, SupportedAPIVersion))
	} else if *m.APIVersion != SupportedAPIVersion {
		r.add(Error, "API_VERSION_UNSUPPORTED", file+"#api_version", fmt.Sprintf("api_version %d is not supported by this SDK", *m.APIVersion), fmt.Sprintf(`set "api_version": %d`, SupportedAPIVersion))
	}
	if len(m.Description) > 200 {
		r.add(Error, "MANIFEST_FIELD", file+"#description", "description must be at most 200 characters", "shorten it")
	}
	if !namePattern.MatchString(m.Entry) {
		r.add(Error, "ENTRY_FORMAT", file+"#entry", fmt.Sprintf("entry %q is not valid", m.Entry), "use the build target name: lowercase letters, digits and '_'")
	}

	if m.UI != nil {
		seen := map[string]bool{}
		for _, s := range m.UI.Screens {
			p := file + "#ui.screens"
			switch {
			case !namePattern.MatchString(s) || len(s) > 32:
				r.add(Error, "SCREEN_NAME", p, fmt.Sprintf("screen name %q is not valid", s), "use lowercase letters, digits and '_', at most 32 characters")
			case seen[s]:
				r.add(Error, "SCREEN_NAME", p, fmt.Sprintf("screen %q is listed twice", s), "list each screen once")
			default:
				if _, err := os.Stat(filepath.Join(dir, "ui", "screens", s+".xml")); err != nil {
					r.add(Error, "SCREEN_MISSING", "ui/screens/"+s+".xml", fmt.Sprintf("screen %q has no file", s), "create ui/screens/"+s+".xml or remove it from ui.screens")
				}
			}
			seen[s] = true
		}
	}

	if m.IdleFace != "" {
		listed := false
		for _, s := range screensOf(&m) {
			listed = listed || s == m.IdleFace
		}
		if !listed {
			r.add(Error, "IDLE_FACE_SCREEN", file+"#idle_face", fmt.Sprintf("idle_face %q is not one of ui.screens", m.IdleFace),
				"name a screen listed in ui.screens, or add it there")
		}
	}

	if len(m.Triggers) > 4 {
		r.add(Error, "TRIGGER_FORMAT", file+"#triggers", "at most 4 triggers", "remove some")
	}
	schedules := 0
	for _, t := range m.Triggers {
		p := file + "#triggers"
		switch {
		case t.Event != "schedule":
			r.add(Error, "TRIGGER_UNKNOWN", p, fmt.Sprintf("unknown trigger %q; devices support \"schedule\"", t.Event),
				`use {"event": "schedule", "every_min": 60}`)
		case t.EveryMin == nil || *t.EveryMin != float64(int(*t.EveryMin)) ||
			*t.EveryMin < ScheduleMinMinutes || *t.EveryMin > ScheduleMaxMinutes:
			r.add(Error, "TRIGGER_SCHEDULE", p, "a schedule needs every_min, a whole number of minutes from 15 to 1440",
				`for example {"event": "schedule", "every_min": 60}`)
		default:
			if schedules++; schedules > 1 {
				r.add(Error, "TRIGGER_SCHEDULE", p, "only one schedule", "keep one schedule trigger")
			}
		}
	}

	seenSound := map[string]bool{}
	for _, s := range m.Sounds {
		p := file + "#sounds"
		switch {
		case !namePattern.MatchString(s) || len(s) > 32:
			r.add(Error, "SOUND_NAME", p, fmt.Sprintf("sound name %q is not valid", s), "use lowercase letters, digits and '_', at most 32 characters")
		case seenSound[s]:
			r.add(Error, "SOUND_NAME", p, fmt.Sprintf("sound %q is listed twice", s), "list each sound once")
		default:
			wav := "sounds/" + s + ".wav"
			data, err := os.ReadFile(filepath.Join(dir, "sounds", s+".wav"))
			if err != nil {
				r.add(Error, "SOUND_MISSING", wav, fmt.Sprintf("sound %q has no file", s), "add "+wav+" or remove it from sounds")
			} else if problem := WavProblem(data); problem != "" {
				r.add(Error, "SOUND_FORMAT", wav, problem, "convert it, e.g. `ffmpeg -i in.mp3 -ac 1 -ar 16000 -sample_fmt s16 "+wav+"`")
			}
		}
		seenSound[s] = true
	}

	// D32 (devices after 2026-10-20, SDK 0.11): every plugin may use every capability, so there is
	// nothing to request. An old list still works; it is only noise.
	if _, ok := raw["requested_capabilities"]; ok {
		r.add(Warning, "CAPABILITIES_IGNORED", file+"#requested_capabilities",
			"requested_capabilities is no longer needed: devices let every plugin use every capability",
			"remove requested_capabilities from plugin.json")
	}
	checkAssistantTools(r, file, raw["assistant_tools"])
	checkSettings(r, file, raw["settings"])
	return &m
}

// IDProblem reports why id can't be a plugin id, or nil.
func IDProblem(id string, allowReserved bool) *Diagnostic {
	switch {
	case len(id) > 64 || !idPattern.MatchString(id):
		return &Diagnostic{Error, "ID_FORMAT", "", fmt.Sprintf("id %q is not valid", id),
			"use at least two dot-separated parts of lowercase letters, digits, '-' or '_', at most 64 characters, e.g. \"yourname.weather\""}
	case strings.HasPrefix(id, "aitbase.") && !allowReserved:
		return &Diagnostic{Error, "ID_RESERVED", "", "the aitbase.* namespace is reserved for official plugins",
			"use your own prefix, e.g. \"yourname." + strings.TrimPrefix(id, "aitbase.") + "\""}
	}
	return nil
}

// Windows device names, which can't be created as files on Windows.
var reservedNames = map[string]bool{"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true}

// CheckPackagePath reports why rel (slash-separated) can't be stored in a package, or "".
// Packages must unpack identically on Windows, macOS and Linux (D12).
func CheckPackagePath(rel string) (code, msg string) {
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." {
			return "PATH_CHARS", "empty, '.' or '..' path component"
		}
		for _, ch := range part {
			ok := ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '-' || ch == '_'
			if !ok {
				return "PATH_CHARS", "use ASCII letters, digits, '.', '-' and '_' only"
			}
		}
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return "PATH_TRAILING", "a name ends with '.' or a space, which Windows drops"
		}
		base := strings.ToLower(part)
		if i := strings.IndexByte(base, '.'); i >= 0 {
			base = base[:i]
		}
		if reservedNames[base] {
			return "PATH_RESERVED_NAME", fmt.Sprintf("%q is a reserved name on Windows", part)
		}
	}
	return "", ""
}

// checkPaths walks dir/sub and reports names that would not survive packaging on every OS.
func checkPaths(r *Result, dir, sub string) {
	root := filepath.Join(dir, sub)
	if _, err := os.Stat(root); err != nil {
		return
	}
	lower := map[string]string{}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if d.Type()&os.ModeSymlink != 0 {
			r.add(Error, "FILE_SYMLINK", rel, "symbolic links are not allowed in a plugin", "replace the link with the file itself")
			return nil
		}
		if code, msg := CheckPackagePath(rel); code != "" {
			r.add(Error, code, rel, msg, "rename the file")
		}
		key := strings.ToLower(rel)
		if other, ok := lower[key]; ok {
			r.add(Error, "PATH_CASE_COLLISION", rel, "differs from "+other+" only by letter case", "rename one of them; Windows treats them as the same file")
		} else {
			lower[key] = rel
		}
		return nil
	})
}

// checkSourceID verifies that AITBASE_PLUGIN_ID in src/ matches the manifest.
func checkSourceID(r *Result, dir, id string) {
	var found []string
	_ = filepath.WalkDir(filepath.Join(dir, "src"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if ext := path.Ext(d.Name()); ext != ".c" && ext != ".h" {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		for _, m := range sourceIDRegexp.FindAllSubmatch(data, -1) {
			found = append(found, filepath.ToSlash(rel))
			if string(m[1]) != id {
				r.add(Error, "SOURCE_ID_MISMATCH", filepath.ToSlash(rel), fmt.Sprintf("AITBASE_PLUGIN_ID(%q) does not match id %q in plugin.json", m[1], id), "make them the same")
			}
		}
		return nil
	})
	switch {
	case len(found) == 0:
		r.add(Error, "SOURCE_ID_MISSING", "src", "no AITBASE_PLUGIN_ID(...) in src/", fmt.Sprintf("add AITBASE_PLUGIN_ID(%q); once, at file scope", id))
	case len(found) > 1:
		r.add(Error, "SOURCE_ID_DUPLICATE", found[1], "AITBASE_PLUGIN_ID is used more than once", "keep a single AITBASE_PLUGIN_ID in one source file")
	}
}

// FindPython returns a Python 3 interpreter on PATH, or "".
func FindPython() string {
	for _, name := range []string{"python3", "python", "py"} {
		if p, err := exec.LookPath(name); err == nil {
			out, err := exec.Command(p, "-c", "import sys; print(sys.version_info[0])").Output()
			if err == nil && strings.TrimSpace(string(out)) == "3" {
				return p
			}
		}
	}
	return ""
}

// compileScreens runs the SDK UI compiler on each screen and turns its UI_* errors into
// diagnostics. TODO(D12): port the compiler into the CLI so Python is not needed.
func compileScreens(r *Result, dir, sdkDir string, screens []string, face, python string) {
	if len(screens) == 0 {
		return
	}
	if python == "" {
		python = FindPython()
	}
	compiler := filepath.Join(sdkDir, "tools", "plugin_ui_compile.py")
	if python == "" {
		r.add(Warning, "UI_COMPILER_UNAVAILABLE", "ui", "Python 3 not found, so screens were not compiled", "install Python 3.8 or later, or pass --python")
		return
	}
	if _, err := os.Stat(compiler); err != nil {
		r.add(Warning, "UI_COMPILER_UNAVAILABLE", "ui", "UI compiler not found at "+compiler, "pass --sdk with the SDK directory, or keep the sdk/ folder that `init` created")
		return
	}
	tmp, err := os.MkdirTemp("", "aitbase-check-")
	if err != nil {
		r.add(Warning, "UI_COMPILER_UNAVAILABLE", "ui", "cannot create a temporary directory: "+err.Error(), "")
		return
	}
	defer os.RemoveAll(tmp)
	for _, s := range screens {
		if !namePattern.MatchString(s) {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "ui", "screens", s+".xml")); err != nil {
			continue // already reported as SCREEN_MISSING
		}
		var stderr bytes.Buffer
		out := filepath.Join(tmp, s+".ui.json")
		cmd := exec.Command(python, "-I", compiler, filepath.Join(dir, "ui"), s, out)
		cmd.Stderr = &stderr
		if err := cmd.Run(); err == nil {
			if s == face {
				data, err := os.ReadFile(out)
				if problem := FaceProblem(data); err == nil && problem != "" {
					r.add(Error, "IDLE_FACE_BOUNDS", "ui/screens/"+s+".xml", problem,
						"a face is display only: no buttons or input widgets; bind its widgets to face.1 to face.16")
				}
			}
			continue
		}
		msg := strings.TrimSpace(stderr.String())
		if m := uiErrorPattern.FindStringSubmatch(lastLine(msg)); m != nil {
			r.add(Error, m[1], "ui/"+m[2], m[3], "see docs/ui-xml.md for what is accepted")
		} else {
			r.add(Error, "UI_COMPILE", "ui/screens/"+s+".xml", msg, "see docs/ui-xml.md for what is accepted")
		}
	}
}

func screensOf(m *Manifest) []string {
	if m.UI == nil {
		return nil
	}
	return m.UI.Screens
}

// The idle face bounds (D22) from the UI registry.
var faceLimits = func() (l struct {
	Bytes int `json:"max_face_bytes"`
	Nodes int `json:"max_face_nodes"`
}) {
	var reg struct {
		Limits json.RawMessage `json:"limits"`
	}
	if err := json.Unmarshal(aitbasesdk.UIRegistry, &reg); err != nil || json.Unmarshal(reg.Limits, &l) != nil || l.Bytes == 0 {
		panic("schema/plugin_ui_registry.json: no idle face limits")
	}
	return l
}()

type uiBinding struct {
	Key string `json:"key"`
}

// The input widgets of the UI registry (D36): a face can't have them.
var inputWidgets = func() map[string]bool {
	var reg struct {
		Widgets map[string]struct {
			Input bool `json:"input"`
		} `json:"widgets"`
	}
	if err := json.Unmarshal(aitbasesdk.UIRegistry, &reg); err != nil {
		panic("schema/plugin_ui_registry.json: " + err.Error())
	}
	out := map[string]bool{}
	for name, w := range reg.Widgets {
		if w.Input {
			out[name] = true
		}
	}
	return out
}()

type uiNode struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Action  json.RawMessage `json:"action"`
	Binding *uiBinding      `json:"binding"`
	// Charts and tables bind several properties (D36).
	Bindings []uiBinding `json:"bindings"`
	Children []uiNode    `json:"children"`
}

// FaceProblem says why a compiled screen can't be an idle face (D22), or "" when it can: the
// device refuses a face over the size or node bounds, with an action (a face takes no input), or
// bound to anything but face.N.
func FaceProblem(compiled []byte) string {
	if len(compiled) > faceLimits.Bytes {
		return fmt.Sprintf("the face compiles to %d bytes; a face may have at most %d", len(compiled), faceLimits.Bytes)
	}
	var doc struct {
		Tree   uiNode `json:"tree"`
		Limits struct {
			Nodes int `json:"nodes"`
		} `json:"limits"`
	}
	if err := json.Unmarshal(compiled, &doc); err != nil {
		return "the compiled face is not valid JSON: " + err.Error()
	}
	if doc.Limits.Nodes > faceLimits.Nodes {
		return fmt.Sprintf("the face has %d widgets; a face may have at most %d", doc.Limits.Nodes, faceLimits.Nodes)
	}
	var walk func(n *uiNode) string
	walk = func(n *uiNode) string {
		if len(n.Action) > 0 && string(n.Action) != "null" {
			return fmt.Sprintf("%s has an action; a face takes no input", n.ID)
		}
		if inputWidgets[n.Type] {
			return fmt.Sprintf("%s is a %s, an input widget; a face takes no input", n.ID, n.Type)
		}
		bindings := n.Bindings
		if n.Binding != nil {
			bindings = append(bindings, *n.Binding)
		}
		for _, b := range bindings {
			if !strings.HasPrefix(b.Key, "face.") {
				return fmt.Sprintf("%s binds %q; a face binds only face.1 to face.16", n.ID, b.Key)
			}
		}
		for i := range n.Children {
			if p := walk(&n.Children[i]); p != "" {
				return p
			}
		}
		return ""
	}
	return walk(&doc.Tree)
}

func lastLine(s string) string {
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}
