package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigUsesDefaultsAndEnvOverrides(t *testing.T) {
	t.Setenv("CONFIG_CENTER_SERVER_PORT", "9090")
	t.Setenv("CONFIG_CENTER_DB_HOST", "mysql")
	t.Setenv("CONFIG_CENTER_DB_PASSWORD", "secret")
	t.Setenv("CONFIG_CENTER_JWT_SECRET", "jwt-secret")

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Fatalf("Server.Port = %d, want 9090", cfg.Server.Port)
	}
	if cfg.Database.Host != "mysql" {
		t.Fatalf("Database.Host = %q, want mysql", cfg.Database.Host)
	}
	if cfg.Database.Password != "secret" {
		t.Fatalf("Database.Password = %q, want secret", cfg.Database.Password)
	}
	if cfg.JWT.Secret != "jwt-secret" {
		t.Fatalf("JWT.Secret = %q, want jwt-secret", cfg.JWT.Secret)
	}
}

func TestLoadConfigFileThenEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := []byte(`server:
  port: 8081
database:
  host: db.example
  port: 3307
  user: app
  password: from-file
  name: app_db
jwt:
  secret: from-file-secret
  expires: 3600
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	t.Setenv("CONFIG_CENTER_DB_PASSWORD", "from-env")
	t.Setenv("CONFIG_CENTER_JWT_EXPIRES", "7200")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.Server.Port != 8081 {
		t.Fatalf("Server.Port = %d, want 8081", cfg.Server.Port)
	}
	if cfg.Database.Password != "from-env" {
		t.Fatalf("Database.Password = %q, want from-env", cfg.Database.Password)
	}
	if cfg.JWT.Expires != 7200 {
		t.Fatalf("JWT.Expires = %d, want 7200", cfg.JWT.Expires)
	}
}
