// Package pack writes .avp plugin packages.
//
// A package is a zip whose bytes depend only on its contents: entries are sorted, use
// forward-slash paths, a fixed timestamp and fixed permissions, so the same plugin produces the
// same digest on Windows, macOS and Linux (D12). The layout is draft package format 0; see
// docs/package-format.md.
package pack

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/Aitbase/aitbase-vision-plugin-sdk/internal/check"
)

// File is one package entry. Name is slash-separated and relative to the package root.
type File struct {
	Name string
	Data []byte
}

// MaxPackageBytes caps a package; the device loads whole packages into memory (D12).
const MaxPackageBytes = 4 << 20

var epoch = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// Build returns the package bytes and their SHA-256 hex digest.
func Build(files []File) ([]byte, string, error) {
	sorted := append([]File(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	seen := map[string]string{}
	for _, f := range sorted {
		if code, msg := check.CheckPackagePath(f.Name); code != "" {
			return nil, "", fmt.Errorf("%s: %s: %s", code, f.Name, msg)
		}
		key := strings.ToLower(f.Name)
		if other, ok := seen[key]; ok {
			return nil, "", fmt.Errorf("PATH_CASE_COLLISION: %s and %s differ only by letter case", other, f.Name)
		}
		seen[key] = f.Name
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(w, flate.BestCompression)
	})
	for _, f := range sorted {
		h := &zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: epoch}
		h.SetMode(0o644)
		w, err := zw.CreateHeader(h)
		if err != nil {
			return nil, "", err
		}
		if _, err := w.Write(f.Data); err != nil {
			return nil, "", err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, "", err
	}
	if buf.Len() > MaxPackageBytes {
		return nil, "", fmt.Errorf("PACKAGE_TOO_LARGE: %d bytes, the limit is %d", buf.Len(), MaxPackageBytes)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:]), nil
}

// FileName is the package name used in dist/ and on the SD card: <id>-<version>.avp.
func FileName(id, version string) string {
	return id + "-" + version + ".avp"
}
