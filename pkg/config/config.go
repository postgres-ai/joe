/*
2019 © Postgres.ai
*/

// Package config provides the App configuration.
package config

import (
	"time"

	"gitlab.com/postgres-ai/joe/features/definition"
	"gitlab.com/postgres-ai/joe/pkg/pgexplain"
)

// Config defines an App configuration.
type Config struct {
	App            App                          `yaml:"app"`
	Platform       Platform                     `yaml:"platform"`
	Registration   Registration                 `yaml:"registration"`
	ChannelMapping *ChannelMapping              `yaml:"channelMapping"`
	Explain        pgexplain.ExplainConfig      `yaml:"-"`
	Enterprise     definition.EnterpriseOptions `yaml:"-"`
}

// App defines a general application configuration.
type App struct {
	Version           string
	Host              string        `env:"JOE_APP_HOST"`
	Port              uint          `env:"JOE_APP_PORT" env-default:"2400"`
	MinNotifyDuration time.Duration `env:"JOE_APP_MIN_NOTIFY_DURATION" env-default:"60s"`
	Debug             bool          `env:"JOE_APP_DEBUG"`
}

// Platform describes configuration parameters of a Postgres.ai platform.
type Platform struct {
	URL            string `yaml:"url" env:"JOE_PLATFORM_URL" env-default:"https://postgres.ai/api/general"`
	Token          string `yaml:"token" env:"JOE_PLATFORM_TOKEN"`
	HistoryEnabled bool   `yaml:"historyEnabled" env:"JOE_PLATFORM_HISTORY_ENABLED"`
}

// Registration describes configuration parameters to register an application on the Platform.
type Registration struct {
	Enable    bool   `yaml:"enable"`
	PublicURL string `yaml:"publicURL"`
}

// ChannelMapping contains configuration parameters of communication types and Database Labs.
type ChannelMapping struct {
	CommunicationTypes map[string][]Workspace   `yaml:"communicationTypes,flow"`
	DBLabInstances     map[string]DBLabInstance `yaml:"dblabServers"`
}

// DBLabInstance contains Database Lab config.
type DBLabInstance struct {
	URL            string
	Token          string
	RequestTimeout time.Duration
}

// Workspace defines a connection space.
type Workspace struct {
	Name        string
	Credentials Credentials
	Channels    []Channel
}

// Credentials defines connection space credentials.
type Credentials struct {
	AccessToken   string `yaml:"accessToken"`
	SigningSecret string `yaml:"signingSecret"`
	AppLevelToken string `yaml:"appLevelToken"`
}

// Channel defines a connection channel configuration.
type Channel struct {
	ChannelID   string      `yaml:"channelID" json:"channel_id"`
	DBLabID     string      `yaml:"dblabServer" json:"-"`
	Project     string      `yaml:"project" json:"-"`
	DBLabParams DBLabParams `yaml:"dblabParams" json:"-"`
}

// DBLabParams defines database params for clone creation.
type DBLabParams struct {
	DBName  string `yaml:"dbname" json:"-"`
	SSLMode string `yaml:"sslmode" json:"-"`
}
