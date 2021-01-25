/*
2019 © Postgres.ai
*/

package command

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/pkg/errors"
	"gitlab.com/postgres-ai/database-lab/pkg/log"

	"gitlab.com/postgres-ai/joe/features/definition"
	"gitlab.com/postgres-ai/joe/pkg/bot/querier"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/pgexplain"
	"gitlab.com/postgres-ai/joe/pkg/services/estimator"
	"gitlab.com/postgres-ai/joe/pkg/services/platform"
	"gitlab.com/postgres-ai/joe/pkg/util/operator"
)

const (
	// msgExecOptionReq describes an exec error.
	msgExecOptionReq = "Use `exec` to run query, e.g. `exec drop index some_index_name`"

	// profiling default values.
	profilingInterval = 10 * time.Millisecond
	sampleThreshold   = 20
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

const dbStatQuery = `
select
  blk_write_time,
  blks_read,
  blks_hit,
  blk_read_time
from pg_stat_bgwriter, pg_stat_database
where datname = current_database();
`

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
	})

	// Start profiling.
	go p.Start(ctx)

	var explain *pgexplain.Explain = nil

	op := strings.SplitN(cmd.command.Query, " ", 2)[0]

	switch {
	case operator.IsDML(op):
		explain, err = getExplain(ctx, conn, cmd.command.Query)
		if err != nil {
			log.Err("Failed to exec command: ", err)
			return err
		}

	default:
		if _, err := conn.Exec(ctx, cmd.command.Query); err != nil {
			log.Err("Failed to exec command: ", err)
			return err
		}
	}

	if err := conn.Conn().Close(ctx); err != nil {
		log.Err("Failed to close connection: ", err)
		return err
	}

	// Wait for profiling results.
	<-p.Finish()

	estimationTime, description := "", ""

	// Show stats if the total number of samples more than the default threshold.
	if p.CountSamples() >= cmd.estCfg.SampleThreshold {
		cmd.message.AppendText(fmt.Sprintf("```%s```", p.RenderStat()))

		est := estimator.NewTiming(p.WaitEventsRatio(), cmd.estCfg.ReadRatio, cmd.estCfg.WriteRatio)

		if explain != nil {
			dbStat := estimator.StatDatabase{}

			if err := cmd.db.QueryRow(ctx, dbStatQuery).Scan(
				&dbStat.BlockWriteTime,
				&dbStat.BlocksRead,
				&dbStat.BlocksHit,
				&dbStat.BlockReadTime); err != nil {
				log.Err("Failed to collect database stat: ", err)
				return err
			}

			readBlocks := explain.SharedHitBlocks + explain.SharedReadBlocks

			est.SetDBStat(dbStat)
			est.SetReadBlocks(readBlocks)
		}

		minTiming := est.CalcMin(p.TotalTime())
		maxTiming := est.CalcMax(p.TotalTime())

		estimationTime = fmt.Sprintf(" (estimated* for prod: min - %.3f s, max - %.3f s)", minTiming, maxTiming)
		description = fmt.Sprintf("\n⠀* <%s|How estimation works>", timingEstimatorDocLink)
	}

	result := fmt.Sprintf("The query has been executed. Duration: %.3f s%s", p.TotalTime(), estimationTime)

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

// getExplain analyzes query.
func getExplain(ctx context.Context, conn *pgxpool.Conn, query string) (*pgexplain.Explain, error) {
	explainAnalyze, err := querier.DBQueryWithResponse(ctx, conn, queryExplainAnalyze+query)
	if err != nil {
		log.Err("Failed to exec command: ", err)
		return nil, err
	}

	var explains []pgexplain.Explain

	if err := json.NewDecoder(strings.NewReader(explainAnalyze)).Decode(&explains); err != nil {
		return nil, err
	}

	if len(explains) == 0 {
		return nil, errors.New("Empty explain")
	}

	explain := explains[0]

	return &explain, nil
}
