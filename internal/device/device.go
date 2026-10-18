// Package device talks to a device's plugin developer routes (D19, contracts/plugin-dev-api in the
// device app): pair once with the Device Console password, then upload signed packages and read
// status and logs with a developer token.
//
// Paired devices are kept in <user config dir>/aitbase-plugin/devices.json (or $AITBASE_DEVICES),
// owner-only, next to the signing key: address, token and its id. The password is never stored.
package device

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// EnvDevices overrides the paired devices file's path.
const EnvDevices = "AITBASE_DEVICES"

// ChunkBytes is the largest upload chunk the device accepts.
const ChunkBytes = 256 * 1024

const prefix = "/api/v1/plugins/dev/"

// Error is a refusal from the device, with its stable code ("default_password", "author_changed"...).
type Error struct {
	Status  int
	Code    string
	Message string
	// RetryAfter is set for rate_limited, in seconds.
	RetryAfter int
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("the device answered %d %s", e.Status, e.Code)
}

// ErrUnreachable wraps network failures: no answer from the address.
var ErrUnreachable = errors.New("the device does not answer")

// Normalize turns "192.168.1.50", "192.168.1.50:8080" or "http://192.168.1.50/" into "host[:port]".
func Normalize(address string) (string, error) {
	a := strings.TrimSpace(address)
	if strings.HasPrefix(a, "https://") {
		return "", errors.New("devices serve plain HTTP on the local network; use the address without https://")
	}
	a = strings.TrimPrefix(a, "http://")
	a = strings.TrimSuffix(a, "/")
	if a == "" || strings.ContainsAny(a, "/?#@ ") {
		return "", fmt.Errorf("%q is not a device address; use its IP address, for example 192.168.1.50", address)
	}
	if host, port, err := net.SplitHostPort(a); err == nil {
		if host == "" || port == "" {
			return "", fmt.Errorf("%q is not a device address", address)
		}
	}
	return a, nil
}

// Client calls one device. Token is empty before pairing.
type Client struct {
	Address string
	Token   string
	HTTP    *http.Client
}

// New returns a client for address (normalized) with a 30 s timeout per request.
func New(address, token string) *Client {
	return &Client{Address: address, Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) do(method, route, query, contentType string, body []byte, out any) error {
	u := "http://" + c.Address + prefix + route
	if query != "" {
		u += "?" + query
	}
	req, err := http.NewRequest(method, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%w at %s: %v", ErrUnreachable, c.Address, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("%w at %s: %v", ErrUnreachable, c.Address, err)
	}
	var head struct {
		Success    bool   `json:"success"`
		Error      string `json:"error"`
		Message    string `json:"message"`
		RetryAfter int    `json:"retry_after_sec"`
	}
	if json.Unmarshal(data, &head) != nil {
		if resp.StatusCode == http.StatusNotFound {
			return &Error{Status: resp.StatusCode, Code: "not_found",
				Message: "the device has no plugin developer routes; update its firmware"}
		}
		return &Error{Status: resp.StatusCode, Code: "bad_reply", Message: "the device sent a reply the CLI can't read"}
	}
	if resp.StatusCode != http.StatusOK || !head.Success {
		code := head.Error
		if code == "" {
			code = "http_" + fmt.Sprint(resp.StatusCode)
		}
		return &Error{Status: resp.StatusCode, Code: code, Message: head.Message, RetryAfter: head.RetryAfter}
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// Paired is what pairing returns.
type Paired struct {
	Token     string `json:"token"`
	TokenID   string `json:"token_id"`
	ExpiresAt int64  `json:"expires_at"`
}

// Pair exchanges the Device Console password for a developer token.
func (c *Client) Pair(password, name string) (*Paired, error) {
	body, _ := json.Marshal(map[string]string{"password": password, "name": name})
	var p Paired
	if err := c.do(http.MethodPost, "pair", "", "application/json", body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// Status is the device's plugin status (the fields the CLI reads).
type Status struct {
	Launches int `json:"launches"`
	Running  *struct {
		PluginID string `json:"plugin_id"`
	} `json:"running"`
	LastEnded *struct {
		PluginID string `json:"plugin_id"`
		Reason   string `json:"reason"`
	} `json:"last_ended"`
	Skipped []struct {
		Folder string `json:"folder"`
		Reason string `json:"reason"`
	} `json:"skipped"`
}

// Status reads the plugin status.
func (c *Client) Status() (*Status, error) {
	var s Status
	if err := c.do(http.MethodGet, "status", "", "", nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Committed describes the package now on the device's SD card.
type Committed struct {
	PluginID string `json:"plugin_id"`
	Version  string `json:"version"`
	Author   string `json:"author"`
	File     string `json:"file"`
	Replaced bool   `json:"replaced"`
}

// Upload sends a signed package in chunks, then commits it; the device verifies it first.
func (c *Client) Upload(pkg []byte) (*Committed, error) {
	for at := 0; at < len(pkg); at += ChunkBytes {
		end := at + ChunkBytes
		if end > len(pkg) {
			end = len(pkg)
		}
		if err := c.do(http.MethodPut, "package", "offset="+fmt.Sprint(at), "application/octet-stream", pkg[at:end], nil); err != nil {
			return nil, err
		}
	}
	sum := sha256.Sum256(pkg)
	body, _ := json.Marshal(map[string]any{"sha256": hex.EncodeToString(sum[:]), "size": len(pkg)})
	var done Committed
	if err := c.do(http.MethodPost, "package/commit", "", "application/json", body, &done); err != nil {
		return nil, err
	}
	return &done, nil
}

// Log is a piece of a plugin's log.
type Log struct {
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"next_offset"`
	Size       int64  `json:"size"`
	Restarted  bool   `json:"restarted"`
	Text       string `json:"text"`
}

// Logs reads plugin id's log from offset.
func (c *Client) Logs(id string, offset int64) (*Log, error) {
	var l Log
	q := url.Values{"id": {id}, "offset": {fmt.Sprint(offset)}}
	if err := c.do(http.MethodGet, "logs", q.Encode(), "", nil, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// Revoke revokes the client's token on the device.
func (c *Client) Revoke() error {
	return c.do(http.MethodDelete, "token", "", "", nil, nil)
}

// Entry is one paired device.
type Entry struct {
	Token    string `json:"token"`
	TokenID  string `json:"token_id"`
	PairedAt int64  `json:"paired_at"`
}

// Store is the paired devices file.
type Store struct {
	Default string           `json:"default"`
	Devices map[string]Entry `json:"devices"`
}

// StorePath is <user config dir>/aitbase-plugin/devices.json, or $AITBASE_DEVICES.
func StorePath() (string, error) {
	if p := os.Getenv(EnvDevices); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "aitbase-plugin", "devices.json"), nil
}

// LoadStore reads the paired devices; a missing file is an empty store.
func LoadStore() (*Store, string, error) {
	path, err := StorePath()
	if err != nil {
		return nil, "", err
	}
	s := &Store{Devices: map[string]Entry{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, path, nil
	}
	if err != nil {
		return nil, path, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, path, fmt.Errorf("%s is damaged: %v", path, err)
	}
	if s.Devices == nil {
		s.Devices = map[string]Entry{}
	}
	return s, path, nil
}

// Save writes the store owner-only, atomically.
func (s *Store) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Pick returns the device to use: address when given (normalized), else the default.
func (s *Store) Pick(address string) (string, Entry, error) {
	if address != "" {
		a, err := Normalize(address)
		if err != nil {
			return "", Entry{}, err
		}
		e, ok := s.Devices[a]
		if !ok {
			return a, Entry{}, fmt.Errorf("%s is not paired", a)
		}
		return a, e, nil
	}
	if e, ok := s.Devices[s.Default]; ok && s.Default != "" {
		return s.Default, e, nil
	}
	if len(s.Devices) == 1 {
		for a, e := range s.Devices {
			return a, e, nil
		}
	}
	names := make([]string, 0, len(s.Devices))
	for a := range s.Devices {
		names = append(names, a)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "", Entry{}, errors.New("no device is paired")
	}
	return "", Entry{}, fmt.Errorf("several devices are paired (%s); pass --device", strings.Join(names, ", "))
}
