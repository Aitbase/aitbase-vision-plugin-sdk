// Package bundled gives access to the SDK copy built into the CLI: `init` vendors it into a new
// project, and a plugin without an sdk/ of its own builds with it unpacked in the user cache
// directory (aitbase-plugin/sdk-<version>-<digest>), so no git submodule or download is needed.
package bundled

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"

	aitbasesdk "github.com/Aitbase/aitbase-vision-plugin-sdk"
)

// Files calls fn for every file of the bundled SDK, with slash-separated paths, and last for
// VERSION, which holds the SDK version.
func Files(fn func(rel string, data []byte) error) error {
	err := fs.WalkDir(aitbasesdk.Runtime, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := aitbasesdk.Runtime.ReadFile(p)
		if err != nil {
			return err
		}
		return fn(p, data)
	})
	if err != nil {
		return err
	}
	return fn("VERSION", []byte(aitbasesdk.Version+"\n"))
}

// digest identifies the bundled files, so that two CLI builds with the same version but different
// SDK files (development builds) never share a directory.
func digest() (string, error) {
	h := sha256.New()
	err := Files(func(rel string, data []byte) error {
		sum := sha256.Sum256(data)
		h.Write([]byte(rel + "\x00"))
		h.Write(sum[:])
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:12], err
}

func valid(root string) bool {
	_, err := os.Stat(filepath.Join(root, "VERSION"))
	return err == nil
}

// Dir returns the directory holding the bundled SDK, unpacking it on first use.
func Dir() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	sum, err := digest()
	if err != nil {
		return "", err
	}
	parent := filepath.Join(cache, "aitbase-plugin")
	name := "sdk-" + aitbasesdk.Version + "-" + sum
	root := filepath.Join(parent, name)
	if valid(root) {
		return root, nil
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp(parent, name+".partial-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	err = Files(func(rel string, data []byte) error {
		dst := filepath.Join(staging, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		return "", err
	}
	if err := os.Rename(staging, root); err != nil {
		if valid(root) { // another command finished first
			return root, nil
		}
		return "", err
	}
	return root, nil
}
