// Package wasisdk finds or downloads the wasi-sdk toolchain that builds plugins as WebAssembly.
//
// The version and each platform's SHA-256 are pinned here. A download goes to the user cache
// directory (aitbase-plugin/wasi-sdk-<version>-<platform>), is checked against the pinned digest
// before it is unpacked, and is reused by every later build. AITBASE_WASI_SDK names an existing
// installation instead.
package wasisdk

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Version is the pinned wasi-sdk release.
const Version = "34.0"

// EnvOverride names an existing wasi-sdk installation to use instead of downloading one.
const EnvOverride = "AITBASE_WASI_SDK"

type asset struct{ platform, sha256 string }

// From the wasi-sdk-34 GitHub release (asset digests).
var assets = map[string]asset{
	"linux/amd64":   {"x86_64-linux", "b761e3a0721dbae9c09a0059e5fdb2bf917d1b4a8a7b430fb3b5aafb0984b2c4"},
	"linux/arm64":   {"arm64-linux", "f7e243dff54d60bcc576e94d6166b69f410f2500ae4a9ceef34315be10e77971"},
	"darwin/amd64":  {"x86_64-macos", "87d27fa8adc68dee59bfbf2e22a6d34ef717c34d6bf1d8af2a56fc929d9ce0eb"},
	"darwin/arm64":  {"arm64-macos", "9c59398106b417f8f14913380fdf0097a8cc0ff4af9eb3ce0065a859e88d49e9"},
	"windows/amd64": {"x86_64-windows", "cccb5c323a9b34f0349a9b09e8804a0a7632c68c3310f4b5f437ed57d7e71d8f"},
	"windows/arm64": {"arm64-windows", "45e1c71f3e965621e7b98ebe1d37b0e4b1f77f3e8072113ffb4534e67b1a4b7c"},
}

// ErrUnsupported means no wasi-sdk is published for this OS and CPU.
var ErrUnsupported = errors.New("no wasi-sdk release for this system")

// ToolchainFile is the CMake toolchain file inside a wasi-sdk installation.
func ToolchainFile(root string) string {
	return filepath.Join(root, "share", "cmake", "wasi-sdk-p1.cmake")
}

func valid(root string) bool {
	_, err := os.Stat(ToolchainFile(root))
	return err == nil
}

func name() (string, asset, error) {
	a, ok := assets[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return "", asset{}, fmt.Errorf("%w (%s/%s)", ErrUnsupported, runtime.GOOS, runtime.GOARCH)
	}
	return "wasi-sdk-" + Version + "-" + a.platform, a, nil
}

// Find returns an installation without downloading: AITBASE_WASI_SDK, or a previous download.
func Find() (string, bool) {
	if dir := os.Getenv(EnvOverride); dir != "" {
		return dir, valid(dir)
	}
	cache, err := cacheDir()
	if err != nil {
		return "", false
	}
	n, _, err := name()
	if err != nil {
		return "", false
	}
	root := filepath.Join(cache, n)
	return root, valid(root)
}

func cacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "aitbase-plugin"), nil
}

// Ensure returns a wasi-sdk installation, downloading the pinned release on first use. Progress
// goes to log.
func Ensure(log io.Writer) (string, error) {
	if dir := os.Getenv(EnvOverride); dir != "" {
		if !valid(dir) {
			return "", fmt.Errorf("%s=%s is not a wasi-sdk installation (no %s)", EnvOverride, dir, ToolchainFile(dir))
		}
		return dir, nil
	}
	if root, ok := Find(); ok {
		return root, nil
	}
	n, a, err := name()
	if err != nil {
		return "", err
	}
	cache, err := cacheDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return "", err
	}
	url := fmt.Sprintf("https://github.com/WebAssembly/wasi-sdk/releases/download/wasi-sdk-%s/%s.tar.gz",
		strings.Split(Version, ".")[0], n)
	fmt.Fprintf(log, "Downloading wasi-sdk %s for %s (about 200 MB, once)...\n", Version, a.platform)
	archive, err := download(url, cache, a.sha256)
	if err != nil {
		return "", err
	}
	defer os.Remove(archive)
	staging, err := os.MkdirTemp(cache, n+".partial-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	if err := extract(archive, staging, n); err != nil {
		return "", fmt.Errorf("unpacking %s: %w", n, err)
	}
	root := filepath.Join(cache, n)
	if err := os.Rename(staging, root); err != nil {
		if valid(root) { // another build finished first
			return root, nil
		}
		return "", err
	}
	if !valid(root) {
		return "", fmt.Errorf("%s has no %s", root, ToolchainFile(root))
	}
	fmt.Fprintf(log, "wasi-sdk %s is in %s\n", Version, root)
	return root, nil
}

// download fetches url into dir and checks its SHA-256 before returning the file's path.
func download(url, dir, want string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading %s: %s", url, resp.Status)
	}
	f, err := os.CreateTemp(dir, "download-*.tar.gz")
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("downloading %s: %w", url, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		os.Remove(f.Name())
		return "", fmt.Errorf("%s has SHA-256 %s, expected %s; not used", url, got, want)
	}
	return f.Name(), nil
}

// extract unpacks a .tar.gz whose entries all live under top/ into dest, without top/.
// Entries that would leave dest are refused. A symbolic link that the system can't create
// (Windows without privilege) is replaced by a copy of its target.
func extract(archive, dest, top string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	type link struct{ path, target string }
	var pending []link
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		rel, ok := strings.CutPrefix(strings.TrimPrefix(hdr.Name, "./"), top+"/")
		if !ok || rel == "" {
			continue
		}
		path, err := inside(dest, rel)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if hdr.Mode&0o111 != 0 {
				mode = 0o755
			}
			out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, tr)
			if cerr := out.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			target := filepath.Join(filepath.Dir(path), filepath.FromSlash(hdr.Linkname))
			if filepath.IsAbs(hdr.Linkname) {
				return fmt.Errorf("absolute link %s", hdr.Name)
			}
			if _, err := inside(dest, mustRel(dest, target)); err != nil {
				return fmt.Errorf("link %s points outside the toolchain", hdr.Name)
			}
			pending = append(pending, link{path, hdr.Linkname})
		}
	}
	for _, l := range pending {
		if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
			return err
		}
		if os.Symlink(l.target, l.path) == nil {
			continue
		}
		if err := copyFile(filepath.Join(filepath.Dir(l.path), filepath.FromSlash(l.target)), l.path); err != nil {
			return fmt.Errorf("link %s: %w", l.path, err)
		}
	}
	return nil
}

func mustRel(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return ".."
	}
	return filepath.ToSlash(rel)
}

// inside joins dest and a slash path, refusing anything that escapes dest.
func inside(dest, rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe path %q in archive", rel)
	}
	return filepath.Join(dest, clean), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil || info.IsDir() {
		return fmt.Errorf("cannot copy %s", src)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}
