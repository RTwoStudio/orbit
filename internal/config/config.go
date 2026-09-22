// Package config implements orbit's configuration layering:
// embedded defaults < user config.yml < environment variables.
package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/orbit-sh/orbit-cli/internal/exit"
	"gopkg.in/yaml.v3"
)

//go:embed defaults.yml
var defaultsYAML []byte

// Registry holds registry access settings.
type Registry struct {
	URL      string `yaml:"url"`
	Ref      string `yaml:"ref"`
	TokenEnv string `yaml:"token_env"`
}

// OpenCode holds the opencode deployment dir.
type OpenCode struct {
	Dir string `yaml:"dir"`
}

// NeoCortex holds project-side paths.
type NeoCortex struct {
	Dir string `yaml:"dir"`
}

// Vault is reserved for v0.2.0.
type Vault struct {
	Dir string `yaml:"dir"`
}

// UI holds display settings.
type UI struct {
	Color string `yaml:"color"`
}

// Config is the fully-resolved orbit configuration.
type Config struct {
	Registry  Registry  `yaml:"registry"`
	OpenCode  OpenCode  `yaml:"opencode"`
	NeoCortex NeoCortex `yaml:"neocortex"`
	Vault     Vault     `yaml:"vault"`
	UI        UI        `yaml:"ui"`

	// Source file the user layer came from ("" if none).
	SourceFile string `yaml:"-"`
}

// UserPath returns the user config path, honoring --config and ORBIT_CONFIG.
// Without overrides: an existing config.yml wins; otherwise an existing
// config.json (JSON is valid YAML) is used; otherwise config.yml is the
// canonical default path.
func UserPath(flagOverride string) string {
	if flagOverride != "" {
		return flagOverride
	}
	if p := os.Getenv("ORBIT_CONFIG"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".config", "orbit")
	if _, err := os.Stat(filepath.Join(base, "config.yml")); err == nil {
		return filepath.Join(base, "config.yml")
	}
	if _, err := os.Stat(filepath.Join(base, "config.json")); err == nil {
		return filepath.Join(base, "config.json")
	}
	return filepath.Join(base, "config.yml")
}

// Load resolves the layered configuration. missingFile is not an error;
// bad types are. Returns a config even when no user file exists.
func Load(flagOverride string) (*Config, error) {
	cfg := &Config{}
	if err := yaml.Unmarshal(defaultsYAML, cfg); err != nil {
		return nil, exit.New(exit.General, "embedded defaults are invalid: "+err.Error())
	}

	userPath := UserPath(flagOverride)
	if data, err := os.ReadFile(userPath); err == nil {
		cfg.SourceFile = userPath
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, exit.New(exit.ConfigError,
				fmt.Sprintf("invalid config at %s: %v", userPath, err),
				"fix the YAML types in "+userPath)
		}
		// Warn about unknown top-level keys.
		var raw map[string]any
		if err := yaml.Unmarshal(data, &raw); err == nil {
			for k := range raw {
				switch k {
				case "registry", "opencode", "neocortex", "vault", "ui":
				default:
					fmt.Fprintf(os.Stderr, "warning: unknown config key %q in %s\n", k, userPath)
				}
			}
		}
	}

	// Environment layer (highest).
	if v := os.Getenv("ORBIT_REGISTRY_URL"); v != "" {
		cfg.Registry.URL = v
	}
	if v := os.Getenv("ORBIT_OPENCODE_DIR"); v != "" {
		cfg.OpenCode.Dir = v
	}
	if cfg.Registry.TokenEnv == "" {
		cfg.Registry.TokenEnv = "ORBIT_REGISTRY_TOKEN"
	}
	if cfg.Registry.Ref == "" {
		cfg.Registry.Ref = "main"
	}

	expand(cfg)
	return cfg, nil
}

// expand applies ~ expansion to all path fields.
func expand(cfg *Config) {
	cfg.OpenCode.Dir = expandPath(cfg.OpenCode.Dir)
	cfg.NeoCortex.Dir = expandPath(cfg.NeoCortex.Dir)
	cfg.Vault.Dir = expandPath(cfg.Vault.Dir)
}

func expandPath(p string) string {
	if p == "" {
		return p
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

// CacheDir returns the global cache dir (~/.config/orbit/neocortex/cache).
func CacheDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "orbit", "neocortex", "cache")
}

// DeployedJSONPath returns the path of deployed.json.
func DeployedJSONPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "orbit", "neocortex", "deployed.json")
}

// RequireRegistryURL returns the registry URL or a config_error.
func (c *Config) RequireRegistryURL() (string, error) {
	if c.Registry.URL == "" {
		return "", exit.New(exit.ConfigError,
			"registry.url is not configured",
			"add to "+UserPath("")+":",
			"registry:\n  url: https://github.com/<org>/orbit-registry\n  ref: main")
	}
	return c.Registry.URL, nil
}
