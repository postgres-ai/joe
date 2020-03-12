/*
2019 © Postgres.ai
*/

package config

import "gitlab.com/postgres-ai/joe/pkg/pgexplain"

type Bot struct {
	ConnStr                  string
	Port                     uint
	Explain                  pgexplain.ExplainConfig
	Quota                    Quota
	AuditEnabled             bool
	MinNotifyDurationMinutes uint

	DBLab DBLabInstance

	ApiUrl         string
	ApiToken       string
	ApiProject     string
	HistoryEnabled bool

	Version string
}

// Quota contains quota configuration parameters.
type Quota struct {
	Limit    uint
	Interval uint // Seconds.
}

// DBLabInstance contains Database Lab config.
type DBLabInstance struct {
	URL     string
	Token   string
	DBName  string // TODO(akartasov): Make a dynamically used name.
	SSLMode string
}
