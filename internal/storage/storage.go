package storage

import (
	"encoding/json"
	"lampengeist/internal/config"
	"os"
)

type Store struct {
	FilePath string
}

func NewStore(path string) *Store {
	return &Store{FilePath: path}
}

func (s *Store) Save(cfg config.Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.FilePath, data, 0644)
}

func (s *Store) Load() (config.Config, error) {
	data, err := os.ReadFile(s.FilePath)
	if err != nil {
		return config.Config{}, err
	}
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}
