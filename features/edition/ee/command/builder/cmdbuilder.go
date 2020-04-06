// +build ee

/*
2019 © Postgres.ai
*/

// Package builder provides command builder for building the Enterprise commands.
package builder

import (
	"github.com/jackc/pgx/v4/pgxpool"

	"gitlab.com/postgres-ai/joe/features/definition"
	"gitlab.com/postgres-ai/joe/features/edition/ee/command"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/services/platform"
)

// EnterpriseBuilder defines an enterprise command builder.
type EnterpriseBuilder struct {
	apiCommand *platform.Command
	message    *models.Message
	db         *pgxpool.Pool
	messenger  connection.Messenger
}

var _ definition.CmdBuilder = (*EnterpriseBuilder)(nil)

// NewBuilder creates a new enterprise command builder.
func NewBuilder(apiCmd *platform.Command, msg *models.Message, db *pgxpool.Pool, msgSvc connection.Messenger) definition.CmdBuilder {
	return &EnterpriseBuilder{
		apiCommand: apiCmd,
		message:    msg,
		db:         db,
		messenger:  msgSvc,
	}
}

// BuildActivityCmd builds a new activity command.
func (b *EnterpriseBuilder) BuildActivityCmd() definition.Executor {
	return command.NewActivityCmd(b.apiCommand, b.message, b.db, b.messenger)
}

// BuildTerminateCmd builds a new activity command.
func (b *EnterpriseBuilder) BuildTerminateCmd() definition.Executor {
	return command.NewTerminateCmd(b.apiCommand, b.message, b.db, b.messenger)
}
