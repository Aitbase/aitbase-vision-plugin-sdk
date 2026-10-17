package pack

import (
	"archive/zip"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/Aitbase/aitbase-vision-plugin-sdk/internal/signing"
)

// SignaturePrefix starts the zip comment of a signed package.
const SignaturePrefix = "AVP-SIG-1 "

// BuildAVP writes a signed package (format 1): a deterministic zip of files, its comment
// "AVP-SIG-1 <base64 public key> <base64 signature>". The ECDSA-SHA256 signature covers every byte
// before the comment length field. The module and sounds are stored, not deflated, so the device
// can use them in place without a second copy in memory.
//
// It returns the package and the digest of the signed bytes, which depends only on the contents
// (the signature itself differs each time).
func BuildAVP(files []File, key *ecdsa.PrivateKey) ([]byte, string, error) {
	unsigned, _, err := build(files, func(name string) uint16 {
		if strings.HasSuffix(name, ".wasm") || strings.HasSuffix(name, ".wav") {
			return zip.Store
		}
		return zip.Deflate
	})
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
