/*
2019 © Postgres.ai
*/

// Package features provides Enterprise features and their mocks.
package features

import (
	"database/sql"

	"gitlab.com/postgres-ai/joe/features/definition"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/services/platform"
)

// CommandFactoryMethod defines a factory method to create Enterprise commands.
type CommandFactoryMethod func(*platform.Command, *models.Message, *sql.DB, connection.Messenger) definition.CmdBuilder

var commandBuilder CommandFactoryMethod

// GetBuilder gets builder initialized Enterprise command builder.
func GetBuilder() CommandFactoryMethod {
	return commandBuilder
}
