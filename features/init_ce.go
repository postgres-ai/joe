// +build !ee

/*
2019 © Postgres.ai
*/

// Package features provides Enterprise features and their mocks.
package features

import (
	"gitlab.com/postgres-ai/joe/features/ce/command/builder"
	"gitlab.com/postgres-ai/joe/features/ce/options"
)

// nolint:gochecknoinits
func init() {
	commandBuilder = builder.NewBuilder
	flagProvider = &options.Extra{}
}
