package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOpenRouterKeyFromEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := "ai:\n  providers:\n    openrouter:\n      api_key: \"\"\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MATHTRAINER_AI_PROVIDERS_OPENROUTER_API_KEY", "sk-test-value")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.AI.Providers["openrouter"].APIKey; got != "sk-test-value" {
		t.Fatalf("openrouter api key = %q, want %q", got, "sk-test-value")
	}
}
