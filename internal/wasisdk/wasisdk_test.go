package wasisdk

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type entry struct {
	name, link string
	data       string
	dir        bool
}

func tarGz(t *testing.T, entries []entry) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o755}
		switch {
		case e.dir:
			h.Typeflag = tar.TypeDir
		case e.link != "":
			h.Typeflag, h.Linkname = tar.TypeSymlink, e.link
		default:
			h.Typeflag, h.Size = tar.TypeReg, int64(len(e.data))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			tw.Write([]byte(e.data))
		}
	}
	tw.Close()
	gz.Close()
	path := filepath.Join(t.TempDir(), "sdk.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractStripsTopDirAndKeepsLinksInside(t *testing.T) {
	archive := tarGz(t, []entry{
		{name: "top/", dir: true},
		{name: "top/bin/clang-23", data: "compiler"},
		{name: "top/bin/clang", link: "clang-23"},
		{name: "top/share/cmake/wasi-sdk-p1.cmake", data: "toolchain"},
	})
	dest := t.TempDir()
	if err := extract(archive, dest, "top"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dest, "bin", "clang")); err != nil || string(data) != "compiler" {
		t.Fatalf("link not usable: %q %v", data, err)
	}
	if !valid(dest) {
		t.Fatal("toolchain file not found after extraction")
	}
}

func TestExtractRefusesEscapes(t *testing.T) {
	for _, bad := range [][]entry{
		{{name: "top/../../evil", data: "x"}},
		{{name: "top/bin/evil", link: "../../../etc/passwd"}},
		{{name: "top/bin/evil", link: "/etc/passwd"}},
	} {
		if err := extract(tarGz(t, bad), t.TempDir(), "top"); err == nil {
			t.Errorf("%s was accepted", bad[0].name+bad[0].link)
		}
	}
}

func TestDownloadChecksTheDigest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) }))
	defer srv.Close()
	dir := t.TempDir()
	// sha256("payload")
	const good = "239f59ed55e737c77147cf55ad0c1b030b6d7ee748a7426952f9b852d5a935e5"
	path, err := download(srv.URL, dir, good)
	if err != nil {
		t.Fatalf("matching digest refused: %v", err)
	}
	os.Remove(path)
	if _, err := download(srv.URL, dir, strings.Repeat("0", 64)); err == nil || !strings.Contains(err.Error(), "expected") {
		t.Fatalf("wrong digest accepted: %v", err)
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Fatalf("a rejected download was left behind: %v", left)
	}
}

func TestOverrideMustBeAnInstallation(t *testing.T) {
	t.Setenv(EnvOverride, t.TempDir())
	if _, err := Ensure(&bytes.Buffer{}); err == nil {
		t.Fatal("an empty override directory was accepted")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "share", "cmake"), 0o755)
	os.WriteFile(ToolchainFile(dir), []byte("x"), 0o644)
	t.Setenv(EnvOverride, dir)
	if got, err := Ensure(&bytes.Buffer{}); err != nil || got != dir {
		t.Fatalf("override not used: %q %v", got, err)
	}
}

func TestEveryPlatformIsPinned(t *testing.T) {
	for platform, a := range assets {
		if len(a.sha256) != 64 || a.platform == "" {
			t.Errorf("%s: bad pin %+v", platform, a)
		}
	}
}
