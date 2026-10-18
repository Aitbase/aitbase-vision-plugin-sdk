package bundled

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	aitbasesdk "github.com/Aitbase/aitbase-vision-plugin-sdk"
)

// useTempCache points os.UserCacheDir at a temporary directory on every system.
func useTempCache(t *testing.T) string {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("LocalAppData", dir)
	t.Setenv("HOME", dir)
	return dir
}

func TestDirUnpacksOnceAndBuilds(t *testing.T) {
	cache := useTempCache(t)
	root, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(root, cache) {
		t.Fatalf("%s is not in the cache %s", root, cache)
	}
	for _, f := range []string{"CMakeLists.txt", "include/aitbase_plugin.h", "cmake/AitbasePluginUi.cmake",
		"tools/plugin_ui_compile.py", "src/aitbase_plugin_core.c"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(f))); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	if v, _ := os.ReadFile(filepath.Join(root, "VERSION")); strings.TrimSpace(string(v)) != aitbasesdk.Version {
		t.Errorf("VERSION %q", v)
	}
	// A second call reuses the directory, even after a file in it changed.
	os.WriteFile(filepath.Join(root, "CMakeLists.txt"), []byte("# changed"), 0o644)
	again, err := Dir()
	if err != nil || again != root {
		t.Fatalf("second Dir = %q, %v", again, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(root))
	if len(entries) != 1 {
		t.Fatalf("staging left behind: %v", entries)
	}
}
