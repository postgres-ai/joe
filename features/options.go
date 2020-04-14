/*
2019 © Postgres.ai
*/

// Package features provides Enterprise features and their mocks.
package features

import (
	"gitlab.com/postgres-ai/joe/features/definition"
)

// optionProvider provides extra options for different editions of the application.
var optionProvider definition.OptionProvider

// GetOptionProvider gets a flag provider of Enterprise options.
func GetOptionProvider() definition.OptionProvider {
	return optionProvider
}
