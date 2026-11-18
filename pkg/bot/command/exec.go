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

	"gitlab.com/postgres-ai/joe/pkg/log"
	"gitlab.com/postgres-ai/joe/pkg/util"

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

// connAcquireTimeout bounds the wait for a free session pool connection, so a
// starved pool fails the command instead of blocking it forever.
var connAcquireTimeout = time.Minute

// errNoFreeConn is returned when the session pool has no free connection within connAcquireTimeout.
var errNoFreeConn = errors.New("all database connections of the session are busy; " +
	"wait for the running commands to finish and try again")

// getConn acquires a connection from the session pool.
func getConn(ctx context.Context, db *pgxpool.Pool) (*pgxpool.Conn, error) {
	acquireCtx, cancel := context.WithTimeout(ctx, connAcquireTimeout)
	defer cancel()

	conn, err := db.Acquire(acquireCtx)
	if err != nil {
		log.Err("failed to acquire connection: ", err)

		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return nil, errNoFreeConn
		}

		return nil, err
	}

	return conn, nil
}

// closeConn closes the connection instead of returning it to the pool, then releases its pool slot.
func closeConn(ctx context.Context, conn *pgxpool.Conn) {
	if err := conn.Conn().Close(ctx); err != nil {
		log.Err("failed to close connection: ", err)
	}

	conn.Release()
}
