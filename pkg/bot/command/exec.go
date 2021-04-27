/*
2019 © Postgres.ai
*/

package command

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/database-lab/v2/pkg/client/dblabapi"
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
	db        *pgxpool.Pool
	messenger connection.Messenger
	dblab     *dblabapi.Client
	clone     *dblabmodels.Clone
}

// NewExec return a new exec command.
func NewExec(command *platform.Command, msg *models.Message, session usermanager.UserSession,
	messengerSvc connection.Messenger, dblab *dblabapi.Client) *ExecCmd {
	return &ExecCmd{
		command:   command,
		message:   msg,
		db:        session.CloneConnection,
		clone:     session.Clone,
		messenger: messengerSvc,
		dblab:     dblab,
	}
}

// Execute runs the exec command.
func (cmd ExecCmd) Execute(ctx context.Context) error {
	if cmd.command.Query == "" {
		return errors.New(msgExecOptionReq)
	}

	conn, pid, err := getConn(ctx, cmd.db)
	if err != nil {
		log.Err("failed to get connection: ", err)
		return err
	}

	defer conn.Release()

	est, err := cmd.dblab.Estimate(ctx, cmd.clone.ID, strconv.Itoa(pid))
	if err != nil {
		return err
	}

	// Start profiling.
	<-est.Wait()

	start := time.Now()

	if _, err := conn.Exec(ctx, cmd.command.Query); err != nil {
		log.Err("Failed to exec command: ", err)
		return err
	}

	totalTime := util.DurationToString(time.Since(start))

	if err := conn.Conn().Close(ctx); err != nil {
		log.Err("Failed to close connection: ", err)
		return err
	}

	// Wait for profiling results.
	profResult := est.ReadResult()

	estimationTime, description := "", ""

	// Show stats if the total number of samples more than the default threshold.
	if profResult.IsEnoughStat {
		cmd.message.AppendText(fmt.Sprintf("```%s```", profResult.RenderedStat))
		estimationTime = profResult.EstTime
		totalTime = fmt.Sprintf("%.3f s", profResult.TotalTime)
		description = fmt.Sprintf("\n⠀* Estimated timing for production (experimental). <%s|How it works>", timingEstimatorDocLink)
	}

	result := fmt.Sprintf("The query has been executed. Duration: %s%s", totalTime, estimationTime)

	cmd.command.Response = result
	cmd.message.AppendText(result + description)

	if err = cmd.messenger.UpdateText(cmd.message); err != nil {
		log.Err("failed to update text while running the exec command:", err)
		return err
	}

	return nil
}

// getConn returns an acquired connection and Postgres backend PID.
func getConn(ctx context.Context, db *pgxpool.Pool) (*pgxpool.Conn, int, error) {
	var (
		pid int
		err error
	)

	conn, err := db.Acquire(ctx)
	if err != nil {
		log.Err("failed to acquire connection: ", err)
		return nil, 0, err
	}

	defer func() {
		if err != nil && conn != nil {
			conn.Release()
		}
	}()

	if err = conn.QueryRow(ctx, `select pg_backend_pid()`).Scan(&pid); err != nil {
		log.Err("failed to get backend PID: ", err)
		return nil, 0, err
	}

	return conn, pid, nil
}
