/*
2019 © Postgres.ai
*/

package command

import (
	"context"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/joe/features/definition"
	"gitlab.com/postgres-ai/joe/pkg/bot/querier"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/services/platform"
)

// TerminateCaption contains caption for rendered tables.
const TerminateCaption = "*Terminate response:*\n"

// TerminateCmd defines the terminate command.
type TerminateCmd struct {
	command   *platform.Command
	message   *models.Message
	db        *pgxpool.Pool
	messenger connection.Messenger
}

var _ definition.Executor = (*TerminateCmd)(nil)

// NewTerminateCmd return a new terminate command.
func NewTerminateCmd(cmd *platform.Command, msg *models.Message, db *pgxpool.Pool, messengerSvc connection.Messenger) *TerminateCmd {
	return &TerminateCmd{
		command:   cmd,
		message:   msg,
		db:        db,
		messenger: messengerSvc,
	}
}

// Execute runs the terminate command.
func (c *TerminateCmd) Execute() error {
	pid, err := strconv.Atoi(c.command.Query)
	if err != nil {
		return errors.Wrap(err, "invalid pid given")
	}

	query := "select pg_terminate_backend($1)::text"

	terminate, err := querier.DBQuery(context.TODO(), c.db, query, pid)
	if err != nil {
		return errors.Wrap(err, "failed to make query")
	}

	tableString := &strings.Builder{}
	tableString.WriteString(TerminateCaption)

	querier.RenderTable(tableString, terminate)
	c.message.AppendText(tableString.String())

	if err := c.messenger.UpdateText(c.message); err != nil {
		return errors.Wrap(err, "failed to publish message")
	}

	return nil
}
