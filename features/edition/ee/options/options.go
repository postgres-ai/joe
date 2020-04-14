// +build ee

/*
2019 © Postgres.ai
*/

// Package options provides Enterprise options.
package options

import (
	"github.com/ilyakaznacheev/cleanenv"

	"gitlab.com/postgres-ai/joe/features/definition"
)

// EnterpriseContainer provides a wrapper for Enterprise configuration options.
// Changing these options you confirm that you have active
// subscription to Postgres.ai Platform Enterprise Edition https://postgres.ai).
type EnterpriseContainer struct {
	Enterprise `yaml:"enterprise"`
}

// Enterprise defines Enterprise options.
type Enterprise struct {
	Quota Quota `yaml:"quota"`
	Audit Audit `yaml:"audit"`
	DBLab DBLab `yaml:"dblab"`
}

// TODO (akartasov): add `env-default` tags after https://github.com/ilyakaznacheev/cleanenv/issues/40 has been merged.
// Quota contains quota configuration parameters.
type Quota struct {
	Limit    uint `description:"limit request rates to up to 2x of this number" env:"EE_QUOTA_LIMIT"`
	Interval uint `description:"a time interval (in seconds) to apply a quota-limit" env:"EE_QUOTA_INTERVAL"`
}

// Audit contains audit configuration parameters.
type Audit struct {
	Enabled bool `description:"enable logging of received commands" env:"EE_AUDIT_ENABLED"`
}

// DBLab contains Database Lab configuration parameters.
type DBLab struct {
	InstanceLimit uint `yaml:"instanceLimit" description:"limit of available Database Lab instances" env:"EE_DBLAB_INSTANCE_LIMIT"`
}

// Provider provides Enterprise configuration options.
type Provider struct{}

var _ definition.OptionProvider = (*Provider)(nil)

// GetEnterpriseOptions provides enterprise options.
func (e *Provider) GetEnterpriseOptions(file string) (definition.EnterpriseOptions, error) {
	container := EnterpriseContainer{}

	if err := cleanenv.ReadConfig(file, &container); err != nil {
		return definition.EnterpriseOptions{}, err
	}

	return container.toEnterpriseOptions(), nil
}

// toEnterpriseOptions converts an Enterprise specific container the EnterpriseOptions struct.
func (e *Enterprise) toEnterpriseOptions() definition.EnterpriseOptions {
	return definition.EnterpriseOptions{
		Quota: definition.Quota{
			Limit:    e.Quota.Limit,
			Interval: e.Quota.Interval,
		},
		Audit: definition.Audit{
			Enabled: e.Audit.Enabled,
		},
		DBLab: definition.DBLab{
			InstanceLimit: e.DBLab.InstanceLimit,
		},
	}
}
