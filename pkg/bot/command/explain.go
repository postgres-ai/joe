/*
2019 © Postgres.ai
*/

package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgtype/pgxtype"
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
	"gitlab.com/postgres-ai/joe/pkg/util/text"
)

// MsgExplainOptionReq describes an explain error.
const MsgExplainOptionReq = "Use `explain` to see the query's plan, e.g. `explain select 1`"

// Query Explain prefixes.
const (
	queryExplain        = "EXPLAIN (FORMAT TEXT) "
	queryExplainAnalyze = "EXPLAIN (ANALYZE, COSTS, VERBOSE, BUFFERS, FORMAT JSON) "
)

// Explain runs an explain query.
func Explain(ctx context.Context, msgSvc connection.Messenger, command *platform.Command, msg *models.Message,
	explainConfig pgexplain.ExplainConfig, estCfg definition.Estimator, db *pgxpool.Pool) error {
	if command.Query == "" {
		return errors.New(MsgExplainOptionReq)
	}

	conn, pid, err := getConn(ctx, db)
	if err != nil {
		log.Err("failed to get connection: ", err)
		return err
	}

	defer conn.Release()

	p := estimator.NewProfiler(db, estimator.TraceOptions{
		Pid:      pid,
		Interval: profilingInterval,
		StrSize:  strSize,
	})

	cmd := NewPlan(command, msg, db, msgSvc)
	msgInitText, err := cmd.explainWithoutExecution(ctx, conn)
	if err != nil {
		return errors.Wrap(err, "failed to run explain without execution")
	}

	// Start profiling.
	go p.Start(ctx)

	// Explain analyze request and processing.
	explainAnalyze, err := querier.DBQueryWithResponse(ctx, conn, queryExplainAnalyze+command.Query)
	if err != nil {
		return err
	}

	if err := conn.Conn().Close(ctx); err != nil {
		log.Err("Failed to close connection: ", err)
		return err
	}

	command.PlanExecJSON = explainAnalyze

	// Visualization.
	explain, err := pgexplain.NewExplain(explainAnalyze, explainConfig)
	if err != nil {
		log.Err("Explain parsing: ", err)

		return err
	}

	planText := explain.RenderPlanText()
	command.PlanExecText = planText

	planExecPreview, isTruncated := text.CutText(planText, PlanSize, SeparatorPlan)

	msg.SetText(msgInitText)
	msg.AppendText(fmt.Sprintf("*Plan with execution:*\n```%s```", planExecPreview))

	if err = msgSvc.UpdateText(msg); err != nil {
		log.Err("Show the plan with execution:", err)

		return err
	}

	if _, err := msgSvc.AddArtifact("plan-json", explainAnalyze, msg.ChannelID, msg.MessageID); err != nil {
		log.Err("File upload failed:", err)
		return err
	}

	filePlanPermalink, err := msgSvc.AddArtifact("plan-text", planText, msg.ChannelID, msg.MessageID)
	if err != nil {
		log.Err("File upload failed:", err)
		return err
	}

	detailsText := ""
	if isTruncated {
		detailsText = " " + CutText
	}

	msg.AppendText(fmt.Sprintf("<%s|Full execution plan>%s \n"+
		"_Other artifacts are provided in the thread_", filePlanPermalink, detailsText))

	if err = msgSvc.UpdateText(msg); err != nil {
		log.Err("File: ", err)
		return err
	}

	// Recommendations.
	tips, err := explain.GetTips()
	if err != nil {
		log.Err("Recommendations: ", err)
		return err
	}

	recommends := ""
	if len(tips) == 0 {
		recommends += ":white_check_mark: Looks good"
	} else {
		for _, tip := range tips {
			recommends += fmt.Sprintf(
				":exclamation: %s – %s <%s|Show details>\n", tip.Name,
				tip.Description, tip.DetailsUrl)
		}
	}

	command.Recommendations = recommends

	msg.AppendText("*Recommendations:*\n" + recommends)
	if err = msgSvc.UpdateText(msg); err != nil {
		log.Err("Show recommendations: ", err)
		return err
	}

	// Wait for profiling results.
	<-p.Finish()

	// Show stats if the total number of samples more than the default threshold.
	if p.CountSamples() >= sampleThreshold {
		msg.AppendText(fmt.Sprintf("*Profiling of wait events:*\n```%s```\n", p.RenderStat()))

		explain.EstimationTime = estimator.CalcTiming(p.WaitEventsRatio(), estCfg.ReadFactor, estCfg.WriteFactor, p.TotalTime())
	}

	// Summary.
	stats := explain.RenderStats()
	command.Stats = stats

	msg.AppendText(fmt.Sprintf("*Summary:*\n```%s```", stats))
	if err = msgSvc.UpdateText(msg); err != nil {
		log.Err("Show summary: ", err)
		return err
	}

	return nil
}

func listHypoIndexes(ctx context.Context, db pgxtype.Querier) ([]string, error) {
	rows, err := db.Query(ctx, "SELECT indexname FROM hypopg_list_indexes()")
	if err != nil {
		return nil, err
	}

	hypoIndexes := []string{}
	for rows.Next() {
		var indexName string
		if err := rows.Scan(&indexName); err != nil {
			return nil, err
		}

		hypoIndexes = append(hypoIndexes, indexName)
	}

	return hypoIndexes, nil
}

func isHypoIndexInvolved(explainResult string, hypoIndexes []string) bool {
	for _, index := range hypoIndexes {
		if strings.Contains(explainResult, index) {
			return true
		}
	}

	return false
}
