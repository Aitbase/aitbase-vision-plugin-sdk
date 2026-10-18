package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Aitbase/aitbase-vision-plugin-sdk/internal/device"
)

// fakeDevice serves the plugin developer routes (contracts/plugin-dev-api in the device app) the
// way ControlCenter does, for one token.
type fakeDevice struct {
	mu            sync.Mutex
	password      string
	defaultPass   bool
	token         string
	upload        []byte
	committed     []byte
	launches      int
	running       string
	skipReason    string // when set, a commit is skipped instead of started
	log           string
	revoked       bool
	maxChunk      int
	authHeaders   []string
	pairPasswords []string
	foreground    bool // a community plugin's screen shows
	pressed       []string
}

func (f *fakeDevice) reply(w http.ResponseWriter, status int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (f *fakeDevice) fail(w http.ResponseWriter, status int, code string) {
	f.reply(w, status, map[string]any{"success": false, "error": code, "message": code + " from the device"})
}

func (f *fakeDevice) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	route := strings.TrimPrefix(r.URL.Path, "/api/v1/plugins/dev/")
	body, _ := io.ReadAll(r.Body)
	if route == "pair" {
		var req struct{ Password, Name string }
		_ = json.Unmarshal(body, &req)
		f.pairPasswords = append(f.pairPasswords, req.Password)
		switch {
		case f.defaultPass:
			f.fail(w, 403, "default_password")
		case req.Password != f.password:
			f.fail(w, 401, "password_incorrect")
		default:
			f.token = "aitbase_dev_" + strings.Repeat("ab", 32)
			f.reply(w, 200, map[string]any{"success": true, "token": f.token, "token_id": "abababababab", "expires_at": 4102444800})
		}
		return
	}
	f.authHeaders = append(f.authHeaders, r.Header.Get("Authorization"))
	if f.token == "" || f.revoked || r.Header.Get("Authorization") != "Bearer "+f.token {
		f.fail(w, 401, "unauthorized")
		return
	}
	switch route {
	case "status":
		st := map[string]any{"success": true, "launches": f.launches, "packages": []any{}, "skipped": []any{}}
		if f.running != "" {
			st["running"] = map[string]any{"plugin_id": f.running}
		}
		if f.skipReason != "" && f.committed != nil {
			st["skipped"] = []any{map[string]any{"folder": "alice.hello-1.0.0.avp", "reason": f.skipReason}}
		}
		f.reply(w, 200, st)
	case "package":
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if len(body) > f.maxChunk {
			f.maxChunk = len(body)
		}
		if offset == 0 {
			f.upload = nil
		}
		if offset != len(f.upload) {
			f.reply(w, 409, map[string]any{"success": false, "error": "offset_mismatch", "received": len(f.upload)})
			return
		}
		f.upload = append(f.upload, body...)
		f.reply(w, 200, map[string]any{"success": true, "received": len(f.upload)})
	case "package/commit":
		var req struct {
			Sha256 string
			Size   int
		}
		_ = json.Unmarshal(body, &req)
		sum := sha256.Sum256(f.upload)
		if req.Size != len(f.upload) || req.Sha256 != hex.EncodeToString(sum[:]) {
			f.fail(w, 422, "digest_mismatch")
			return
		}
		f.committed = f.upload
		if f.skipReason == "" {
			f.launches++
			f.running = "alice.hello"
		}
		f.reply(w, 200, map[string]any{"success": true, "plugin_id": "alice.hello", "version": "1.0.0", "author": "ab12 cd34",
			"file": "alice.hello-1.0.0.avp", "replaced": false})
	case "logs":
		f.reply(w, 200, map[string]any{"success": true, "plugin_id": r.URL.Query().Get("id"), "offset": 0,
			"next_offset": len(f.log), "size": len(f.log), "restarted": false, "text": f.log})
	case "screenshot":
		if !f.foreground {
			f.fail(w, 409, "not_foreground")
			return
		}
		// 2x1: red, green.
		f.reply(w, 200, map[string]any{"success": true, "width": 2, "height": 1, "format": "rgb565le", "pixels": "APjgBw=="})
	case "press":
		var req struct {
			Button string
			Long   bool
		}
		_ = json.Unmarshal(body, &req)
		if !f.foreground {
			f.fail(w, 409, "not_foreground")
			return
		}
		f.pressed = append(f.pressed, fmt.Sprintf("%s long=%v", req.Button, req.Long))
		f.reply(w, 200, map[string]any{"success": true})
	case "token":
		f.revoked = true
		f.reply(w, 200, map[string]any{"success": true})
	default:
		f.fail(w, 404, "not_found")
	}
}

// startFake serves a fake device and points the CLI's paired devices file at a temporary one.
func startFake(t *testing.T) (*fakeDevice, string) {
	t.Helper()
	f := &fakeDevice{password: "owner-password-123"}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	t.Setenv(device.EnvDevices, filepath.Join(t.TempDir(), "cfg", "devices.json"))
	pollInterval = 10 * time.Millisecond
	return f, strings.TrimPrefix(srv.URL, "http://")
}

func withPassword(t *testing.T, password string, err error) {
	t.Helper()
	old := readPassword
	readPassword = func(env, string) (string, error) { return password, err }
	t.Cleanup(func() { readPassword = old })
}

func TestPairStoresOnlyTheToken(t *testing.T) {
	f, addr := startFake(t)
	withPassword(t, "owner-password-123", nil)
	code, res := run(t, "pair", addr, "--name", "laptop")
	if code != 0 || res["device"] != addr || res["token_id"] != "abababababab" {
		t.Fatalf("pair: %d %v", code, res)
	}
	out, _ := json.Marshal(res)
	if strings.Contains(string(out), f.token) {
		t.Fatal("the JSON result shows the token")
	}
	path := os.Getenv(device.EnvDevices)
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), f.token) || strings.Contains(string(data), "owner-password") {
		t.Fatalf("devices file: %v %s", err, data)
	}
	if info, _ := os.Stat(path); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("devices file mode %v", info.Mode())
	}
}

func TestPairNeedsATerminal(t *testing.T) {
	f, addr := startFake(t)
	withPassword(t, "", errors.New("no terminal"))
	code, res := run(t, "pair", addr)
	if code != exitFailed || !strings.Contains(fmtDiags(res), "PASSWORD_NEEDS_TERMINAL") || len(f.pairPasswords) != 0 {
		t.Fatalf("pair without a terminal: %d %v", code, res)
	}
}

func TestPairReportsTheDevicesRefusal(t *testing.T) {
	f, addr := startFake(t)
	withPassword(t, "aitbase-camera", nil)
	f.defaultPass = true
	if code, res := run(t, "pair", addr); code != exitFailed || !strings.Contains(fmtDiags(res), "DEVICE_DEFAULT_PASSWORD") {
		t.Fatalf("default password: %d %v", code, res)
	}
	f.defaultPass = false
	if code, res := run(t, "pair", addr); code != exitFailed || !strings.Contains(fmtDiags(res), "DEVICE_PASSWORD_INCORRECT") {
		t.Fatalf("wrong password: %d %v", code, res)
	}
	if code, res := run(t, "pair", "https://"+addr); code != exitUsage {
		t.Fatalf("https: %d %v", code, res)
	}
}

func pairWithFake(t *testing.T) (*fakeDevice, *device.Client) {
	t.Helper()
	f, addr := startFake(t)
	withPassword(t, "owner-password-123", nil)
	if code, res := run(t, "pair", addr); code != 0 {
		t.Fatalf("pair: %v", res)
	}
	client, d := pickDevice("")
	if d != nil {
		t.Fatal(d.Message)
	}
	return f, client
}

func TestDeployUploadsInChunksAndWaitsForTheRun(t *testing.T) {
	f, client := pairWithFake(t)
	pkg := make([]byte, 2*device.ChunkBytes+1000)
	for i := range pkg {
		pkg[i] = byte(i)
	}
	done, d := deploy(client, pkg, time.Second)
	if d != nil {
		t.Fatal(d.Code, d.Message)
	}
	if done.PluginID != "alice.hello" || string(f.committed) != string(pkg) || f.maxChunk != device.ChunkBytes {
		t.Fatalf("deploy: %+v, committed %d bytes, largest chunk %d", done, len(f.committed), f.maxChunk)
	}
	for _, h := range f.authHeaders {
		if h != "Bearer "+f.token {
			t.Fatalf("a request without the bearer token: %q", h)
		}
	}
}

func TestDeployWaitsForANewRun(t *testing.T) {
	f, client := pairWithFake(t)
	// The same plugin already runs: only a new launch counts.
	f.running = "alice.hello"
	f.skipReason = "signature does not match its contents"
	_, d := deploy(client, []byte("package"), time.Second)
	if d == nil || d.Code != "DEVICE_PACKAGE_SKIPPED" || !strings.Contains(d.Message, "signature") {
		t.Fatalf("skipped package: %+v", d)
	}
}

func TestLogsAndUnpair(t *testing.T) {
	f, client := pairWithFake(t)
	f.log = "[alice.hello] info: one\n"
	code, res := run(t, "logs", "--id", "alice.hello")
	if code != 0 || res["text"] != f.log || res["device"] != client.Address {
		t.Fatalf("logs: %d %v", code, res)
	}
	if code, res := run(t, "logs", "--id", "alice.hello", "--follow"); code != exitUsage {
		t.Fatalf("--follow with --json: %d %v", code, res)
	}
	if code, res := run(t, "unpair"); code != 0 || !f.revoked {
		t.Fatalf("unpair: %d %v", code, res)
	}
	if code, res := run(t, "logs", "--id", "alice.hello"); code != exitFailed || !strings.Contains(fmtDiags(res), "DEVICE_NOT_PAIRED") {
		t.Fatalf("after unpair: %d %v", code, res)
	}
}

func TestUnreachableDevice(t *testing.T) {
	_, client := pairWithFake(t)
	client.Address = "127.0.0.1:1"
	if _, d := deploy(client, []byte("x"), time.Second); d == nil || d.Code != "DEVICE_UNREACHABLE" {
		t.Fatalf("unreachable: %+v", d)
	}
}

func TestScreenshotAndPress(t *testing.T) {
	f, _ := pairWithFake(t)
	out := filepath.Join(t.TempDir(), "shot.png")
	if code, res := run(t, "screenshot", "-o", out); code != exitFailed || !strings.Contains(fmtDiags(res), "DEVICE_NOT_FOREGROUND") {
		t.Fatalf("screenshot of a core screen: %d %v", code, res)
	}
	if code, res := run(t, "press", "right"); code != exitFailed || !strings.Contains(fmtDiags(res), "DEVICE_NOT_FOREGROUND") {
		t.Fatalf("press on a core screen: %d %v", code, res)
	}
	f.foreground = true
	code, res := run(t, "screenshot", "-o", out)
	if code != 0 || res["file"] != out || res["width"] != float64(2) {
		t.Fatalf("screenshot: %d %v", code, res)
	}
	file, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	img, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := img.At(0, 0).RGBA()
	r2, g2, b2, _ := img.At(1, 0).RGBA()
	if r>>8 != 0xff || g != 0 || b != 0 || r2 != 0 || g2>>8 != 0xff || b2 != 0 {
		t.Fatalf("pixels: %v %v", img.At(0, 0), img.At(1, 0))
	}
	if code, res := run(t, "press", "left", "--long"); code != 0 || res["long"] != true {
		t.Fatalf("press: %d %v", code, res)
	}
	if code, _ := run(t, "press", "right"); code != 0 {
		t.Fatal("press right")
	}
	if strings.Join(f.pressed, ",") != "left long=true,right long=false" {
		t.Fatalf("pressed: %v", f.pressed)
	}
	if code, _ := run(t, "press", "up"); code != exitUsage {
		t.Fatal("press up was accepted")
	}
}
