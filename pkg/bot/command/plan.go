/*
2019 © Postgres.ai
*/

package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/pkg/errors"
	"gitlab.com/postgres-ai/database-lab/pkg/log"

	"gitlab.com/postgres-ai/joe/pkg/bot/querier"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/services/platform"
	"gitlab.com/postgres-ai/joe/pkg/util/text"
)

// MsgPlanOptionReq describes an explain without execution error.
const MsgPlanOptionReq = "Use `plan` to see the query's plan without execution, e.g. `plan select 1`"

// PlanCmd defines the plan command.
type PlanCmd struct {
	command   *platform.Command
	message   *models.Message
	db        *pgxpool.Pool
	messenger connection.Messenger
}

// NewPlan return a new plan command.
func NewPlan(cmd *platform.Command, msg *models.Message, db *pgxpool.Pool, messengerSvc connection.Messenger) *PlanCmd {
	return &PlanCmd{
		command:   cmd,
		message:   msg,
		db:        db,
		messenger: messengerSvc,
	}
}

// Execute runs the plan command.
func (cmd PlanCmd) Execute(ctx context.Context) error {
	if cmd.command.Query == "" {
		return errors.New(MsgPlanOptionReq)
	}

	if _, err := cmd.explainWithoutExecution(ctx); err != nil {
		return errors.Wrap(err, "failed to run explain without execution")
	}

	fmt.Println(cmd.message.Text)

	return nil
}

// explainWithoutExecution runs explain without execution.
func (cmd *PlanCmd) explainWithoutExecution(ctx context.Context) (string, error) {
	// Explain request and show.
	explainResult, err := querier.DBQueryWithResponse(cmd.db, queryExplain+cmd.command.Query)
	if err != nil {
		return "", err
	}

	cmd.command.PlanText = explainResult
	planPreview, isTruncated := text.CutText(explainResult, PlanSize, SeparatorPlan)

	msgInitText := cmd.message.Text

	includeHypoPG := false
	explainPlanTitle := ""

	if hypoIndexes, err := listHypoIndexes(ctx, cmd.db); err == nil && len(hypoIndexes) > 0 {
		if isHypoIndexInvolved(explainResult, hypoIndexes) {
			explainPlanTitle = " (HypoPG involved :ghost:)"
			includeHypoPG = true
		}
	}

	cmd.message.AppendText(fmt.Sprintf("*Plan%s:*\n```%s```", explainPlanTitle, planPreview))

	if err := cmd.messenger.UpdateText(cmd.message); err != nil {
		log.Err("Show plan: ", err)
		return "", err
	}

	permalink, err := cmd.messenger.AddArtifact("plan-wo-execution-text", explainResult, cmd.message.ChannelID, cmd.message.MessageID)
	if err != nil {
		log.Err("File upload failed:", err)
		return "", err
	}

	if includeHypoPG {
		msgInitText = cmd.message.Text

		if explainResultWithoutHypo, err := cmd.runQueryWithoutHypo(ctx); err == nil {
			planPreview, isTruncated = text.CutText(explainResultWithoutHypo, PlanSize, SeparatorPlan)

			cmd.message.AppendText(fmt.Sprintf("*Plan without HypoPG indexes:*\n```%s```", planPreview))
			if err := cmd.messenger.UpdateText(cmd.message); err != nil {
				log.Err("Show plan: ", err)
				return "", err
			}

			msgInitText = cmd.message.Text

			if _, err := cmd.messenger.AddArtifact("plan-wo-execution-wo-hypo-text", explainResultWithoutHypo,
				cmd.message.ChannelID, cmd.message.MessageID); err != nil {
				log.Err("File upload failed:", err)
				return "", err
			}
		} else {
			log.Err("Failed to get a plan without a hypo index:", err)
		}
	}

	detailsText := ""
	if isTruncated {
		detailsText = " " + CutText
	}

	cmd.message.AppendText(fmt.Sprintf("<%s|Full plan (w/o execution)>%s", permalink, detailsText))
	err = cmd.messenger.UpdateText(cmd.message)
	if err != nil {
		log.Err("File: ", err)
		return "", err
	}

	return msgInitText, nil
}

func (cmd *PlanCmd) runQueryWithoutHypo(ctx context.Context) (string, error) {
	tx, err := cmd.db.Begin(ctx)
	if err != nil {
		return "", errors.Wrap(err, "failed to start a transaction")
	}

	defer func() {
		// Rollback is safe to call even if the tx is already closed.
		err = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, "set hypopg.enabled to false;"); err != nil {
		return "", errors.Wrap(err, "failed to disable a hypopg setting")
	}

	queryWithoutHypo := fmt.Sprintf(`%s %s`, queryExplain, strings.Trim(cmd.command.Query, ";"))

	rows, err := tx.Query(ctx, queryWithoutHypo)
	if err != nil {
		return "", errors.Wrap(err, "failed to run query")
	}
	defer rows.Close()

	explainResultWithoutHypo := strings.Builder{}

	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return "", errors.Wrap(err, "failed to scan result")
		}

		explainResultWithoutHypo.WriteString(s)
		explainResultWithoutHypo.WriteString("\n")
	}

	if err := rows.Err(); err != nil {
		return "", errors.Wrap(err, "failed to complete query")
	}

	if _, err := tx.Exec(ctx, "reset hypopg.enabled"); err != nil {
		return "", errors.Wrap(err, "failed to reset a hypopg setting ")
	}

	return explainResultWithoutHypo.String(), tx.Commit(ctx)
}
