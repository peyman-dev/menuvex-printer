package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadCreatesDefaultWithToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Host != DefaultHost || cfg.Port != DefaultPort {
		t.Fatalf("bad defaults: %+v", cfg)
	}
	if len(cfg.Token) != 64 {
		t.Fatalf("expected 64-char hex token, got %q", cfg.Token)
	}
	if len(cfg.TrustedOrigins) == 0 {
		t.Fatal("expected default trusted origins")
	}
	// Second load returns the same token (persisted).
	cfg2, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg2.Token != cfg.Token {
		t.Fatal("token not persisted")
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("config perm = %o, want 600", st.Mode().Perm())
		}
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := DefaultConfig()
	cfg.Token = "abc123"
	cfg.Printers = []NetworkPrinterConfig{{Name: "OCOM", Address: "192.168.1.50", Port: 9100}}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Printers) != 1 || loaded.Printers[0].Name != "OCOM" {
		t.Fatalf("printers not round-tripped: %+v", loaded.Printers)
	}
	if loaded.Token != "abc123" {
		t.Fatalf("token changed: %q", loaded.Token)
	}
}

func TestValidateErrors(t *testing.T) {
	good := DefaultConfig()
	good.Token = "x"
	if err := good.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	cases := []func(*Config){
		func(c *Config) { c.Host = "" },
		func(c *Config) { c.Host = "not-an-ip" },
		func(c *Config) { c.Port = 0 },
		func(c *Config) { c.Port = 70000 },
		func(c *Config) { c.Token = "" },
		func(c *Config) { c.ConnectTimeoutMs = 0 },
		func(c *Config) { c.Printers = []NetworkPrinterConfig{{Address: "", Port: 9100}} },
		func(c *Config) { c.Printers = []NetworkPrinterConfig{{Address: "1.2.3.4", Port: 0}} },
	}
	for i, mutate := range cases {
		c := DefaultConfig()
		c.Token = "x"
		mutate(c)
		if err := c.Validate(); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
	// Non-loopback bind is allowed (with a runtime warning), not a config error.
	lan := DefaultConfig()
	lan.Token = "x"
	lan.Host = "192.168.1.5"
	if err := lan.Validate(); err != nil {
		t.Fatalf("LAN bind should validate: %v", err)
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestDefaultPath(t *testing.T) {
	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	if filepath.Base(path) != "config.json" {
		t.Fatalf("unexpected path %s", path)
	}
	t.Logf("default path: %s", path)
}

func TestGenerateTokenUnique(t *testing.T) {
	a, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if a == b || len(a) != 64 {
		t.Fatalf("tokens not unique/64 chars: %q %q", a, b)
	}
}
