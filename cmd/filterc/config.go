package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/jfberry/golbat-filterc/filterc"
)

type latLon struct {
	Lat float64 `toml:"lat"`
	Lon float64 `toml:"lon"`
}

// Config is filterc.toml. Flags override it; it overrides the defaults.
type Config struct {
	Listen string `toml:"listen"`
	Golbat struct {
		URL    string `toml:"url"`
		Secret string `toml:"secret"`
	} `toml:"golbat"`
	Bounds struct {
		Min   latLon `toml:"min"`
		Max   latLon `toml:"max"`
		Limit int    `toml:"limit"`
	} `toml:"bounds"`
}

func defaultConfig() Config {
	var c Config
	c.Listen = ":8080"
	return c
}

func (c Config) bounds() filterc.Bounds {
	return filterc.Bounds{
		Min: filterc.LatLon{Lat: c.Bounds.Min.Lat, Lon: c.Bounds.Min.Lon},
		Max: filterc.LatLon{Lat: c.Bounds.Max.Lat, Lon: c.Bounds.Max.Lon},
	}
}

// loadConfig reads path, or with an empty path the first of ./filterc.toml
// and $XDG_CONFIG_HOME/filterc/filterc.toml (default ~/.config) that exists.
// An explicit path must exist; a missing default is not an error.
func loadConfig(path string) (Config, error) {
	cfg := defaultConfig()
	explicit := path != ""
	if !explicit {
		candidates := []string{"filterc.toml"}
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			candidates = append(candidates, filepath.Join(xdg, "filterc", "filterc.toml"))
		} else if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, ".config", "filterc", "filterc.toml"))
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				path = c
				break
			}
		}
		if path == "" {
			return cfg, nil
		}
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if explicit || !errors.Is(err, os.ErrNotExist) {
			return cfg, fmt.Errorf("config %s: %w", path, err)
		}
	}
	return cfg, nil
}
