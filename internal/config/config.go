package config

import (
	"encoding/json"
	"flag"
	"os"
)

type Config struct {
	TelegramToken string `json:"telegram_token"`
	DatabaseDSN   string `json:"database_dsn"`
	SaluteAuthKey string `json:"salute_auth_key"`
	GigaChatAuthKey string `json:"gigachat_auth_key"`
}

func Load() (*Config, error) {
	var configPath string
	flag.StringVar(&configPath, "c", "config.json", "Path to config file")
	flag.Parse()

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
