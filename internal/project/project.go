// Package project creates new plugin projects from the embedded template.
package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	aitbasesdk "github.com/Aitbase/aitbase-vision-plugin-sdk"
)

type Params struct {
	ID     string // e.g. "yourname.weather"
	Name   string // display name
	Target string // build target and manifest entry, e.g. "weather"
}

// DefaultTarget derives a build target name from the last part of an id.
func DefaultTarget(id string) string {
	last := id[strings.LastIndexByte(id, '.')+1:]
	return strings.ReplaceAll(last, "-", "_")
}

// DefaultName derives a display name from the last part of an id.
func DefaultName(id string) string {
	words := strings.FieldsFunc(id[strings.LastIndexByte(id, '.')+1:], func(r rune) bool { return r == '-' || r == '_' })
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// Create writes the template and a vendored SDK copy into dir, which must be empty or absent.
// It returns the slash-separated paths it wrote.
func Create(dir string, p Params) ([]string, error) {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("%s is not empty", dir)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	replacer := strings.NewReplacer("__PLUGIN_ID__", p.ID, "__PLUGIN_NAME__", p.Name, "__TARGET__", p.Target,
		"__SDK_VERSION__", aitbasesdk.Version)
	var written []string
	write := func(rel string, data []byte) error {
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		written = append(written, rel)
		return os.WriteFile(dst, data, 0o644)
	}

	err := fs.WalkDir(aitbasesdk.Template, "template", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := aitbasesdk.Template.ReadFile(p)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, "template/")
		if rel == "gitignore" { // stored without the dot so it doesn't apply to the SDK repo
			rel = ".gitignore"
		}
		data = []byte(replacer.Replace(string(data)))
		if err := write(rel, data); err != nil {
			return err
		}
		// One skill source, written for both agent conventions; no symlinks (they break on Windows).
		if strings.HasPrefix(rel, ".agents/skills/") {
			return write(".claude/skills/"+strings.TrimPrefix(rel, ".agents/skills/"), data)
		}
		return nil
	})
	if err != nil {
		return written, err
	}

	err = fs.WalkDir(aitbasesdk.Runtime, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := aitbasesdk.Runtime.ReadFile(p)
		if err != nil {
			return err
		}
		return write(path.Join("sdk", p), data)
	})
	if err != nil {
		return written, err
	}
	return written, write("sdk/VERSION", []byte(aitbasesdk.Version+"\n"))
}
