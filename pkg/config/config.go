/*
2019 © Postgres.ai
*/

// Package config provides the App configuration.
package config

import (
	"time"

	"gitlab.com/postgres-ai/joe/features/definition"
)

const (
	// ConfigsPath declares the directory where configuration files are stored.
	ConfigsPath = "configs"

	// AppFilename declares name of the application configuration file.
	AppFilename = "joe.yml"

	// MetadataPath declares path where metadata files are stored.
	MetadataPath = "meta"

	// SessionsFilename declares name of the file that stores current Joe Bot sessions.
	SessionsFilename = "sessions.json"
)

// Config defines an App configuration.
type Config struct {
	App            App                          `yaml:"app"`
	Platform       Platform                     `yaml:"platform"`
	APIV2          APIV2                        `yaml:"apiV2"`
	Registration   Registration                 `yaml:"registration"`
	ChannelMapping *ChannelMapping              `yaml:"channelMapping"`
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
	Project        string `yaml:"project" env:"JOE_PLATFORM_PROJECT"`
	HistoryEnabled bool   `yaml:"historyEnabled" env:"JOE_PLATFORM_HISTORY_ENABLED"`
}

// APIV2 describes the Joe API v2 (signed reply callback) options. When
// enabled, Joe accepts the Platform's schema_version=2 dispatch requests on
// the webui command endpoint and POSTs HMAC-signed replies to the dispatch's
// reply_url (header x-joe-signature) per the locked signing contract.
type APIV2 struct {
	// Enabled turns on v2 dispatch handling. Default: false.
	Enabled bool `yaml:"enabled" env:"JOE_API_V2_ENABLED"`

	// ReplySecret signs v2 replies. When empty, the webui workspace's
	// signingSecret (the instance verify token — the secret the Platform
	// uses for reply verification) is used, which is the standard setup.
	ReplySecret string `yaml:"replySecret" env:"JOE_API_V2_REPLY_SECRET"`

	// ReplyHost pins the reply_url callback hostname (SSRF allowlist): a
	// dispatch whose reply_url points anywhere else is rejected. When
	// empty, the hostname of platform.url is used, which is the standard
	// setup.
	ReplyHost string `yaml:"replyHost" env:"JOE_API_V2_REPLY_HOST"`
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
	DBLabParams DBLabParams `yaml:"dblabParams" json:"-"`
}

// DBLabParams defines database params for clone creation.
type DBLabParams struct {
	DBName  string `yaml:"dbname" json:"-"`
	SSLMode string `yaml:"sslmode" json:"-"`
}
