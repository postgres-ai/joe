/*
2019 © Postgres.ai
*/

package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgtype/pgxtype"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/database-lab/v2/pkg/log"

	"gitlab.com/postgres-ai/joe/pkg/bot/querier"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/pgexplain"
	"gitlab.com/postgres-ai/joe/pkg/services/platform"
	"gitlab.com/postgres-ai/joe/pkg/services/usermanager"
	"gitlab.com/postgres-ai/joe/pkg/util/text"
)

const (
	// MsgExplainOptionReq describes an explain error.
	MsgExplainOptionReq = "Use `explain` to see the query's plan, e.g. `explain select 1`"

	// Query Explain prefixes.
	queryExplain        = "EXPLAIN (FORMAT TEXT) "
	queryExplainAnalyze = "EXPLAIN (ANALYZE, COSTS, VERBOSE, BUFFERS, FORMAT JSON) "

	// locksTitle shows locks for a single query analyzed with EXPLAIN.
	locksTitle = "*Query heavy locks:*\n"
)

// Explain runs an explain query.
func Explain(ctx context.Context, msgSvc connection.Messenger, command *platform.Command, msg *models.Message,
	explainConfig pgexplain.ExplainConfig, session usermanager.UserSession) error {
	if command.Query == "" {
		return errors.New(MsgExplainOptionReq)
	}

	serviceConn, err := getConn(ctx, session.Pool)
	if err != nil {
		log.Err("failed to get connection:", err)
		return err
	}

	defer func() {
		if err := serviceConn.Conn().Close(ctx); err != nil {
			log.Err("failed to close connection: ", err)
		}

		serviceConn.Release()
	}()

	tx, err := serviceConn.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		log.Err("failed to begin transaction:", err)
		return err
	}

	defer func() {
		if err := tx.Rollback(ctx); err != nil {
			log.Err("failed to rollback transaction:", err)
		}
	}()

	txPID, err := querier.GetBackendPID(ctx, tx)
	if err != nil {
		log.Err("failed to get backend PID:", err)
		return err
	}

	cmd := NewPlan(command, msg, session.CloneConnection, msgSvc)
	msgInitText, err := cmd.explainWithoutExecution(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to run explain without execution")
	}

	explainAnalyze, err := querier.DBQueryWithResponse(ctx, tx, queryExplainAnalyze+command.Query)
	if err != nil {
		return err
	}

	// Observe query locks.
	result, err := observeLocks(ctx, session.Pool, txPID)
	if err != nil {
		log.Err("failed to observe locks:", err)
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

	// Show query locks.
	tableString := &strings.Builder{}
	querier.RenderTable(tableString, result)

	queryLocks := tableString.String()
	command.QueryLocks = strings.Trim(queryLocks, "`")
	msg.AppendText(locksTitle + queryLocks)

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

func observeLocks(ctx context.Context, db *pgxpool.Pool, txPID int) ([][]string, error) {
	observeConn, err := getConn(ctx, db)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err := observeConn.Conn().Close(ctx); err != nil {
			log.Err("failed to close observer connection:", err)
		}

		observeConn.Release()
	}()

	return querier.ObserveLocks(ctx, observeConn, txPID)
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
