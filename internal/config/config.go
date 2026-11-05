package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	BridgeIP         string          `json:"bridge_ip"`
	Token            string          `json:"token"`
	Enabled          bool            `json:"enabled"`
	SimulationLights map[string]bool `json:"simulation_lights,omitempty"`
}

// IsLightInSimulation reports whether a light takes part in the simulation.
// Lights without an explicit setting are enabled by default.
func IsLightInSimulation(settings map[string]bool, lightID string) bool {
	enabled, ok := settings[lightID]
	return !ok || enabled
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
