/*
2019 © Postgres.ai
*/

// Package features provides Enterprise features and their mocks.
package features

import (
	"gitlab.com/postgres-ai/joe/features/definition"
)

// options contains extra flags for different editions of the application.
var flagProvider definition.FlagProvider

// GetFlagProvider gets a flag provider of Enterprise options.
func GetFlagProvider() definition.FlagProvider {
	return flagProvider
}
