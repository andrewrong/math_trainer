package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	DB       DBConfig       `mapstructure:"db"`
	AI       AIConfig       `mapstructure:"ai"`
}

type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

func (s ServerConfig) Addr() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

type DBConfig struct {
	Path string `mapstructure:"path"`
}

type AIConfig struct {
	FallbackOrder []string             `mapstructure:"fallback_order"`
	Providers     map[string]AIProvider `mapstructure:"providers"`
}

type AIProvider struct {
	APIKey      string `mapstructure:"api_key"`
	Model       string `mapstructure:"model"`
	VisionModel string `mapstructure:"vision_model"` // Ollama only
	BaseURL     string `mapstructure:"base_url"`
}

func Load(path string) (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)
	v.SetDefault("db.path", "./math_trainer.db")
	v.SetDefault("ai.fallback_order", []string{"ollama", "anthropic", "openai", "gemini"})

	// Config file
	if path != "" {
		v.SetConfigFile(path)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
		// No config file is fine; use defaults + env
	}

	// Environment variable overrides: MATHTRAINER_SERVER_PORT, etc.
	v.SetEnvPrefix("MATHTRAINER")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Also support bare env vars for common API keys
	for _, pair := range []struct{ env, key string }{
		{"ANTHROPIC_API_KEY", "ai.providers.anthropic.api_key"},
		{"OPENAI_API_KEY", "ai.providers.openai.api_key"},
		{"GEMINI_API_KEY", "ai.providers.gemini.api_key"},
	} {
		if val := v.GetString(pair.env); val != "" {
			v.Set(pair.key, val)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}
	return &cfg, nil
}
