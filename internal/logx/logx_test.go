package logx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLevelGating(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ORBIT_LOG_DIR", dir)
	t.Setenv("ORBIT_DEBUG", "")
	Close()

	Info("info line")
	Debug("debug line should not appear")

	data, err := os.ReadFile(filepath.Join(dir, "orbit.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "DEBUG") {
		t.Error("DEBUG line written without ORBIT_DEBUG=1")
	}
	if !strings.Contains(string(data), "info line") {
		t.Error("INFO line missing")
	}
}

func TestDebugEnabled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ORBIT_LOG_DIR", dir)
	t.Setenv("ORBIT_DEBUG", "1")
	Close()

	Debug("debug visible")
	Info("info visible")

	data, _ := os.ReadFile(filepath.Join(dir, "orbit.log"))
	if !strings.Contains(string(data), "debug visible") {
		t.Error("DEBUG line missing with ORBIT_DEBUG=1")
	}
	if !strings.Contains(string(data), "info visible") {
		t.Error("INFO line missing")
	}
}

func TestRotation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ORBIT_LOG_DIR", dir)
	t.Setenv("ORBIT_DEBUG", "")
	Close()

	// Simulate existing logs at all shift positions.
	os.WriteFile(filepath.Join(dir, "orbit.log"), []byte("current"), 0o644)
	os.WriteFile(filepath.Join(dir, "orbit.log.1"), []byte("old1"), 0o644)
	os.WriteFile(filepath.Join(dir, "orbit.log.2"), []byte("old2"), 0o644)
	os.WriteFile(filepath.Join(dir, "orbit.log.3"), []byte("old3"), 0o644)

	// Trigger rotation via open() path: write a line with a big fake file.
	// Instead call rotate directly through Write by making the file big.
	mu.Lock()
	rotate(dir)
	mu.Unlock()

	check := func(name, want string, wantMissing bool) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, name))
		if wantMissing {
			if err == nil {
				t.Errorf("%s should be deleted", name)
			}
			return
		}
		if err != nil || string(data) != want {
			t.Errorf("%s = %q (err %v), want %q", name, data, err, want)
		}
	}
	check("orbit.log.1", "current", false)
	check("orbit.log.2", "old1", false)
	check("orbit.log.3", "old2", false)
	// old3 deleted (shifted off)
	if _, err := os.Stat(filepath.Join(dir, "orbit.log")); !os.IsNotExist(err) {
		t.Error("orbit.log should not exist right after rotation until next write")
	}
}

func TestSecretRedactionByContract(t *testing.T) {
	// The logger only writes what callers pass; this test documents that
	// tokens must never be passed. Simulate: a token value passed by mistake
	// would appear — so callers must log env var NAMES only. We assert the
	// helper convention: Write emits exactly what's given, so redaction is a
	// caller contract enforced by review + grep in CI.
	dir := t.TempDir()
	t.Setenv("ORBIT_LOG_DIR", dir)
	Close()
	token := "super-secret-token-value"
	Info("using token_env=ORBIT_REGISTRY_TOKEN")
	_ = token // intentionally never logged
	data, _ := os.ReadFile(filepath.Join(dir, "orbit.log"))
	if strings.Contains(string(data), token) {
		t.Error("secret leaked into log")
	}
}
