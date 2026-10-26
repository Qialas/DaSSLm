package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DASSLM_CONFIG", path)
	return path
}

const baseConfig = `server:
  port: 8080

# 登录凭证
jwt:
  secret: "" # 留空自动生成
  expireHours: 24
`

func TestEmptySecretIsGeneratedPersistedAndReused(t *testing.T) {
	path := writeConfig(t, baseConfig)

	first, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(first.JWT.Secret) != 64 {
		t.Fatalf("expected generated 64-char secret, got %q", first.JWT.Secret)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	if !strings.Contains(text, `secret: "`+first.JWT.Secret+`" # 留空自动生成`) {
		t.Fatalf("expected secret written back with comment kept, got:\n%s", text)
	}
	if !strings.Contains(text, "# 登录凭证") || !strings.Contains(text, "expireHours: 24") {
		t.Fatalf("expected rest of config untouched, got:\n%s", text)
	}

	second, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if second.JWT.Secret != first.JWT.Secret {
		t.Fatal("expected persisted secret to be reused on next start")
	}
}

func TestCustomSecretIsKept(t *testing.T) {
	path := writeConfig(t, strings.Replace(baseConfig, `secret: ""`, `secret: "my-own-secret-value-0123456789abcdef"`, 1))
	before, _ := os.ReadFile(path)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.JWT.Secret != "my-own-secret-value-0123456789abcdef" {
		t.Fatalf("expected custom secret kept, got %q", cfg.JWT.Secret)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("config file should not be modified when secret is set")
	}
}

func TestLegacyDefaultSecretIsReplaced(t *testing.T) {
	writeConfig(t, strings.Replace(baseConfig, `secret: ""`, `secret: please-change-this-secret`, 1))
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.JWT.Secret == legacyDefaultJWTSecret || len(cfg.JWT.Secret) != 64 {
		t.Fatalf("expected legacy default to be replaced, got %q", cfg.JWT.Secret)
	}
}

func TestMissingSecretKeyOrSection(t *testing.T) {
	cases := map[string]string{
		"no secret key":  "jwt:\n  expireHours: 12\n",
		"empty jwt":      "server:\n  port: 8080\njwt:\n",
		"no jwt section": "server:\n  port: 8080\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeConfig(t, content)
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			again, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if len(cfg.JWT.Secret) != 64 || again.JWT.Secret != cfg.JWT.Secret {
				data, _ := os.ReadFile(path)
				t.Fatalf("expected secret generated and reused, got %q / %q\n%s", cfg.JWT.Secret, again.JWT.Secret, data)
			}
		})
	}
}

func TestReadOnlyConfigFallsBackToMemorySecret(t *testing.T) {
	path := writeConfig(t, baseConfig)
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	if os.Getuid() == 0 {
		t.Skip("root can write read-only files")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.JWT.Secret) != 64 {
		t.Fatalf("expected in-memory secret, got %q", cfg.JWT.Secret)
	}
}
