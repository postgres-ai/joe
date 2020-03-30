/*
2019 © Postgres.ai
*/

// Package builder provides command builder for building the Enterprise commands.
package builder

import (
	"database/sql"

	"gitlab.com/postgres-ai/joe/features/ce/command"
	"gitlab.com/postgres-ai/joe/features/definition"
	"gitlab.com/postgres-ai/joe/pkg/bot/api"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
)

const featuresDescription = ""

// CommunityBuilder represents a builder for non enterprise activity command.
type CommunityBuilder struct {
}

var (
	_ definition.CmdBuilder              = (*CommunityBuilder)(nil)
	_ definition.EnterpriseHelpMessenger = (*CommunityBuilder)(nil)
)

// NewBuilder creates a new activity builder.
func NewBuilder(_ *api.ApiCommand, _ *models.Message, _ *sql.DB, _ connection.Messenger) definition.CmdBuilder {
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

// GetEnterpriseHelpMessage provides description enterprise features.
func (builder *CommunityBuilder) GetEnterpriseHelpMessage() string {
	return featuresDescription
}
