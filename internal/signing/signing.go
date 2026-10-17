// Package signing holds the author's package signing key (D12).
//
// Every package is signed with the author's ECDSA P-256 key. The CLI creates the key the first
// time it packs, in the user's configuration directory, and never uploads it. The device pins the
// first key it sees for a plugin id, so keep the key and back it up: a package signed with a new
// key is refused until the owner forgets the plugin in the Device Console.
package signing

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvKey overrides the key file's path.
const EnvKey = "AITBASE_SIGNING_KEY"

// DefaultPath is <user config dir>/aitbase-plugin/signing-key.pem, or $AITBASE_SIGNING_KEY.
func DefaultPath() (string, error) {
	if p := os.Getenv(EnvKey); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "aitbase-plugin", "signing-key.pem"), nil
}

// LoadOrCreate reads the key at path, creating it (owner-only permissions) if it doesn't exist.
func LoadOrCreate(path string) (key *ecdsa.PrivateKey, created bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, false, err
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return nil, false, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, false, err
		}
		out := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
		// O_EXCL: never replace a key another process just created.
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, false, err
		}
		if _, err := f.Write(out); err != nil {
			f.Close()
			return nil, false, err
		}
		return key, true, f.Close()
	}
	if err != nil {
		return nil, false, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, false, fmt.Errorf("%s is not a PEM private key", path)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", path, err)
	}
	ec, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || ec.Curve != elliptic.P256() {
		return nil, false, fmt.Errorf("%s is not an ECDSA P-256 key", path)
	}
	return ec, false, nil
}

// PublicKeyDER is the public key as the device reads it (DER SubjectPublicKeyInfo).
func PublicKeyDER(key *ecdsa.PrivateKey) ([]byte, error) {
	return x509.MarshalPKIXPublicKey(&key.PublicKey)
}

// Fingerprint is the author identity the device shows: SHA-256 of the public key, hex.
func Fingerprint(key *ecdsa.PrivateKey) (string, error) {
	der, err := PublicKeyDER(key)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:]), nil
}

// Short is the fingerprint as the Device Console shows it: "ab12 cd34 ef56 7890".
func Short(fingerprint string) string {
	if len(fingerprint) < 16 {
		return fingerprint
	}
	parts := []string{fingerprint[0:4], fingerprint[4:8], fingerprint[8:12], fingerprint[12:16]}
	return strings.Join(parts, " ")
}
