package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	DefaultHost = "127.0.0.1"
	DefaultPort = 7070
	AppName     = "SMLSignX"
)

// Config holds all runtime settings for the signing service.
type Config struct {
	Host                 string
	Port                 int
	DataDir              string
	APIKey               string
	RequireRegisteredPFX bool
	TSAURL               string
	Headless             bool
}

// Load reads configuration from environment variables and applies defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Host:                 envString("SML_SIGNING_HOST", DefaultHost),
		Port:                 DefaultPort,
		APIKey:               envString("SML_API_KEY", ""),
		RequireRegisteredPFX: !envBool("SML_ALLOW_UNREGISTERED_PFX", false),
		TSAURL:               envString("SML_TSA_URL", ""),
	}

	port, err := envInt("SML_SIGNING_PORT", DefaultPort)
	if err != nil {
		return nil, err
	}
	cfg.Port = port

	dataDir, err := DefaultDataDir()
	if err != nil {
		return nil, err
	}
	cfg.DataDir = envString("SML_DATA_DIR", dataDir)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("invalid port %d: must be between 1 and 65535", c.Port)
	}
	if strings.TrimSpace(c.Host) == "" {
		return fmt.Errorf("host must not be empty")
	}
	if strings.TrimSpace(c.DataDir) == "" {
		return fmt.Errorf("data directory must not be empty")
	}
	return nil
}

func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func (c *Config) BaseURL() string {
	return fmt.Sprintf("http://%s:%d", c.Host, c.Port)
}

func (c *Config) StorePath() string {
	return filepath.Join(c.DataDir, "certs.json")
}

// DefaultDataDir returns the per-user application data directory.
func DefaultDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return "", fmt.Errorf("resolve user config dir: %w", err)
		}
		base = home
	}
	return filepath.Join(base, AppName), nil
}

func envString(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return v, nil
}

func envBool(key string, fallback bool) bool {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback
	}
	v, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return v
}
