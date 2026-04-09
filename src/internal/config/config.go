package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds values from a .smokorc file.
type Config struct {
	Image   string `toml:"image"`
	Timeout int    `toml:"timeout"` // seconds per setup/action command; 0 means use default
}

const DefaultTimeout = 30

// Load reads .smokorc from dir. If the file is absent it returns a zero-value
// Config (not an error).
func Load(dir string) (Config, error) {
	path := filepath.Join(dir, ".smokorc")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, err
	}
	var cfg Config
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
