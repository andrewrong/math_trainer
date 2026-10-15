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

func TestLoadDocumentedPrefixedKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := "ai:\n  providers:\n    openai:\n      api_key: \"\"\nautomation:\n  api_token: \"\"\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MATHTRAINER_AI_PROVIDERS_OPENAI_API_KEY", "dummy-openai-key")
	t.Setenv("MATHTRAINER_AUTOMATION_API_TOKEN", "dummy-token")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.AI.Providers["openai"].APIKey; got != "dummy-openai-key" {
		t.Fatalf("openai API key = %q, want %q", got, "dummy-openai-key")
	}
	if got := cfg.Automation.APIToken; got != "dummy-token" {
		t.Fatalf("automation token = %q, want %q", got, "dummy-token")
	}
}
