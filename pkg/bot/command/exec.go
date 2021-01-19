/*
2019 © Postgres.ai
*/

package command

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/pkg/errors"
	"gitlab.com/postgres-ai/database-lab/pkg/log"

	"gitlab.com/postgres-ai/joe/features/definition"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/services/estimator"
	"gitlab.com/postgres-ai/joe/pkg/services/platform"
)

const (
	// msgExecOptionReq describes an exec error.
	msgExecOptionReq = "Use `exec` to run query, e.g. `exec drop index some_index_name`"

	// profiling default values.
	profilingInterval = 20 * time.Millisecond
	strSize           = 128
	sampleThreshold   = 100
)

// ExecCmd defines the exec command.
type ExecCmd struct {
	command   *platform.Command
	message   *models.Message
	db        *pgxpool.Pool
	messenger connection.Messenger
	estCfg    definition.Estimator
}

// NewExec return a new exec command.
func NewExec(estCfg definition.Estimator, command *platform.Command, msg *models.Message, db *pgxpool.Pool,
	messengerSvc connection.Messenger) *ExecCmd {
	return &ExecCmd{
		command:   command,
		message:   msg,
		db:        db,
		messenger: messengerSvc,
		estCfg:    estCfg,
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

	p := estimator.NewProfiler(cmd.db, estimator.TraceOptions{
		Pid:      pid,
		Interval: cmd.estCfg.ProfilingInterval,
		StrSize:  strSize,
	})

	// Start profiling.
	go p.Start(ctx)

	if _, err := conn.Exec(ctx, cmd.command.Query); err != nil {
		log.Err("Failed to exec command: ", err)
		return err
	}

	if err := conn.Conn().Close(ctx); err != nil {
		log.Err("Failed to close connection: ", err)
		return err
	}

	// Wait for profiling results.
	<-p.Finish()

	result, estimationTime := "", ""

	// Show stats if the total number of samples more than the default threshold.
	if p.CountSamples() >= cmd.estCfg.SampleThreshold {
		result += fmt.Sprintf("```%s```\n", p.RenderStat())

		estimationTime = fmt.Sprintf(" (estimated for prod: %.3f s)",
			estimator.CalcTiming(p.WaitEventsRatio(), cmd.estCfg.ReadFactor, cmd.estCfg.WriteFactor, p.TotalTime()))
	}

	result += fmt.Sprintf("The query has been executed. Duration: %.3f s%s", p.TotalTime(), estimationTime)
	cmd.command.Response = result

	cmd.message.AppendText(result)
	if err = cmd.messenger.UpdateText(cmd.message); err != nil {
		log.Err("failed to update text while running the exec command:", err)
		return err
	}

	return nil
}

// getConn returns an acquired connection and Postgres backend PID.
func getConn(ctx context.Context, db *pgxpool.Pool) (*pgxpool.Conn, int, error) {
	var pid int

	conn, err := db.Acquire(ctx)
	if err != nil {
		log.Err("failed to acquire connection: ", err)
		return nil, 0, err
	}

	if err := conn.QueryRow(ctx, `select pg_backend_pid()`).Scan(&pid); err != nil {
		log.Err("failed to get backend PID: ", err)
		return nil, 0, err
	}

	return conn, pid, nil
}
