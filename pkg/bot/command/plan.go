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

	"gitlab.com/postgres-ai/joe/pkg/bot/api"
	"gitlab.com/postgres-ai/joe/pkg/bot/querier"
	"gitlab.com/postgres-ai/joe/pkg/chatapi"
	"gitlab.com/postgres-ai/joe/pkg/util/text"
)

// MsgPlanOptionReq describes an explain without execution error.
const MsgPlanOptionReq = "Use `plan` to see the query's plan without execution, e.g. `plan select 1`"

// PlanCmd defines the plan command.
type PlanCmd struct {
	apiCommand *api.ApiCommand
	message    *chatapi.Message
	db         *sql.DB
	chat       *chatapi.Chat
}

// NewPlan return a new plan command.
func NewPlan(apiCmd *api.ApiCommand, msg *chatapi.Message, db *sql.DB, chat *chatapi.Chat) *PlanCmd {
	return &PlanCmd{
		apiCommand: apiCmd,
		message:    msg,
		db:         db,
		chat:       chat,
	}
}

// Execute runs the plan command.
func (cmd PlanCmd) Execute() error {
	if cmd.apiCommand.Query == "" {
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
	explainResult, err := querier.DBQueryWithResponse(cmd.db, queryExplain+cmd.apiCommand.Query)
	if err != nil {
		return "", false, err
	}

	cmd.apiCommand.PlanText = explainResult
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

	if err := cmd.message.Append(fmt.Sprintf("*Plan%s:*\n```%s```", explainPlanTitle, planPreview)); err != nil {
		log.Err("Show plan: ", err)
		return "", false, err
	}

	filePlanWoExec, err := cmd.chat.UploadFile("plan-wo-execution-text", explainResult, cmd.message.ChannelID, cmd.message.Timestamp)
	if err != nil {
		log.Err("File upload failed:", err)
		return "", false, err
	}

	if includeHypoPG {
		msgInitText = cmd.message.Text

		queryWithoutHypo := fmt.Sprintf(`set hypopg.enabled to false; %s %s; reset hypopg.enabled;`, queryExplain,
			strings.Trim(cmd.apiCommand.Query, ";"))

		explainResultWithoutHypo, err := querier.DBQueryWithResponse(cmd.db, queryWithoutHypo)
		if err == nil {
			planPreview, isTruncated = text.CutText(explainResultWithoutHypo, PlanSize, SeparatorPlan)

			if err := cmd.message.Append(fmt.Sprintf("*Plan without HypoPG indexes:*\n```%s```", planPreview)); err != nil {
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

	err = cmd.message.Append(fmt.Sprintf("<%s|Full plan (w/o execution)>%s", filePlanWoExec.Permalink, detailsText))
	if err != nil {
		log.Err("File: ", err)
		return "", false, err
	}

	return msgInitText, isTruncated, nil
}
