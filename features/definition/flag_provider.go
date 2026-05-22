/*
2019 © Postgres.ai
*/

// Package definition provides basic Enterprise feature definitions.
package definition

// OptionProvider defines an interface to receive values of Enterprise application options.
// The data argument carries the env-expanded YAML produced by config.LoadFile so providers
// can decode without re-reading the filesystem.
type OptionProvider interface {
	GetEnterpriseOptions(data []byte) (EnterpriseOptions, error)
}

// EnterpriseOptions describes Enterprise options of the application.
type EnterpriseOptions struct {
	Quota Quota
	Audit Audit
	DBLab DBLab
}

// Quota describes Enterprise quota options of the application.
type Quota struct {
	Limit    uint
	Interval uint
}

// Audit describes Enterprise audit options of the application.
type Audit struct {
	Enabled bool
}

// DBLab describes Enterprise dblab options of the application.
type DBLab struct {
	InstanceLimit uint
}
