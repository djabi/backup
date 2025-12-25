package backup

import (
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Store string `toml:"store"`
	Name  string `toml:"name"`
}

func LoadConfig(path string) (*Config, error) {
	var config Config
	if _, err := toml.DecodeFile(path, &config); err != nil {
		if os.IsNotExist(err) {
			// Return empty config if file doesn't exist?
			// Or return error? The old code returned error if config load failed,
			// but handled "IsNotExist" before calling load?
			// loadProperties returned wrapped error.
			// Let's return error and let caller handle.
			return nil, err
		}
		return nil, err
	}
	return &config, nil
}
