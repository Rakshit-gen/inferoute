package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// load writes body to a temp file and loads it.
func load(t *testing.T, body string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

const oneBackend = `"backends":[{"name":"a","url":"http://a:1","models":["m"]}]`

func wantErr(t *testing.T, body, substr string) {
	t.Helper()
	_, err := load(t, body)
	if err == nil || !strings.Contains(err.Error(), substr) {
		t.Fatalf("Load error = %v, want one containing %q", err, substr)
	}
}

func TestDefaults(t *testing.T) {
	cfg, err := load(t, `{`+oneBackend+`}`)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != ":8081" || cfg.HealthCheckPath != "/" || cfg.LoadBalancing != "round_robin" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if time.Duration(cfg.HealthCheckInterval) != 10*time.Second {
		t.Fatalf("HealthCheckInterval = %v", cfg.HealthCheckInterval)
	}
}

func TestRejectsUnknownField(t *testing.T) {
	wantErr(t, `{"api_key":["k"],`+oneBackend+`}`, "api_key")
}

func TestRejectsTrailingData(t *testing.T) {
	wantErr(t, `{`+oneBackend+`} {}`, "unexpected data")
}

func TestBackendErrorOmitsAPIKey(t *testing.T) {
	_, err := load(t, `{"backends":[{"name":"a","api_key":"sk-secret"}]}`)
	if err == nil || strings.Contains(err.Error(), "sk-secret") {
		t.Fatalf("error must exist and omit the key, got %v", err)
	}
}
