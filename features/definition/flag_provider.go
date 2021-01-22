/*
2019 © Postgres.ai
*/

// Package definition provides basic Enterprise feature definitions.
package definition

import (
	"time"
)

// OptionProvider defines an interface to receive values of Enterprise application options.
type OptionProvider interface {
	GetEnterpriseOptions(file string) (EnterpriseOptions, error)
}

// EnterpriseOptions describes Enterprise options of the application.
type EnterpriseOptions struct {
	Quota     Quota
	Audit     Audit
	DBLab     DBLab
	Estimator Estimator
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

// Estimator describes Enterprise options to estimate query timing.
type Estimator struct {
	ReadRatio         float64
	WriteRatio        float64
	ProfilingInterval time.Duration
	SampleThreshold   int
}
