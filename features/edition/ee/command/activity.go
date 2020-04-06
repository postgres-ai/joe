// +build ee

/*
2019 © Postgres.ai
*/

// Package command provides the Enterprise Edition commands.
package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/joe/features/definition"
	"gitlab.com/postgres-ai/joe/pkg/bot/querier"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/services/platform"
)

// ActivityCaption contains caption for rendered tables.
const ActivityCaption = "*Activity response:*\n"

// ActivityCmd defines the activity command.
type ActivityCmd struct {
	command   *platform.Command
	message   *models.Message
	db        *pgxpool.Pool
	messenger connection.Messenger
}

var _ definition.Executor = (*ActivityCmd)(nil)

// NewActivityCmd return a new exec command.
func NewActivityCmd(cmd *platform.Command, msg *models.Message, db *pgxpool.Pool, messengerSvc connection.Messenger) *ActivityCmd {
	return &ActivityCmd{
		command:   cmd,
		message:   msg,
		db:        db,
		messenger: messengerSvc,
	}
}

// Execute runs the activity command.
func (c *ActivityCmd) Execute() error {
	const truncateLength = 100

	query := fmt.Sprintf(`select
  pid::text,
  (case when (query <> '' and length(query) > %[1]d) then left(query, %[1]d) || '...' else query end) as query,
  coalesce(state, '') as state,
  coalesce(wait_event, '') as wait_event,
  coalesce(wait_event_type, '') as wait_event_type,
  coalesce((clock_timestamp() - query_start)::text, '') as query_duration,
  coalesce((clock_timestamp() - state_change)::text, '') as state_changed_ago
from pg_stat_activity 
where state in ('active', 'idle in transaction', 'disabled') and pid <> pg_backend_pid();`, truncateLength)

	tableString := &strings.Builder{}
	tableString.WriteString(ActivityCaption)

	activity, err := querier.DBQuery(context.TODO(), c.db, query)
	if err != nil {
		return errors.Wrap(err, "failed to make query")
	}

	querier.RenderTable(tableString, activity)
	c.message.AppendText(tableString.String())

	if err := c.messenger.UpdateText(c.message); err != nil {
		return errors.Wrap(err, "failed to publish message")
	}

	return nil
}
