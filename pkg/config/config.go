/*
2019 © Postgres.ai
*/

// Package config provides the App configuration.
package config

import (
	"io/ioutil"

	"gopkg.in/yaml.v2"

	"gitlab.com/postgres-ai/joe/pkg/pgexplain"
)

// Config defines an App configuration.
type Config struct {
	App                      App
	Version                  string
	Port                     uint
	Explain                  pgexplain.ExplainConfig
	Quota                    Quota
	AuditEnabled             bool
	MinNotifyDurationMinutes uint
	Platform                 Platform
}

// App defines a general application configuration.
type App struct {
	Version                  string
	Port                     uint
	AuditEnabled             bool
	MinNotifyDurationMinutes uint
}

// Quota contains quota configuration parameters.
type Quota struct {
	Limit    uint
	Interval uint // Seconds.
}

// Platform describes configuration parameters of a Postgres.ai platform.
type Platform struct {
	URL            string
	Token          string
	Project        string
	HistoryEnabled bool
}

// Space contains configuration parameters of connections and Database Labs.
type Space struct {
	Connections    map[string][]Workspace   `yaml:"connections,flow"`
	DBLabInstances map[string]DBLabInstance `yaml:"dblabs"`
}

// DBLabInstance contains Database Lab config.
type DBLabInstance struct {
	URL     string `yaml:"url"`
	Token   string `yaml:"token"`
	DBName  string `yaml:"dbname"`
	SSLMode string `yaml:"sslmode"`
}

// Workspace defines a connection space.
type Workspace struct {
	Name        string      `yaml:"name"`
	Credentials Credentials `yaml:"credentials"`
	Channels    []Channel   `yaml:"channels"`
}

// Credentials defines connection space credentials.
type Credentials struct {
	AccessToken   string `yaml:"accessToken"`
	SigningSecret string `yaml:"signingSecret"`
}

// Channel defines a connection channel configuration.
type Channel struct {
	ChannelID string `yaml:"channelID"`
	DBLabID   string `yaml:"dblab"`
}

// Load loads configuration from file.
func Load(filename string) (*Space, error) {
	//nolint:gosec
	bytes, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var config Space
	if err = yaml.Unmarshal(bytes, &config); err != nil {
		return nil, err
	}

	return &config, nil
}
