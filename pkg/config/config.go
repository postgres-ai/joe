/*
2019 © Postgres.ai
*/

package config

import "gitlab.com/postgres-ai/joe/pkg/pgexplain"

type Bot struct {
	ConnStr              string
	Port                 uint
	Explain              pgexplain.ExplainConfig
	QuotaLimit           uint
	QuotaInterval        uint // Seconds.
	AuditEnabled         bool
	QueryReminderMinutes uint

	DBLab DBLabInstance

	ApiUrl         string
	ApiToken       string
	ApiProject     string
	HistoryEnabled bool

	Version string
}

// DBLabInstance contains Database Lab config.
type DBLabInstance struct {
	URL     string
	Token   string
	DBName  string // TODO(akartasov): Make a dynamically used name.
	SSLMode string
}
