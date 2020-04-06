/*
2019 © Postgres.ai
*/

// Package builder provides command builder for building the Enterprise commands.
package builder

import (
	"github.com/jackc/pgx/v4/pgxpool"

	"gitlab.com/postgres-ai/joe/features/definition"
	"gitlab.com/postgres-ai/joe/features/edition/ce/command"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/services/platform"
)

// CommunityBuilder represents a builder for non enterprise activity command.
type CommunityBuilder struct {
}

var _ definition.CmdBuilder = (*CommunityBuilder)(nil)

// NewBuilder creates a new activity builder.
func NewBuilder(_ *platform.Command, _ *models.Message, _ *pgxpool.Pool, _ connection.Messenger) definition.CmdBuilder {
	return &CommunityBuilder{}
}

// BuildActivityCmd build a new Activity command.
func (builder *CommunityBuilder) BuildActivityCmd() definition.Executor {
	return &command.ActivityCmd{}
}

// BuildTerminateCmd build a new Terminate command.
func (builder *CommunityBuilder) BuildTerminateCmd() definition.Executor {
	return &command.TerminateCmd{}
}
