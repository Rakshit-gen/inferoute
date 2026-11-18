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

func TestRejectsEmptyAPIKey(t *testing.T) {
	wantErr(t, `{"api_keys":[""],`+oneBackend+`}`, "api_keys[0]")
}

func TestExpandsSecretsFromEnv(t *testing.T) {
	t.Setenv("GW_KEY", "sk-env")
	t.Setenv("UP_KEY", "up-env")
	cfg, err := load(t, `{"api_keys":["${GW_KEY}"],"backends":[{"name":"a","url":"http://a:1","models":["m"],"api_key":"${UP_KEY}"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKeys[0] != "sk-env" || cfg.Backends[0].APIKey != "up-env" {
		t.Fatalf("secrets not expanded: %+v", cfg)
	}
}

func TestUnsetSecretIsAnError(t *testing.T) {
	wantErr(t, `{"api_keys":["${INFEROUTE_TEST_UNSET}"],`+oneBackend+`}`, "INFEROUTE_TEST_UNSET")
}

func TestRejectsBadBackendFields(t *testing.T) {
	for name, b := range map[string]string{
		"no scheme":  `{"name":"a","url":"localhost:11434","models":["m"]}`,
		"ftp":        `{"name":"a","url":"ftp://h","models":["m"]}`,
		"prefix":     `{"name":"a","url":"http://h","models":["m"],"path_prefix":"openai"}`,
		"neg weight": `{"name":"a","url":"http://h","models":["m"],"weight":-1}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := load(t, `{"backends":[`+b+`]}`); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestRejectsDuplicateBackendName(t *testing.T) {
	wantErr(t, `{"backends":[{"name":"a","url":"http://x","models":["m"]},{"name":"a","url":"http://y","models":["n"]}]}`, "two different urls")
}

func TestRejectsBadHealthCheck(t *testing.T) {
	wantErr(t, `{"health_check_interval":"0s",`+oneBackend+`}`, "health_check_interval")
	wantErr(t, `{"health_check_path":"healthz",`+oneBackend+`}`, "health_check_path")
}

func TestRejectsBadListenAddr(t *testing.T) {
	wantErr(t, `{"listen_addr":"8081",`+oneBackend+`}`, "listen_addr")
}
