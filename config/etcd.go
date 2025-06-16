package config

import (
	"github.com/Oudwins/zog"
	"regexp"
)

// EtcdConfig holds the configuration for connecting to etcd.
type EtcdConfig struct {
	Endpoints   []string `config:"endpoints"`
	DialTimeout int      `config:"dial_timeout"`
	Username    string   `config:"username"`
	Password    string   `config:"password"`
	Prefix      string   `config:"prefix"`
}

// Default returns a new EtcdConfig with default values
func (c *EtcdConfig) Default() *EtcdConfig {
	return &EtcdConfig{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 5000, // 5 seconds
		Prefix:      "/",
	}
}

// Schema returns the Zog validation schema for EtcdConfig
func (c *EtcdConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"endpoints": zog.Slice(zog.String().Match(regexp.MustCompile(`^[a-zA-Z0-9\-.]+:\d+$`))).Min(1).Required(),
		"dial_timeout": zog.Int().
			GTE(1).
			LTE(30000), // 30,000ms = 30 seconds
		"username": zog.String().Optional(),
		"password": zog.String().Optional(),
		"prefix":   zog.String().Default("/"),
	})
}
