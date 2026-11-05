package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	BridgeIP string `json:"bridge_ip"`
	Token    string `json:"token"`
	Enabled  bool   `json:"enabled"`
}

func LoadConfig() Config {
	cfg := Config{Enabled: true}

	file, err := os.ReadFile("config.json")
	if err != nil {
		return cfg
	}

	if err := json.Unmarshal(file, &cfg); err != nil {
		return Config{Enabled: true}
	}

	return cfg
}
