package pack

import (
	"archive/zip"
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"sort"
	"strings"

	"github.com/Aitbase/aitbase-vision-plugin-sdk/internal/check"
	"github.com/Aitbase/aitbase-vision-plugin-sdk/internal/signing"
)

// SignaturePrefix starts the zip comment of a signed package.
const SignaturePrefix = "AVP-SIG-1 "

// BuildAVP writes a signed package (format 1): a deterministic zip of files, its comment
// "AVP-SIG-1 <base64 public key> <base64 signature>". The ECDSA-SHA256 signature covers every byte
// before the comment length field. Every entry is stored, with no extra fields or data
// descriptors, so the device reads each file in place on the SD card and never unpacks anything.
//
// It returns the package and the digest of the signed bytes, which depends only on the contents
// (the signature itself differs each time).
func BuildAVP(files []File, key *ecdsa.PrivateKey) ([]byte, string, error) {
	unsigned, err := buildStored(files)
	if err != nil {
		return nil, "", err
	}
	// The writer ends with an empty comment: a zero length field, nothing after it.
	n := len(unsigned)
	if n < 22 || binary.LittleEndian.Uint32(unsigned[n-22:]) != 0x06054b50 || unsigned[n-2] != 0 || unsigned[n-1] != 0 {
		return nil, "", fmt.Errorf("PACKAGE_INTERNAL: unexpected zip layout")
	}
	signed := unsigned[:n-2]
	sum := sha256.Sum256(signed)
	sig, err := ecdsa.SignASN1(rand.Reader, key, sum[:])
	if err != nil {
		return nil, "", err
	}
	pub, err := signing.PublicKeyDER(key)
	if err != nil {
		return nil, "", err
	}
	comment := SignaturePrefix + base64.StdEncoding.EncodeToString(pub) + " " + base64.StdEncoding.EncodeToString(sig)
	out := append(append([]byte(nil), signed...), byte(len(comment)), byte(len(comment)>>8))
	out = append(out, comment...)
	if len(out) > MaxPackageBytes {
		return nil, "", fmt.Errorf("PACKAGE_TOO_LARGE: %d bytes, the limit is %d", len(out), MaxPackageBytes)
	}
	return out, hex.EncodeToString(sum[:]), nil
}

// buildStored writes the deterministic zip of files with every entry stored: sizes and CRC in
// each local header, no extra fields, no data descriptors, a zero timestamp.
func buildStored(files []File) ([]byte, error) {
	sorted := append([]File(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	seen := map[string]string{}
	for _, f := range sorted {
		if code, msg := check.CheckPackagePath(f.Name); code != "" {
			return nil, fmt.Errorf("%s: %s: %s", code, f.Name, msg)
		}
		key := strings.ToLower(f.Name)
		if other, ok := seen[key]; ok {
			return nil, fmt.Errorf("PATH_CASE_COLLISION: %s and %s differ only by letter case", other, f.Name)
		}
		seen[key] = f.Name
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range sorted {
		h := &zip.FileHeader{
			Name:               f.Name,
			Method:             zip.Store,
			CRC32:              crc32.ChecksumIEEE(f.Data),
			CompressedSize64:   uint64(len(f.Data)),
			UncompressedSize64: uint64(len(f.Data)),
		}
		w, err := zw.CreateRaw(h)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(f.Data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	if buf.Len() > MaxPackageBytes {
		return nil, fmt.Errorf("PACKAGE_TOO_LARGE: %d bytes, the limit is %d", buf.Len(), MaxPackageBytes)
	}
	return buf.Bytes(), nil
}
