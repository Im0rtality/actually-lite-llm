package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(f, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestLoad_Valid(t *testing.T) {
	f := writeConfig(t, `
listen: ":9090"
providers:
  openai:
    api_key: "sk-test"
api_keys:
  - key: "sk-validkey-1234567"
    app: "myapp"
    allowed_models: ["*"]
`)
	cfg, err := Load(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Listen != ":9090" {
		t.Errorf("expected :9090, got %s", cfg.Listen)
	}
}

func TestLoad_DefaultListen(t *testing.T) {
	f := writeConfig(t, `
api_keys:
  - key: "sk-validkey-1234567"
    app: "myapp"
    allowed_models: ["*"]
`)
	cfg, err := Load(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Listen != ":8080" {
		t.Errorf("expected :8080, got %s", cfg.Listen)
	}
}

func TestLoad_EnvExpansion(t *testing.T) {
	t.Setenv("TEST_API_KEY", "sk-from-env")
	f := writeConfig(t, `
providers:
  openai:
    api_key: "${TEST_API_KEY}"
api_keys:
  - key: "sk-validkey-1234567"
    app: "myapp"
    allowed_models: ["*"]
`)
	cfg, err := Load(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Providers["openai"].APIKey != "sk-from-env" {
		t.Errorf("env var not expanded, got %q", cfg.Providers["openai"].APIKey)
	}
}

func TestLoad_EmptyKeyRejected(t *testing.T) {
	f := writeConfig(t, `
api_keys:
  - key: ""
    app: "myapp"
    allowed_models: ["*"]
`)
	_, err := Load(f)
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestLoad_ShortKeyRejected(t *testing.T) {
	f := writeConfig(t, `
api_keys:
  - key: "tooshort"
    app: "myapp"
    allowed_models: ["*"]
`)
	_, err := Load(f)
	if err == nil {
		t.Fatal("expected error for key shorter than minKeyLength")
	}
}

func TestLoad_EnvExpandedEmptyKeyRejected(t *testing.T) {
	t.Setenv("MISSING_VKEY", "")
	f := writeConfig(t, `
api_keys:
  - key: "${MISSING_VKEY}"
    app: "myapp"
    allowed_models: ["*"]
`)
	_, err := Load(f)
	if err == nil {
		t.Fatal("expected error when env var expands to empty key")
	}
}

func TestLoad_MissingAppRejected(t *testing.T) {
	f := writeConfig(t, `
api_keys:
  - key: "sk-validkey-1234567"
    app: ""
    allowed_models: ["*"]
`)
	_, err := Load(f)
	if err == nil {
		t.Fatal("expected error for missing app")
	}
}

func TestLoad_ModelMissingProvider(t *testing.T) {
	f := writeConfig(t, `
models:
  gpt-4o:
    provider: ""
    model: "gpt-4o"
api_keys:
  - key: "sk-validkey-1234567"
    app: "myapp"
    allowed_models: ["*"]
`)
	_, err := Load(f)
	if err == nil {
		t.Fatal("expected error for missing model provider")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	f := writeConfig(t, `{not valid yaml: [`)
	_, err := Load(f)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}
