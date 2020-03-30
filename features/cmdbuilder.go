/*
2019 © Postgres.ai
*/

// Package features provides Enterprise features and their mocks.
package features

import (
	"database/sql"

	"gitlab.com/postgres-ai/joe/features/definition"
	"gitlab.com/postgres-ai/joe/pkg/bot/api"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
)

// CommandFactoryMethod defines a factory method to create Enterprise commands.
type CommandFactoryMethod func(*api.ApiCommand, *models.Message, *sql.DB, connection.Messenger) definition.CmdBuilder

var commandBuilder CommandFactoryMethod

// GetBuilder gets builder initialized Enterprise command builder.
func GetBuilder() CommandFactoryMethod {
	return commandBuilder
}
