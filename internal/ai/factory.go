package ai

import (
	"log"

	"mathtrainer/internal/config"
)

// BuildChain assembles the provider chain from cfg, respecting the
// fallback_order. Providers with no API key or base_url are skipped with a
// warning so the server still starts even if some keys are missing.
func BuildChain(cfg config.AIConfig) *Chain {
	builders := map[string]func(config.AIProvider) Provider{
		"anthropic": func(p config.AIProvider) Provider {
			if p.APIKey == "" {
				log.Printf("[ai] anthropic: api_key not set, skipping")
				return nil
			}
			return NewAnthropic(p.APIKey, p.Model, p.BaseURL)
		},
		"openai": func(p config.AIProvider) Provider {
			if p.APIKey == "" {
				log.Printf("[ai] openai: api_key not set, skipping")
				return nil
			}
			return NewOpenAI(p.APIKey, p.Model, p.BaseURL)
		},
		"gemini": func(p config.AIProvider) Provider {
			if p.APIKey == "" {
				log.Printf("[ai] gemini: api_key not set, skipping")
				return nil
			}
			return NewGemini(p.APIKey, p.Model, p.BaseURL)
		},
		"ollama": func(p config.AIProvider) Provider {
			if p.BaseURL == "" && p.Model == "" {
				log.Printf("[ai] ollama: base_url/model not set, skipping")
				return nil
			}
			return NewOllama(p.BaseURL, p.Model, p.VisionModel)
		},
		"lmstudio": func(p config.AIProvider) Provider {
			if p.BaseURL == "" {
				log.Printf("[ai] lmstudio: base_url not set, skipping")
				return nil
			}
			return NewLMStudio(p.APIKey, p.Model, p.BaseURL)
		},
	}

	var providers []Provider
	for _, name := range cfg.FallbackOrder {
		build, ok := builders[name]
		if !ok {
			log.Printf("[ai] unknown provider %q in fallback_order, skipping", name)
			continue
		}
		pcfg := cfg.Providers[name]
		p := build(pcfg)
		if p != nil {
			providers = append(providers, p)
			log.Printf("[ai] registered provider: %s", name)
		}
	}
	if len(providers) == 0 {
		log.Printf("[ai] WARNING: no AI providers configured — AI features will return errors")
	}
	return NewChain(providers)
}
