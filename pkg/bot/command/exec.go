/*
2019 © Postgres.ai
*/

package command

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/database-lab/v2/pkg/log"
	dblabmodels "gitlab.com/postgres-ai/database-lab/v2/pkg/models"
	"gitlab.com/postgres-ai/database-lab/v2/pkg/util"

	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/services/platform"
	"gitlab.com/postgres-ai/joe/pkg/services/usermanager"
)

const (
	// msgExecOptionReq describes an exec error.
	msgExecOptionReq = "Use `exec` to run query, e.g. `exec drop index some_index_name`"
)

// ExecCmd defines the exec command.
type ExecCmd struct {
	command   *platform.Command
	message   *models.Message
	pool      *pgxpool.Pool
	userConn  *pgx.Conn
	messenger connection.Messenger
	clone     *dblabmodels.Clone
}

// NewExec return a new exec command.
func NewExec(command *platform.Command, msg *models.Message, session usermanager.UserSession,
	messengerSvc connection.Messenger) *ExecCmd {
	return &ExecCmd{
		command:   command,
		message:   msg,
		pool:      session.Pool,
		userConn:  session.CloneConnection,
		clone:     session.Clone,
		messenger: messengerSvc,
	}
}

// Execute runs the exec command.
func (cmd ExecCmd) Execute(ctx context.Context) error {
	if cmd.command.Query == "" {
		return errors.New(msgExecOptionReq)
	}

	serviceConn, err := getConn(ctx, cmd.pool)
	if err != nil {
		log.Err("failed to get connection: ", err)
		return err
	}

	defer serviceConn.Release()

	start := time.Now()

	if _, err := cmd.userConn.Exec(ctx, cmd.command.Query); err != nil {
		log.Err("Failed to exec command: ", err)
		return err
	}

	totalTime := util.DurationToString(time.Since(start))

	if err := serviceConn.Conn().Close(ctx); err != nil {
		log.Err("Failed to close connection: ", err)
		return err
	}

	result := fmt.Sprintf("The query has been executed. Duration: %s", totalTime)

	cmd.command.Response = result
	cmd.message.AppendText(result)

	if err = cmd.messenger.UpdateText(cmd.message); err != nil {
		log.Err("failed to update text while running the exec command:", err)
		return err
	}

	return nil
}

// getConn returns an acquired connection and Postgres backend PID.
func getConn(ctx context.Context, db *pgxpool.Pool) (*pgxpool.Conn, error) {
	conn, err := db.Acquire(ctx)
	if err != nil {
		log.Err("failed to acquire connection: ", err)
		return nil, err
	}

	return conn, nil
}
