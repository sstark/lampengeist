package config

import (
	"encoding/json"
	"os"

	"lampengeist/internal/schedules"
)

type Config struct {
	BridgeIP           string          `json:"bridge_ip"`
	Token              string          `json:"token"`
	Enabled            bool            `json:"enabled"`
	SimulationLights   map[string]bool `json:"simulation_lights,omitempty"`
	SimulationSpeed    int             `json:"simulation_speed,omitempty"`
	SimulationSchedule string          `json:"simulation_schedule,omitempty"`
}

const (
	MinSimulationSpeed = 1
	MaxSimulationSpeed = 1440
)

// ClampSimulationSpeed keeps a speed factor in the supported 1x-1440x range.
func ClampSimulationSpeed(speed int) int {
	if speed < MinSimulationSpeed {
		return MinSimulationSpeed
	}
	if speed > MaxSimulationSpeed {
		return MaxSimulationSpeed
	}
	return speed
}

// IsLightInSimulation reports whether a light takes part in the simulation.
// Lights without an explicit setting are enabled by default.
func IsLightInSimulation(settings map[string]bool, lightID string) bool {
	enabled, ok := settings[lightID]
	return !ok || enabled
}

func LoadConfig() Config {
	cfg := Config{
		Enabled:            true,
		SimulationSpeed:    MinSimulationSpeed,
		SimulationSchedule: schedules.DefaultID,
	}

	file, err := os.ReadFile("config.json")
	if err != nil {
		return cfg
	}

	if err := json.Unmarshal(file, &cfg); err != nil {
		return Config{
			Enabled:            true,
			SimulationSpeed:    MinSimulationSpeed,
			SimulationSchedule: schedules.DefaultID,
		}
	}

	cfg.SimulationSpeed = ClampSimulationSpeed(cfg.SimulationSpeed)
	cfg.SimulationSchedule = schedules.Normalize(cfg.SimulationSchedule)

	return cfg
}
