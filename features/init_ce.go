// +build !ee

/*
2019 © Postgres.ai
*/

// Package features provides Enterprise features and their mocks.
package features

import (
	"gitlab.com/postgres-ai/joe/features/edition/ce/command/builder"
	"gitlab.com/postgres-ai/joe/features/edition/ce/entertainer"
	"gitlab.com/postgres-ai/joe/features/edition/ce/options"
)

// nolint:gochecknoinits
func init() {
	commandBuilder = builder.NewBuilder
	optionProvider = &options.Extra{}
	entertainerService = entertainer.New()
}
