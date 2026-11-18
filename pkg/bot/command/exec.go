/*
2019 © Postgres.ai
*/

package command

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/database-lab/v3/pkg/log"
	"gitlab.com/postgres-ai/database-lab/v3/pkg/util"

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
	userConn  *pgx.Conn
	messenger connection.Messenger
}

// NewExec return a new exec command.
func NewExec(command *platform.Command, msg *models.Message, session usermanager.UserSession,
	messengerSvc connection.Messenger) *ExecCmd {
	return &ExecCmd{
		command:   command,
		message:   msg,
		userConn:  session.CloneConnection,
		messenger: messengerSvc,
	}
}

// Execute runs the exec command.
func (cmd ExecCmd) Execute(ctx context.Context) error {
	if cmd.command.Query == "" {
		return errors.New(msgExecOptionReq)
	}

	start := time.Now()

	if _, err := cmd.userConn.Exec(ctx, cmd.command.Query); err != nil {
		log.Err("Failed to exec command: ", err)
		return err
	}

	totalTime := util.DurationToString(time.Since(start))

	result := fmt.Sprintf("The query has been executed. Duration: %s", totalTime)

	cmd.command.Response = result
	cmd.message.AppendText(result)

	if err := cmd.messenger.UpdateText(cmd.message); err != nil {
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
