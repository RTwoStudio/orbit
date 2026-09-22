package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLayeringPrecedence(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yml")
	os.WriteFile(cfgFile, []byte("registry:\n  url: https://github.com/acme/reg\n  ref: dev\nopencode:\n  dir: /custom/opencode\n"), 0o644)

	os.Setenv("ORBIT_CONFIG", cfgFile)
	defer os.Unsetenv("ORBIT_CONFIG")
	os.Setenv("ORBIT_REGISTRY_URL", "https://env-wins.example/reg")
	defer os.Unsetenv("ORBIT_REGISTRY_URL")

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Registry.URL != "https://env-wins.example/reg" {
		t.Errorf("env should win: got %q", cfg.Registry.URL)
	}
	if cfg.Registry.Ref != "dev" {
		t.Errorf("user ref: got %q", cfg.Registry.Ref)
	}
	if cfg.OpenCode.Dir != "/custom/opencode" {
		t.Errorf("user opencode dir: got %q", cfg.OpenCode.Dir)
	}
}

func TestMissingFileIsNotError(t *testing.T) {
	t.Setenv("ORBIT_CONFIG", filepath.Join(t.TempDir(), "nope.yml"))
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("missing config should not error: %v", err)
	}
	if cfg.OpenCode.Dir == "" {
		t.Error("defaults should apply")
	}
}

func TestBadTypeIsError(t *testing.T) {
	f := filepath.Join(t.TempDir(), "config.yml")
	os.WriteFile(f, []byte("registry: not-a-map\n"), 0o644)
	t.Setenv("ORBIT_CONFIG", f)
	_, err := Load("")
	if err == nil {
		t.Fatal("bad types should error")
	}
}

func TestHomeExpansion(t *testing.T) {
	t.Setenv("ORBIT_CONFIG", filepath.Join(t.TempDir(), "nope.yml"))
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(cfg.OpenCode.Dir, "~") {
		t.Errorf("~ should expand: %q", cfg.OpenCode.Dir)
	}
	if cfg.NeoCortex.Dir != ".neocortex" {
		t.Errorf("relative path preserved: %q", cfg.NeoCortex.Dir)
	}
}
