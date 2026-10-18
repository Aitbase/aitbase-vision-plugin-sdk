package device

import (
	"path/filepath"
	"testing"
)

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"192.168.1.50":         "192.168.1.50",
		" 192.168.1.50 ":       "192.168.1.50",
		"http://192.168.1.50/": "192.168.1.50",
		"192.168.1.50:8080":    "192.168.1.50:8080",
		"camera.local":         "camera.local",
	} {
		if got, err := Normalize(in); err != nil || got != want {
			t.Errorf("Normalize(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "https://192.168.1.50", "192.168.1.50/api", "user@host", ":80"} {
		if _, err := Normalize(bad); err == nil {
			t.Errorf("Normalize(%q) accepted", bad)
		}
	}
}

func TestStorePick(t *testing.T) {
	t.Setenv(EnvDevices, filepath.Join(t.TempDir(), "devices.json"))
	s, path, err := LoadStore()
	if err != nil || len(s.Devices) != 0 {
		t.Fatalf("empty store: %v %v", s, err)
	}
	if _, _, err := s.Pick(""); err == nil {
		t.Fatal("no device paired, but Pick found one")
	}
	s.Devices["10.0.0.2"] = Entry{Token: "a"}
	if addr, _, err := s.Pick(""); err != nil || addr != "10.0.0.2" {
		t.Fatalf("the only device: %q %v", addr, err)
	}
	s.Devices["10.0.0.3"] = Entry{Token: "b"}
	if _, _, err := s.Pick(""); err == nil {
		t.Fatal("two devices and no default, but Pick chose one")
	}
	s.Default = "10.0.0.3"
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	s2, _, _ := LoadStore()
	if addr, e, err := s2.Pick(""); err != nil || addr != "10.0.0.3" || e.Token != "b" {
		t.Fatalf("default after reload: %q %v %v", addr, e, err)
	}
	if _, e, err := s2.Pick("http://10.0.0.2"); err != nil || e.Token != "a" {
		t.Fatalf("--device: %v %v", e, err)
	}
	if _, _, err := s2.Pick("10.0.0.9"); err == nil {
		t.Fatal("an unpaired --device was accepted")
	}
}
