package pack

import (
	"bytes"
	"strings"
	"testing"
)

func leb(n int) []byte {
	var out []byte
	for {
		b := byte(n & 0x7f)
		n >>= 7
		if n != 0 {
			out = append(out, b|0x80)
			continue
		}
		return append(out, b)
	}
}

func name(s string) []byte { return append(leb(len(s)), s...) }

func sec(id byte, payload []byte) []byte {
	return append(append([]byte{id}, leb(len(payload))...), payload...)
}

// testModule has only the sections CheckModule reads: imports, exports, optional extras.
func testModule(imports [][2]string, exports []string, extra ...[]byte) []byte {
	out := append([]byte(nil), wasmHeader...)
	if len(imports) > 0 {
		body := leb(len(imports))
		for _, imp := range imports {
			body = append(append(append(body, name(imp[0])...), name(imp[1])...), 0, 0)
		}
		out = append(out, sec(2, body)...)
	}
	body := leb(len(exports))
	for i, e := range exports {
		kind := byte(0)
		if e == "memory" {
			kind = 2
		}
		body = append(append(append(body, name(e)...), kind), leb(i)...)
	}
	out = append(out, sec(7, body)...)
	for _, x := range extra {
		out = append(out, x...)
	}
	return out
}

var abiImports = [][2]string{{"aitbase", "send"}, {"aitbase", "log"}, {"wasi_snapshot_preview1", "fd_write"}}

func TestCheckModule(t *testing.T) {
	good := testModule(abiImports, requiredExports)
	if err := CheckModule(good); err != nil {
		t.Fatalf("valid module refused: %v", err)
	}
	// Memory at the cap, with a larger maximum: the device caps growth, so that is fine.
	atCap := testModule(abiImports, requiredExports, sec(5, append(leb(1), append(append([]byte{1}, leb(MaxMemoryPages)...), leb(65536)...)...)))
	if err := CheckModule(atCap); err != nil {
		t.Fatalf("memory at the cap refused: %v", err)
	}
	cases := []struct {
		name, code string
		data       []byte
	}{
		{"file import", "WASM_IMPORT_REFUSED", testModule(append(abiImports, [2]string{"wasi_snapshot_preview1", "path_open"}), requiredExports)},
		{"env import", "WASM_IMPORT_REFUSED", testModule([][2]string{{"env", "system"}}, requiredExports)},
		{"missing export", "WASM_EXPORT_MISSING", testModule(abiImports, requiredExports[:3])},
		{"start section", "WASM_START_SECTION", testModule(abiImports, requiredExports, sec(8, leb(0)))},
		{"not wasm", "WASM_INVALID", []byte("\x7fELF native binary")},
		{"truncated", "WASM_INVALID", good[:len(good)-3]},
		{"initial memory over the cap", "WASM_MEMORY_TOO_LARGE", testModule(abiImports, requiredExports, sec(5, append(leb(1), append([]byte{0}, leb(MaxMemoryPages+1)...)...)))},
		{"too large", "WASM_TOO_LARGE", append(append([]byte(nil), good...), make([]byte, MaxModuleBytes)...)},
	}
	for _, c := range cases {
		err := CheckModule(c.data)
		if err == nil || !strings.HasPrefix(err.Error(), c.code+": ") {
			t.Errorf("%s: got %v, want %s", c.name, err, c.code)
		}
	}
}

func TestStripCustomSections(t *testing.T) {
	debug := sec(0, append(name(".debug_info"), bytes.Repeat([]byte{1}, 1000)...))
	withDebug := testModule(abiImports, requiredExports, debug, sec(0, name("producers")))
	stripped, err := StripCustomSections(withDebug)
	if err != nil {
		t.Fatal(err)
	}
	if want := testModule(abiImports, requiredExports); !bytes.Equal(stripped, want) {
		t.Fatalf("custom sections not removed exactly: %d bytes, want %d", len(stripped), len(want))
	}
	if _, err := StripCustomSections([]byte("not wasm")); err == nil {
		t.Fatal("non-module stripped")
	}
}
