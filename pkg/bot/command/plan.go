/*
2019 © Postgres.ai
*/

package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/pkg/errors"
	"gitlab.com/postgres-ai/database-lab/v3/pkg/log"

	"gitlab.com/postgres-ai/joe/pkg/bot/querier"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/services/platform"
	"gitlab.com/postgres-ai/joe/pkg/util/text"
)

const (
	// MsgPlanOptionReq describes an explain without execution error.
	MsgPlanOptionReq = "Use `plan` to see the query's plan without execution, e.g. `plan select 1`"
	// MsgGenericPlanOptionReq describes a generic plan without execution error.
	MsgGenericPlanOptionReq = "Use `generic-plan` to see a generic plan without execution, e.g. `generic-plan select * from t where id = $1`"
	// MsgGenericPlanVersionReq describes the minimum PostgreSQL version for generic plans.
	MsgGenericPlanVersionReq = "`generic-plan` requires PostgreSQL 16 or newer"

	queryGenericPlan = "EXPLAIN (GENERIC_PLAN, FORMAT TEXT) "
)

// PlanCmd defines the plan command.
type PlanCmd struct {
	command   *platform.Command
	message   *models.Message
	userConn  *pgx.Conn
	generic   bool
	dbVersion int
	messenger connection.Messenger
}

// NewPlan returns a new plan command.
func NewPlan(cmd *platform.Command, msg *models.Message, db *pgx.Conn, messengerSvc connection.Messenger) *PlanCmd {
	return &PlanCmd{
		command:   cmd,
		message:   msg,
		userConn:  db,
		messenger: messengerSvc,
	}
}

// NewGenericPlan returns a new generic plan command.
func NewGenericPlan(cmd *platform.Command, msg *models.Message, db *pgx.Conn, dbVersion int,
	messengerSvc connection.Messenger) *PlanCmd {
	planCmd := NewPlan(cmd, msg, db, messengerSvc)
	planCmd.generic = true
	planCmd.dbVersion = dbVersion

	return planCmd
}

// Execute runs the plan command.
func (cmd PlanCmd) Execute(ctx context.Context) error {
	if cmd.command.Query == "" {
		if cmd.generic {
			return errors.New(MsgGenericPlanOptionReq)
		}

		return errors.New(MsgPlanOptionReq)
	}
	if cmd.generic && (cmd.dbVersion/postgresNumDiv) < pgVersion16 {
		return errors.New(MsgGenericPlanVersionReq)
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
	explainResult, err := querier.DBQueryWithResponse(ctx, cmd.userConn, cmd.planPrefix()+cmd.command.Query)
	if err != nil {
		return "", err
	}

	cmd.command.PlanText = explainResult
	planPreview, isTruncated := text.CutText(explainResult, PlanSize, SeparatorPlan)

	msgInitText := cmd.message.Text

	includeHypoPG := false
	planTitle := "Plan"
	explainPlanTitle := ""
	if cmd.generic {
		planTitle = "Generic plan"
	}

	if hypoIndexes, err := listHypoIndexes(ctx, cmd.userConn); err == nil && len(hypoIndexes) > 0 {
		if isHypoIndexInvolved(explainResult, hypoIndexes) {
			explainPlanTitle = " (HypoPG involved :ghost:)"
			includeHypoPG = true
		}
	}

	cmd.message.AppendText(fmt.Sprintf("*%s%s:*\n```%s```", planTitle, explainPlanTitle, planPreview))

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

			cmd.message.AppendText(fmt.Sprintf("*%s without HypoPG indexes:*\n```%s```", planTitle, planPreview))
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

	cmd.message.AppendText(fmt.Sprintf("<%s|Full %s (w/o execution)>%s", permalink, strings.ToLower(planTitle), detailsText))
	err = cmd.messenger.UpdateText(cmd.message)
	if err != nil {
		log.Err("File: ", err)
		return "", err
	}

	return msgInitText, nil
}

func (cmd *PlanCmd) runQueryWithoutHypo(ctx context.Context) (string, error) {
	tx, err := cmd.userConn.Begin(ctx)
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

	queryWithoutHypo := fmt.Sprintf(`%s %s`, cmd.planPrefix(), strings.Trim(cmd.command.Query, ";"))

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

func (cmd PlanCmd) planPrefix() string {
	if cmd.generic {
		return queryGenericPlan
	}

	return queryExplain
}
