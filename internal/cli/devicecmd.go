package cli

// Device commands (D19 in the device app): pair once with the Device Console password, then
// upload, run and read logs over the local network with a developer token.

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/Aitbase/aitbase-vision-plugin-sdk/internal/check"
	"github.com/Aitbase/aitbase-vision-plugin-sdk/internal/device"
)

// readPassword asks for the Device Console password with echo off. Only an interactive terminal
// will do: the password must never pass through flags, environment variables, files or an AI
// agent's transcript. Tests replace it.
var readPassword = func(e env, prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("no terminal")
	}
	fmt.Fprint(e.stderr, prompt)
	data, err := term.ReadPassword(fd)
	fmt.Fprintln(e.stderr)
	return string(data), err
}

// pollInterval is how often dev and logs --follow ask the device. Tests shorten it.
var pollInterval = 500 * time.Millisecond

// deviceCodes maps the device's error codes to CLI codes and what to do about them.
var deviceCodes = map[string]struct{ code, fix string }{
	"default_password":   {"DEVICE_DEFAULT_PASSWORD", "change the password on the Device Console's Password page, then pair again"},
	"password_incorrect": {"DEVICE_PASSWORD_INCORRECT", "run `aitbase-plugin pair` again and type the Device Console password"},
	"rate_limited":       {"DEVICE_RATE_LIMITED", "wait, then try again"},
	"unauthorized":       {"DEVICE_NOT_PAIRED", "the token expired or was revoked: run `aitbase-plugin pair <address>` again"},
	"no_sd_card":         {"DEVICE_NO_SD_CARD", "insert an SD card with an aitbase/ folder at its root"},
	"invalid_package":    {"DEVICE_PACKAGE_REFUSED", "the device refused the package; run `aitbase-plugin pack --wasm` again"},
	"reserved_id":        {"DEVICE_PACKAGE_REFUSED", "use your own id prefix; aitbase.* is for official plugins"},
	"author_changed":     {"DEVICE_AUTHOR_CHANGED", "sign with the key the plugin was first signed with, or forget the plugin on the device's Plugins page (deletes its saved data)"},
	"digest_mismatch":    {"DEVICE_UPLOAD_FAILED", "upload again"},
	"offset_mismatch":    {"DEVICE_UPLOAD_FAILED", "upload again"},
	"no_upload":          {"DEVICE_UPLOAD_FAILED", "upload again"},
	"write_failed":       {"DEVICE_UPLOAD_FAILED", "check the SD card: it may be full or write-protected"},
	"package_too_large":  {"DEVICE_UPLOAD_FAILED", "a package is at most 4 MiB"},
	"not_found":          {"DEVICE_NOT_SUPPORTED", "update the device's firmware: it has no plugin developer routes"},
	"not_foreground":     {"DEVICE_NOT_FOREGROUND", "open the plugin's screen first: run `aitbase-plugin dev`, or ask the user to open it; screenshot and press only reach a plugin's own screen"},
	"busy":               {"DEVICE_BUSY", "wait a second, then press again"},
	"no_display":         {"DEVICE_NO_DISPLAY", "the device's display is off or not working"},
}

// deviceDiagnostic turns an error from the device client into a diagnostic.
func deviceDiagnostic(err error) check.Diagnostic {
	var de *device.Error
	if errors.As(err, &de) {
		c, ok := deviceCodes[de.Code]
		if !ok {
			c.code, c.fix = "DEVICE_ERROR", "read the message; update the CLI if the device is newer"
		}
		fix := c.fix
		if de.RetryAfter > 0 {
			fix = fmt.Sprintf("wait %d s, then try again", de.RetryAfter)
		}
		return check.Diagnostic{Severity: check.Error, Code: c.code, Message: err.Error(), Fix: fix}
	}
	if errors.Is(err, device.ErrUnreachable) {
		return check.Diagnostic{Severity: check.Error, Code: "DEVICE_UNREACHABLE", Message: err.Error(),
			Fix: "check the address (the device shows it in Settings) and that this computer is on the same network"}
	}
	return check.Diagnostic{Severity: check.Error, Code: "DEVICE_ERROR", Message: err.Error()}
}

func storeDiagnostic(err error) check.Diagnostic {
	return check.Diagnostic{Severity: check.Error, Code: "DEVICE_STORE", Message: "paired devices file: " + err.Error(),
		Fix: "check that the user configuration folder is writable, or set " + device.EnvDevices}
}

// pickDevice returns a client for --device, or for the paired default.
func pickDevice(address string) (*device.Client, *check.Diagnostic) {
	store, _, err := device.LoadStore()
	if err != nil {
		d := storeDiagnostic(err)
		return nil, &d
	}
	addr, entry, err := store.Pick(address)
	if err != nil {
		return nil, &check.Diagnostic{Severity: check.Error, Code: "DEVICE_NOT_PAIRED", Message: err.Error(),
			Fix: "run `aitbase-plugin pair <device address>` yourself, in a terminal"}
	}
	return device.New(addr, entry.Token), nil
}

func runPair(e env, args []string) int {
	fs, asJSON := newFlags(e, "pair")
	name := fs.String("name", "", "label for this computer on the device's Plugins page (default: host name)")
	pos, err := parse(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(pos) != 1 {
		return usageError(e, *asJSON, "pair", "usage: pair <device address> [--name <label>]")
	}
	addr, err := device.Normalize(pos[0])
	if err != nil {
		return usageError(e, *asJSON, "pair", err.Error())
	}
	label := *name
	if label == "" {
		label, _ = os.Hostname()
	}
	password, err := readPassword(e, "Device Console password for "+addr+": ")
	if err != nil || password == "" {
		return e.finish(*asJSON, "pair", false, []check.Diagnostic{{Severity: check.Error, Code: "PASSWORD_NEEDS_TERMINAL",
			Message: "pair asks for the Device Console password at a hidden prompt, and only there",
			Fix:     "run `aitbase-plugin pair " + addr + "` yourself, in a terminal; an AI agent must not handle the password"}}, nil, "")
	}
	paired, err := device.New(addr, "").Pair(password, label)
	if err != nil {
		return e.finish(*asJSON, "pair", false, []check.Diagnostic{deviceDiagnostic(err)}, nil, "")
	}
	store, path, err := device.LoadStore()
	if err == nil {
		store.Devices[addr] = device.Entry{Token: paired.Token, TokenID: paired.TokenID, PairedAt: time.Now().Unix()}
		store.Default = addr
		err = store.Save(path)
	}
	if err != nil {
		return e.finish(*asJSON, "pair", false, []check.Diagnostic{storeDiagnostic(err)}, nil, "")
	}
	expires := time.Unix(paired.ExpiresAt, 0).Format("2006-01-02")
	return e.finish(*asJSON, "pair", true, nil,
		result{"device": addr, "token_id": paired.TokenID, "expires_at": paired.ExpiresAt, "devices_file": path},
		fmt.Sprintf("Paired with %s (token %s, expires %s). The device's Plugins page lists and revokes it.\nNext: aitbase-plugin dev",
			addr, paired.TokenID, expires))
}

func runUnpair(e env, args []string) int {
	fs, asJSON := newFlags(e, "unpair")
	addrFlag := fs.String("device", "", "device address (default: the paired device)")
	if _, err := parse(fs, args); err != nil {
		return exitUsage
	}
	client, d := pickDevice(*addrFlag)
	if d != nil {
		return e.finish(*asJSON, "unpair", false, []check.Diagnostic{*d}, nil, "")
	}
	var diags []check.Diagnostic
	// An expired or revoked token is already gone on the device: forget it here all the same.
	if err := client.Revoke(); err != nil {
		var de *device.Error
		if !errors.As(err, &de) || de.Code != "unauthorized" {
			w := deviceDiagnostic(err)
			w.Severity = check.Warning
			diags = append(diags, w)
		}
	}
	store, path, err := device.LoadStore()
	if err == nil {
		delete(store.Devices, client.Address)
		if store.Default == client.Address {
			store.Default = ""
		}
		err = store.Save(path)
	}
	if err != nil {
		return e.finish(*asJSON, "unpair", false, append(diags, storeDiagnostic(err)), nil, "")
	}
	return e.finish(*asJSON, "unpair", true, diags, result{"device": client.Address}, "Unpaired "+client.Address)
}

func runDev(e env, args []string) int {
	fs, asJSON := newFlags(e, "dev")
	addrFlag := fs.String("device", "", "device address (default: the paired device)")
	buildDir := fs.String("build-dir", "", "CMake build directory (default: <dir>/build-wasm)")
	outDir := fs.String("out", "", "output directory for the .avp (default: <dir>/dist)")
	sdk := fs.String("sdk", "", "SDK directory (default: the nearest sdk/ in <dir> or a parent, else the SDK built into the CLI)")
	timeout := fs.Duration("timeout", 20*time.Second, "how long to wait for the device to start the plugin")
	pos, err := parse(fs, args)
	if err != nil {
		return exitUsage
	}
	dir, err := oneDir(pos)
	if err != nil {
		return usageError(e, *asJSON, "dev", err.Error())
	}
	fail := func(diags []check.Diagnostic) int { return e.finish(*asJSON, "dev", false, diags, nil, "dev failed") }

	client, d := pickDevice(*addrFlag)
	if d != nil {
		return fail([]check.Diagnostic{*d})
	}
	// The device refuses aitbase.* ids from the SD card, so dev never allows them.
	r := check.Project(dir, "", check.Options{SkipUI: true})
	if r.HasErrors() {
		return fail(r.Diagnostics)
	}
	built, d := buildProject(e, dir, *buildDir, *sdk, true)
	if d != nil {
		return fail(append(r.Diagnostics, *d))
	}
	packed, _, d := writeWasmPackage(dir, built["build_dir"].(string), pathFlag(dir, *outDir, "dist"), r)
	if d != nil {
		return fail(append(r.Diagnostics, *d))
	}
	pkg, err := os.ReadFile(packed["package"].(string))
	if err != nil {
		return fail(append(r.Diagnostics, check.Diagnostic{Severity: check.Error, Code: "WRITE_FAILED", Message: err.Error()}))
	}
	fmt.Fprintf(e.stderr, "Uploading %s (%d bytes) to %s...\n", filepath.Base(packed["package"].(string)), len(pkg), client.Address)
	done, d := deploy(client, pkg, *timeout)
	if d != nil {
		return fail(append(r.Diagnostics, *d))
	}
	return e.finish(*asJSON, "dev", true, r.Diagnostics,
		result{"device": client.Address, "package": packed["package"], "sha256": packed["sha256"], "author": packed["author"],
			"plugin_id": done.PluginID, "version": done.Version, "file": done.File, "running": true},
		fmt.Sprintf("%s %s is running on %s.\nLogs: aitbase-plugin logs --follow", done.PluginID, done.Version, client.Address))
}

// deploy uploads pkg and waits until the device has started it: autorun starts a package once its
// file is stable, within a few seconds. The launch count tells a new run from the one before.
func deploy(client *device.Client, pkg []byte, timeout time.Duration) (*device.Committed, *check.Diagnostic) {
	before, err := client.Status()
	if err != nil {
		d := deviceDiagnostic(err)
		return nil, &d
	}
	done, err := client.Upload(pkg)
	if err != nil {
		d := deviceDiagnostic(err)
		return nil, &d
	}
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); {
		time.Sleep(pollInterval)
		st, err := client.Status()
		if err != nil {
			d := deviceDiagnostic(err)
			return nil, &d
		}
		for _, s := range st.Skipped {
			if s.Folder == done.File {
				return nil, &check.Diagnostic{Severity: check.Error, Code: "DEVICE_PACKAGE_SKIPPED",
					Message: "the device didn't run " + done.File + ": " + s.Reason}
			}
		}
		if st.Launches <= before.Launches {
			continue
		}
		if st.Running != nil && st.Running.PluginID == done.PluginID {
			return done, nil
		}
		if st.LastEnded != nil && st.LastEnded.PluginID == done.PluginID {
			return nil, &check.Diagnostic{Severity: check.Error, Code: "DEVICE_PLUGIN_ENDED",
				Message: done.PluginID + " started and ended: " + st.LastEnded.Reason, Fix: "read `aitbase-plugin logs`"}
		}
	}
	return nil, &check.Diagnostic{Severity: check.Error, Code: "DEVICE_TIMEOUT",
		Message: fmt.Sprintf("%s is on the device's SD card, but it didn't start within %s", done.File, timeout),
		Fix:     "another plugin or the assistant may hold the screen; check the device, or `aitbase-plugin logs`"}
}

// toImage converts the device's RGB565 pixels to an image.
func toImage(s *device.Screen) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, s.Width, s.Height))
	for i := 0; i < s.Width*s.Height; i++ {
		v := uint16(s.Pixels[2*i]) | uint16(s.Pixels[2*i+1])<<8
		r, g, b := uint8(v>>11&0x1f), uint8(v>>5&0x3f), uint8(v&0x1f)
		img.SetNRGBA(i%s.Width, i/s.Width, color.NRGBA{R: r<<3 | r>>2, G: g<<2 | g>>4, B: b<<3 | b>>2, A: 0xff})
	}
	return img
}

func runScreenshot(e env, args []string) int {
	fs, asJSON := newFlags(e, "screenshot")
	addrFlag := fs.String("device", "", "device address (default: the paired device)")
	out := fs.String("o", "screenshot.png", "PNG file to write")
	if _, err := parse(fs, args); err != nil {
		return exitUsage
	}
	client, d := pickDevice(*addrFlag)
	if d != nil {
		return e.finish(*asJSON, "screenshot", false, []check.Diagnostic{*d}, nil, "")
	}
	screen, err := client.Screenshot()
	if err != nil {
		return e.finish(*asJSON, "screenshot", false, []check.Diagnostic{deviceDiagnostic(err)}, nil, "")
	}
	f, err := os.Create(*out)
	if err == nil {
		err = png.Encode(f, toImage(screen))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		return e.finish(*asJSON, "screenshot", false, []check.Diagnostic{{Severity: check.Error, Code: "WRITE_FAILED",
			Path: *out, Message: err.Error()}}, nil, "")
	}
	return e.finish(*asJSON, "screenshot", true, nil,
		result{"device": client.Address, "file": *out, "width": screen.Width, "height": screen.Height},
		fmt.Sprintf("wrote %s (%dx%d)", *out, screen.Width, screen.Height))
}

func runPress(e env, args []string) int {
	fs, asJSON := newFlags(e, "press")
	addrFlag := fs.String("device", "", "device address (default: the paired device)")
	long := fs.Bool("long", false, "a long press (LEFT long press leaves the plugin)")
	pos, err := parse(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(pos) != 1 || (pos[0] != "left" && pos[0] != "right") {
		return usageError(e, *asJSON, "press", "usage: press left|right [--long]")
	}
	client, d := pickDevice(*addrFlag)
	if d != nil {
		return e.finish(*asJSON, "press", false, []check.Diagnostic{*d}, nil, "")
	}
	if err := client.Press(pos[0], *long); err != nil {
		return e.finish(*asJSON, "press", false, []check.Diagnostic{deviceDiagnostic(err)}, nil, "")
	}
	what := pos[0]
	if *long {
		what += " (long)"
	}
	return e.finish(*asJSON, "press", true, nil, result{"device": client.Address, "button": pos[0], "long": *long},
		"pressed "+what)
}

func runLogs(e env, args []string) int {
	fs, asJSON := newFlags(e, "logs")
	addrFlag := fs.String("device", "", "device address (default: the paired device)")
	follow := fs.Bool("follow", false, "keep printing new lines until interrupted")
	id := fs.String("id", "", "plugin id (default: the id in <dir>/plugin.json)")
	pos, err := parse(fs, args)
	if err != nil {
		return exitUsage
	}
	dir, err := oneDir(pos)
	if err != nil {
		return usageError(e, *asJSON, "logs", err.Error())
	}
	if *follow && *asJSON {
		return usageError(e, *asJSON, "logs", "--follow prints text as it arrives; use it without --json")
	}
	pluginID := *id
	if pluginID == "" {
		r := check.Project(dir, "", check.Options{SkipUI: true, AllowReservedID: true})
		if r.Manifest == nil || r.Manifest.ID == "" {
			return e.finish(*asJSON, "logs", false, r.Diagnostics, nil, "")
		}
		pluginID = r.Manifest.ID
	}
	client, d := pickDevice(*addrFlag)
	if d != nil {
		return e.finish(*asJSON, "logs", false, []check.Diagnostic{*d}, nil, "")
	}
	var offset int64
	for {
		l, err := client.Logs(pluginID, offset)
		if err != nil {
			return e.finish(*asJSON, "logs", false, []check.Diagnostic{deviceDiagnostic(err)}, nil, "")
		}
		if !*follow {
			return e.finish(*asJSON, "logs", true, nil,
				result{"device": client.Address, "plugin_id": pluginID, "text": l.Text, "next_offset": l.NextOffset},
				strings.TrimSuffix(l.Text, "\n"))
		}
		if l.Restarted {
			fmt.Fprintln(e.stdout, "--- new run ---")
		}
		fmt.Fprint(e.stdout, l.Text)
		offset = l.NextOffset
		if l.NextOffset >= l.Size {
			time.Sleep(pollInterval)
		}
	}
}
