package pack

import (
	"archive/zip"
	"bytes"
	"math/rand"
	"strings"
	"testing"
)

func TestDeterministicAndSorted(t *testing.T) {
	a := []File{{"plugin.json", []byte("{}")}, {"ui/main.ui.json", []byte("[]")}, {"bin/x", []byte{1, 2}}}
	b := []File{a[2], a[0], a[1]}
	da, ha, err := Build(a)
	if err != nil {
		t.Fatal(err)
	}
	db, hb, _ := Build(b)
	if ha != hb || !bytes.Equal(da, db) {
		t.Fatal("input order changed the package")
	}
	zr, err := zip.NewReader(bytes.NewReader(da), int64(len(da)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		if !f.Modified.Equal(epoch) {
			t.Errorf("%s: timestamp %v", f.Name, f.Modified)
		}
	}
	if got := strings.Join(names, ","); got != "bin/x,plugin.json,ui/main.ui.json" {
		t.Fatalf("order %s", got)
	}
}

func TestRejectsNonPortableNames(t *testing.T) {
	for _, files := range [][]File{
		{{"ui/A.json", nil}, {"ui/a.json", nil}},
		{{`ui\a.json`, nil}},
		{{"../x", nil}},
		{{"ui/aux.json", nil}},
	} {
		if _, _, err := Build(files); err == nil {
			t.Errorf("%v: expected an error", files)
		}
	}
}

func TestSizeLimit(t *testing.T) {
	big := make([]byte, MaxPackageBytes+1)
	rand.New(rand.NewSource(1)).Read(big) // incompressible
	if _, _, err := Build([]File{{"bin/x", big}}); err == nil || !strings.HasPrefix(err.Error(), "PACKAGE_TOO_LARGE") {
		t.Fatalf("got %v", err)
	}
}
