package aicommits

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/ini.v1"
)

// Config holds the parsed ~/.aicommits configuration.
type Config struct {
	OpenAIAPIKey  string
	OpenAIBaseURL string
	OpenAIModel   string
	Locale        string
	CommitType    string
	Timeout       time.Duration
	MaxLength     int
	Generate      int
}

// LoadConfig reads ~/.aicommits and returns a Config with sensible defaults.
func LoadConfig() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cfgPath := filepath.Join(home, ".aicommits")

	cfg := &Config{
		Locale:     "en",
		CommitType: "conventional",
		Timeout:    60 * time.Second,
		MaxLength:  72,
		Generate:   1,
	}

	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		return cfg, nil
	}

	f, err := ini.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	sec := f.Section("")
	strKey := func(name string) string {
		if k, err := sec.GetKey(name); err == nil {
			return k.String()
		}
		return ""
	}

	if v := strKey("OPENAI_API_KEY"); v != "" {
		cfg.OpenAIAPIKey = v
	}
	if v := strKey("OPENAI_BASE_URL"); v != "" {
		cfg.OpenAIBaseURL = v
	}
	if v := strKey("OPENAI_MODEL"); v != "" {
		cfg.OpenAIModel = v
	}
	if v := strKey("locale"); v != "" {
		cfg.Locale = v
	}
	if v := strKey("type"); v != "" {
		cfg.CommitType = v
	}
	if k, err := sec.GetKey("timeout"); err == nil {
		if ms, err := k.Int64(); err == nil && ms >= 500 {
			cfg.Timeout = time.Duration(ms) * time.Millisecond
		}
	}
	if k, err := sec.GetKey("max-length"); err == nil {
		if n, err := k.Int(); err == nil && n >= 20 {
			cfg.MaxLength = n
		}
	}
	if k, err := sec.GetKey("generate"); err == nil {
		if n, err := k.Int(); err == nil && n >= 1 {
			cfg.Generate = n
		}
	}

	return cfg, nil
}
