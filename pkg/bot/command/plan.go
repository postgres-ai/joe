/*
2019 © Postgres.ai
*/

package command

import (
	"database/sql"
	"fmt"
	"strings"

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
	db        *sql.DB
	messenger connection.Messenger
}

// NewPlan return a new plan command.
func NewPlan(cmd *platform.Command, msg *models.Message, db *sql.DB, messengerSvc connection.Messenger) *PlanCmd {
	return &PlanCmd{
		command:   cmd,
		message:   msg,
		db:        db,
		messenger: messengerSvc,
	}
}

// Execute runs the plan command.
func (cmd PlanCmd) Execute() error {
	if cmd.command.Query == "" {
		return errors.New(MsgPlanOptionReq)
	}

	if _, _, err := cmd.explainWithoutExecution(); err != nil {
		return errors.Wrap(err, "failed to run explain without execution")
	}

	fmt.Println(cmd.message.Text)

	return nil
}

// explainWithoutExecution runs explain without execution.
func (cmd *PlanCmd) explainWithoutExecution() (string, bool, error) {
	// Explain request and show.
	explainResult, err := querier.DBQueryWithResponse(cmd.db, queryExplain+cmd.command.Query)
	if err != nil {
		return "", false, err
	}

	cmd.command.PlanText = explainResult
	planPreview, isTruncated := text.CutText(explainResult, PlanSize, SeparatorPlan)

	msgInitText := cmd.message.Text

	includeHypoPG := false
	explainPlanTitle := ""

	if hypoIndexes, err := listHypoIndexes(cmd.db); err == nil && len(hypoIndexes) > 0 {
		if isHypoIndexInvolved(explainResult, hypoIndexes) {
			explainPlanTitle = " (HypoPG involved :ghost:)"
			includeHypoPG = true
		}
	}

	cmd.message.AppendText(fmt.Sprintf("*Plan%s:*\n```%s```", explainPlanTitle, planPreview))

	if err := cmd.messenger.UpdateText(cmd.message); err != nil {
		log.Err("Show plan: ", err)
		return "", false, err
	}

	permalink, err := cmd.messenger.AddArtifact("plan-wo-execution-text", explainResult, cmd.message.ChannelID, cmd.message.MessageID)
	if err != nil {
		log.Err("File upload failed:", err)
		return "", false, err
	}

	if includeHypoPG {
		msgInitText = cmd.message.Text

		queryWithoutHypo := fmt.Sprintf(`set hypopg.enabled to false; %s %s; reset hypopg.enabled;`, queryExplain,
			strings.Trim(cmd.command.Query, ";"))

		explainResultWithoutHypo, err := querier.DBQueryWithResponse(cmd.db, queryWithoutHypo)
		if err == nil {
			planPreview, isTruncated = text.CutText(explainResultWithoutHypo, PlanSize, SeparatorPlan)

			cmd.message.AppendText(fmt.Sprintf("*Plan without HypoPG indexes:*\n```%s```", planPreview))
			if err := cmd.messenger.UpdateText(cmd.message); err != nil {
				log.Err("Show plan: ", err)
				return "", false, err
			}

			msgInitText = cmd.message.Text
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
		return "", false, err
	}

	return msgInitText, isTruncated, nil
}
