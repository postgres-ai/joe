// +build !ee

/*
2019 © Postgres.ai
*/

// Package options provides extra command line options.
package options

import (
	"gitlab.com/postgres-ai/joe/features/definition"
)

// Default values (changing these options you confirm that you have active
// subscription to Postgres.ai Platform Enterprise Edition https://postgres.ai).
const (
	defaultQuotaLimit    = 10
	defaultQuotaInterval = 60
	defaultAudit         = false
)

// Extra provides a mock of Enterprise flags.
type Extra struct {
	QuotaLimit    uint `long:"quota-limit" description:"Enterprise option. Not supported in CE version" default:"10" choice:"10"`
	QuotaInterval uint `long:"quota-interval" description:"Enterprise option. Not supported in CE version" default:"60" choice:"60"`
	AuditEnabled  bool `long:"audit-enabled" description:"Enterprise option. Not supported in CE version"`
}

var _ definition.FlagProvider = (*Extra)(nil)

// ToOpts returns the EnterpriseOptions struct.
func (e *Extra) ToOpts() definition.EnterpriseOptions {
	return definition.EnterpriseOptions{
		QuotaLimit:    defaultQuotaLimit,
		QuotaInterval: defaultQuotaInterval,
		AuditEnabled:  defaultAudit,
	}
}
