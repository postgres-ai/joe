/*
2019 © Postgres.ai
*/

// Package definition provides basic Enterprise feature definitions.
package definition

// FlagProvider defines an interface to receive values of Enterprise application options.
type FlagProvider interface {
	ToOpts() EnterpriseOptions
}

// EnterpriseOptions describes Enterprise options of the application.
type EnterpriseOptions struct {
	QuotaLimit    uint
	QuotaInterval uint
	AuditEnabled  bool
}
