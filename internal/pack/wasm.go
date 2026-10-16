package pack

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// The device's WASM host ABI (docs/wasm-abi.md): what a module may import, and must export.
var allowedImports = map[string]bool{
	"aitbase.send": true, "aitbase.log": true, "aitbase.timer_set": true, "aitbase.package_read": true,
	"wasi_snapshot_preview1.fd_write": true, "wasi_snapshot_preview1.fd_close": true,
	"wasi_snapshot_preview1.fd_seek": true, "wasi_snapshot_preview1.clock_time_get": true,
	"wasi_snapshot_preview1.proc_exit": true,
}

var requiredExports = []string{"memory", "aitbase_abi_version", "aitbase_rx_buffer", "aitbase_start",
	"aitbase_message", "aitbase_timer", "aitbase_stop"}

// MaxModuleBytes is the largest module the device accepts.
const MaxModuleBytes = 4 << 20

// MaxMemoryPages is the device's linear-memory cap, in 64 KiB pages (2 MiB). memory.grow past it
// returns -1, and the device refuses a module whose initial memory is already larger.
const MaxMemoryPages = 32

var wasmHeader = []byte{0, 'a', 's', 'm', 1, 0, 0, 0}

type section struct {
	id         byte
	start, end int // payload
	raw        []byte
}

func readULEB(data []byte, pos int) (uint32, int, error) {
	var result uint32
	for shift := 0; shift < 35; shift += 7 {
		if pos >= len(data) {
			return 0, 0, fmt.Errorf("truncated module")
		}
		b := data[pos]
		pos++
		result |= uint32(b&0x7f) << shift
		if b&0x80 == 0 {
			return result, pos, nil
		}
	}
	return 0, 0, fmt.Errorf("malformed module")
}

func sections(data []byte) ([]section, error) {
	if !bytes.HasPrefix(data, wasmHeader) {
		return nil, fmt.Errorf("not a WebAssembly module")
	}
	var out []section
	for pos := len(wasmHeader); pos < len(data); {
		id := data[pos]
		size, start, err := readULEB(data, pos+1)
		if err != nil {
			return nil, err
		}
		end := start + int(size)
		if end > len(data) || end < start {
			return nil, fmt.Errorf("truncated module")
		}
		out = append(out, section{id, start, end, data[pos:end]})
		pos = end
	}
	return out, nil
}

func readName(data []byte, pos int) (string, int, error) {
	n, pos, err := readULEB(data, pos)
	if err != nil || pos+int(n) > len(data) {
		return "", 0, fmt.Errorf("malformed module")
	}
	return string(data[pos : pos+int(n)]), pos + int(n), nil
}

// CheckModule reports what would make the device refuse the module: a diagnostic code, a colon
// and a message.
func CheckModule(data []byte) error {
	if len(data) > MaxModuleBytes {
		return fmt.Errorf("WASM_TOO_LARGE: %d bytes, the device accepts at most %d", len(data), MaxModuleBytes)
	}
	secs, err := sections(data)
	if err != nil {
		return fmt.Errorf("WASM_INVALID: %v", err)
	}
	var refused []string
	exports := map[string]bool{}
	for _, s := range secs {
		body := data[:s.end]
		switch s.id {
		case 2: // imports
			count, pos, err := readULEB(body, s.start)
			for i := uint32(0); err == nil && i < count; i++ {
				var module, field string
				if module, pos, err = readName(body, pos); err != nil {
					break
				}
				if field, pos, err = readName(body, pos); err != nil || pos >= len(body) {
					err = fmt.Errorf("malformed import section")
					break
				}
				kind := body[pos]
				pos++
				if kind != 0 {
					refused = append(refused, module+"."+field+" (not a function)")
					err = fmt.Errorf("non-function import")
					break
				}
				_, pos, err = readULEB(body, pos)
				if !allowedImports[module+"."+field] {
					refused = append(refused, module+"."+field)
				}
			}
			if err != nil && len(refused) == 0 {
				return fmt.Errorf("WASM_INVALID: %v", err)
			}
		case 7: // exports
			count, pos, err := readULEB(body, s.start)
			for i := uint32(0); err == nil && i < count; i++ {
				var name string
				if name, pos, err = readName(body, pos); err != nil || pos >= len(body) {
					return fmt.Errorf("WASM_INVALID: malformed export section")
				}
				_, pos, err = readULEB(body, pos+1)
				exports[name] = true
			}
			if err != nil {
				return fmt.Errorf("WASM_INVALID: %v", err)
			}
		case 5: // memories: flags, initial pages, optional maximum
			count, pos, err := readULEB(body, s.start)
			for i := uint32(0); err == nil && i < count; i++ {
				if pos >= len(body) {
					err = fmt.Errorf("malformed memory section")
					break
				}
				flags := body[pos]
				var initial uint32
				if initial, pos, err = readULEB(body, pos+1); err != nil {
					break
				}
				if flags&1 != 0 {
					_, pos, err = readULEB(body, pos)
				}
				if err == nil && initial > MaxMemoryPages {
					return fmt.Errorf("WASM_MEMORY_TOO_LARGE: the module starts with %d pages (%d KiB) of memory, the device allows %d (%d KiB); make large static arrays smaller, or allocate them with malloc",
						initial, initial*64, MaxMemoryPages, MaxMemoryPages*64)
				}
			}
			if err != nil {
				return fmt.Errorf("WASM_INVALID: %v", err)
			}
		case 8:
			return fmt.Errorf("WASM_START_SECTION: the module has a start section; build it with the SDK's CMake, which links a reactor")
		}
	}
	if len(refused) > 0 {
		sort.Strings(refused)
		return fmt.Errorf("WASM_IMPORT_REFUSED: the device does not provide %s; use the calls in aitbase_plugin.h (files, sockets, environment and randomness are not available)",
			strings.Join(refused, ", "))
	}
	var missing []string
	for _, name := range requiredExports {
		if !exports[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("WASM_EXPORT_MISSING: no %s; link aitbase::plugin and build with `aitbase-plugin build --wasm`",
			strings.Join(missing, ", "))
	}
	return nil
}

// StripCustomSections drops custom sections (debug information, names, producers): the device
// doesn't use them, and they are most of an unstripped module.
func StripCustomSections(data []byte) ([]byte, error) {
	secs, err := sections(data)
	if err != nil {
		return nil, err
	}
	out := append([]byte(nil), wasmHeader...)
	for _, s := range secs {
		if s.id != 0 {
			out = append(out, s.raw...)
		}
	}
	return out, nil
}

// SDFileName is the WASM package name: <id>-<version>-wasm.zip.
func SDFileName(id, version string) string {
	return id + "-" + version + "-wasm.zip"
}

// SDDir is where a WASM package lives on the device's SD card, and inside its zip.
func SDDir(id string) string {
	return "aitbase/plugins/" + id
}
