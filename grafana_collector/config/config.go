package config

import (
	"github.com/BurntSushi/toml"
	"github.com/juju/errors"
)

// Config contains configuration options.
type Config struct {
	Grafana  grafana
	Font     font
	Rect     map[string]rect
	Position position
}

type grafana struct {
	Theme         string
	ClientTimeout int `toml:"client-timeout"`
	ServerTimeout int `toml:"server-timeout"`
	RetryInterval int `toml:"retry-interval"`
}

type font struct {
	Family string
	Ttf    string
	Size   int
}

type rect struct {
	Width  float64
	Height float64
}

type position struct {
	X       float64
	TitleY1 float64 `toml:"title-y1"`
	TitleY2 float64 `toml:"title-y2"`
	ImageY1 float64 `toml:"image-y1"`
	ImageY2 float64 `toml:"image-y2"`
	Br      float64
}

var defaultConf = Config{
	Grafana: grafana{
		Theme:         "dark",
		ClientTimeout: 300,
		ServerTimeout: 300,
		RetryInterval: 10,
	},
	Font: font{
		Family: "opensans",
		Ttf:    "OpenSans-Regular.ttf",
		Size:   14,
	},
	Rect: map[string]rect{
		"page": {
			Width:  595.28,
			Height: 841.89,
		},
		"graph": {
			Width:  480.0,
			Height: 240.0,
		},
		"singlestat": {
			Width:  480.0,
			Height: 93.0,
		},
	},
	Position: position{
		X:       50.0,
		TitleY1: 60.0,
		TitleY2: 350.0,
		ImageY1: 80.0,
		ImageY2: 370.0,
		Br:      20.0,
	},
}

var globalConf = defaultConf

// GetGlobalConfig returns global configurations.
func GetGlobalConfig() *Config {
	return &globalConf
}

// SetConfig ... loads config options from a toml file.
func (c *Config) SetConfig(configFile string) error {
	_, err := toml.DecodeFile(configFile, c)
	return errors.Trace(err)
}
