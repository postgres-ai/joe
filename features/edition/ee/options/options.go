// +build ee

/*
2019 © Postgres.ai
*/

// Package options provides Enterprise options.
package options

import (
	"time"

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
	Quota     Quota     `yaml:"quota"`
	Audit     Audit     `yaml:"audit"`
	DBLab     DBLab     `yaml:"dblab"`
	Estimator Estimator `yaml:"estimator"`
}

// Quota contains quota configuration parameters.
type Quota struct {
	Limit    uint `description:"limit request rates to up to 2x of this number" env-default:"10" env:"EE_QUOTA_LIMIT"`
	Interval uint `description:"a time interval (in seconds) to apply a quota-limit" env-default:"60" env:"EE_QUOTA_INTERVAL"`
}

// Audit contains audit configuration parameters.
type Audit struct {
	Enabled bool `description:"enable logging of received commands" env:"EE_AUDIT_ENABLED"`
}

// DBLab contains Database Lab configuration parameters.
//nolint:lll
type DBLab struct {
	InstanceLimit uint `yaml:"instanceLimit" env-default:"2" description:"limit of available Database Lab instances" env:"EE_DBLAB_INSTANCE_LIMIT"`
}

// Estimator describes Enterprise options to estimate query timing.
//nolint:lll
type Estimator struct {
	ReadRatio         float64       `yaml:"readRatio" env-default:"1" description:"set up the read ratio of the estimator" env:"EE_ESTIMATOR_READ_RATIO"`
	WriteRatio        float64       `yaml:"writeRatio" env-default:"1" description:"set up the write ratio of the estimator" env:"EE_ESTIMATOR_WRITE_RATIO"`
	ProfilingInterval time.Duration `yaml:"profilingInterval" env-default:"10ms" description:"set up the profiling interval of the estimator" env:"EE_ESTIMATOR_PROFILING_INTERVAL"`
	SampleThreshold   int           `yaml:"sampleThreshold" env-default:"20" description:"set up the samples threshold of the estimator" env:"EE_ESTIMATOR_SAMPLE_THRESHOLD"`
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
		Estimator: definition.Estimator{
			ReadRatio:         e.Estimator.ReadRatio,
			WriteRatio:        e.Estimator.WriteRatio,
			ProfilingInterval: e.Estimator.ProfilingInterval,
			SampleThreshold:   e.Estimator.SampleThreshold,
		},
	}
}
