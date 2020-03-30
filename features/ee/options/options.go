// +build ee

/*
2019 © Postgres.ai
*/

// Package options provides Enterprise command line options.
package options

import (
	"gitlab.com/postgres-ai/joe/features/definition"
)

// Extra provides Enterprise configuration flags.
// Changing these options you confirm that you have active subscription to Postgres.ai Platform Enterprise Edition https://postgres.ai).
// nolint:lll
type Extra struct {
	QuotaLimit    uint `long:"quota-limit" description:"limit request rates to up to 2x of this number" env:"QUOTA_LIMIT" default:"10"`
	QuotaInterval uint `long:"quota-interval" description:"a time interval (in seconds) to apply a quota-limit" env:"QUOTA_INTERVAL" default:"60"`
	AuditEnabled  bool `long:"audit-enabled" description:"enable logging of received commands" env:"AUDIT_ENABLED"`
}

var _ definition.FlagProvider = (*Extra)(nil)

// ToOpts returns the EnterpriseOptions struct.
func (e *Extra) ToOpts() definition.EnterpriseOptions {
	return definition.EnterpriseOptions{
		QuotaLimit:    e.QuotaLimit,
		QuotaInterval: e.QuotaInterval,
		AuditEnabled:  e.AuditEnabled,
	}
}
