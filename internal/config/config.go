// Package config loads, validates and persists the agent configuration.
//
// The config file lives in the OS-specific user config directory:
//
//	Windows: %APPDATA%\NovexPrinterAgent\config.json
//	macOS:   ~/Library/Application Support/NovexPrinterAgent/config.json
//	Linux:   ~/.config/novex-printer-agent/config.json ($XDG_CONFIG_HOME aware)
//
// On first run a random API token is generated and stored in the file.
// The file is created with 0600 permissions because it contains the token.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
)

// Defaults.
const (
	DefaultHost             = "127.0.0.1"
	DefaultPort             = 8765
	DefaultConnectTimeoutMs = 5000
	DefaultWriteTimeoutMs   = 10000
	DefaultScanTimeoutMs    = 8000
	DefaultScanPort         = 9100
)

// DefaultTrustedOrigins is the initial CORS allow-list.
var DefaultTrustedOrigins = []string{
	"https://menuvex.ir",
	"http://localhost:3000",
	"http://localhost:3001",
}

// NetworkPrinterConfig is a user-registered LAN/TCP printer.
type NetworkPrinterConfig struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Port    int    `json:"port"`
}

// Config is the full agent configuration.
type Config struct {
	Host             string                 `json:"host"`
	Port             int                    `json:"port"`
	Token            string                 `json:"token"`
	TrustedOrigins   []string               `json:"trustedOrigins"`
	Printers         []NetworkPrinterConfig `json:"printers"`
	ConnectTimeoutMs int                    `json:"connectTimeoutMs"`
	WriteTimeoutMs   int                    `json:"writeTimeoutMs"`
	ScanTimeoutMs    int                    `json:"scanTimeoutMs"`
}

// DefaultConfig returns a Config with sensible defaults and an empty token.
// Call EnsureToken or Load to populate the token.
func DefaultConfig() *Config {
	return &Config{
		Host:             DefaultHost,
		Port:             DefaultPort,
		TrustedOrigins:   append([]string(nil), DefaultTrustedOrigins...),
		Printers:         []NetworkPrinterConfig{},
		ConnectTimeoutMs: DefaultConnectTimeoutMs,
		WriteTimeoutMs:   DefaultWriteTimeoutMs,
		ScanTimeoutMs:    DefaultScanTimeoutMs,
	}
}

// Addr returns "host:port" for net.Listen / dialing.
func (c *Config) Addr() string {
	return net.JoinHostPort(c.Host, fmt.Sprint(c.Port))
}

// Validate checks the configuration values.
func (c *Config) Validate() error {
	if c.Host == "" {
		return errors.New("host must not be empty")
	}
	if ip := net.ParseIP(c.Host); ip == nil {
		return fmt.Errorf("host %q is not a valid IP address", c.Host)
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port %d out of range (1-65535)", c.Port)
	}
	if c.Token == "" {
		return errors.New("token must not be empty")
	}
	if c.ConnectTimeoutMs <= 0 || c.WriteTimeoutMs <= 0 || c.ScanTimeoutMs <= 0 {
		return errors.New("timeouts must be positive")
	}
	for i, p := range c.Printers {
		if p.Address == "" {
			return fmt.Errorf("printers[%d]: address must not be empty", i)
		}
		if p.Port < 1 || p.Port > 65535 {
			return fmt.Errorf("printers[%d]: port %d out of range", i, p.Port)
		}
	}
	return nil
}

// GenerateToken creates a random 256-bit hex token.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// DefaultPath returns the OS-specific default config file path.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	// os.UserConfigDir already handles all three platforms:
	//   Windows: %APPDATA%   macOS: ~/Library/Application Support   Linux: ~/.config
	name := "NovexPrinterAgent"
	if runtime.GOOS == "linux" {
		name = "novex-printer-agent"
	}
	return filepath.Join(dir, name, "config.json"), nil
}

// Load reads the config from path. If the file does not exist, a default
// config with a freshly generated token is written and returned.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		cfg := DefaultConfig()
		token, err := GenerateToken()
		if err != nil {
			return nil, err
		}
		cfg.Token = token
		if err := cfg.Save(path); err != nil {
			return nil, err
		}
		return cfg, nil
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("invalid config file %s: %w", path, err)
	}
	// Backfill defaults for missing/zero fields so old configs keep working.
	if cfg.Host == "" {
		cfg.Host = DefaultHost
	}
	if cfg.Port == 0 {
		cfg.Port = DefaultPort
	}
	if cfg.ConnectTimeoutMs == 0 {
		cfg.ConnectTimeoutMs = DefaultConnectTimeoutMs
	}
	if cfg.WriteTimeoutMs == 0 {
		cfg.WriteTimeoutMs = DefaultWriteTimeoutMs
	}
	if cfg.ScanTimeoutMs == 0 {
		cfg.ScanTimeoutMs = DefaultScanTimeoutMs
	}
	if cfg.Token == "" {
		token, err := GenerateToken()
		if err != nil {
			return nil, err
		}
		cfg.Token = token
		if err := cfg.Save(path); err != nil {
			return nil, err
		}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save writes the config to path with 0600 permissions (it holds the token).
func (c *Config) Save(path string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}
