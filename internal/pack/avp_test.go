package pack

import (
	"archive/zip"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/binary"
	"strings"
	"testing"
)

// The device reads entries in place (control_center/src/plugin_avp.cpp): every entry stored, its
// sizes in the local header, no extra field, no data descriptor, and the signature last.
func TestBuildAVPLayout(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	files := []File{
		{Name: "plugin.json", Data: []byte(`{"id":"alice.p"}`)},
		{Name: "p.wasm", Data: bytes.Repeat([]byte{0, 'a', 's', 'm'}, 300)},
		{Name: "main.ui.json", Data: []byte(`{"screen":"main"}`)},
		{Name: "sounds/beep.wav", Data: bytes.Repeat([]byte{1}, 100)},
	}
	data, digest, err := BuildAVP(files, key)
	if err != nil {
		t.Fatal(err)
	}
	again, digest2, err := BuildAVP(files, key)
	if err != nil || digest2 != digest {
		t.Fatal("the signed contents are not deterministic")
	}
	if bytes.Equal(data, again) {
		t.Log("signatures happened to match")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(zr.Comment, SignaturePrefix) {
		t.Fatalf("comment %q", zr.Comment)
	}
	for _, f := range zr.File {
		off, err := f.DataOffset()
		if err != nil {
			t.Fatal(err)
		}
		local := off - 30 - int64(len(f.Name))
		h := data[local:]
		switch {
		case f.Method != zip.Store:
			t.Errorf("%s is compressed", f.Name)
		case binary.LittleEndian.Uint32(h) != 0x04034b50:
			t.Errorf("%s: no local header where expected", f.Name)
		case binary.LittleEndian.Uint16(h[6:])&8 != 0:
			t.Errorf("%s uses a data descriptor", f.Name)
		case binary.LittleEndian.Uint16(h[28:]) != 0:
			t.Errorf("%s has an extra field", f.Name)
		case binary.LittleEndian.Uint32(h[22:]) != uint32(f.UncompressedSize64):
			t.Errorf("%s: no size in the local header", f.Name)
		}
	}
}
