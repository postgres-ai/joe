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
	"gitlab.com/postgres-ai/joe/pkg/config"
	"gitlab.com/postgres-ai/joe/pkg/pgexplain"
	"gitlab.com/postgres-ai/joe/pkg/util/text"
)

func Explain(chat *chatapi.Chat, apiCmd *api.ApiCommand, msg *chatapi.Message, botCfg config.Bot, db *sql.DB) error {
	var detailsText string
	var trnd bool

	explainConfig := botCfg.Explain

	if apiCmd.Query == "" {
		return errors.New(MsgExplainOptionReq)
	}

	// Explain request and show.
	var explainResult, err = querier.DBExplain(db, apiCmd.Query)
	if err != nil {
		return err
	}

	apiCmd.PlanText = explainResult
	planPreview, trnd := text.CutText(explainResult, PlanSize, SeparatorPlan)

	msgInitText := msg.Text

	includeHypoPG := false
	explainPlanTitle := ""

	if hypoIndexes, err := listHypoIndexes(db); err == nil && len(hypoIndexes) > 0 {
		if isHypoIndexInvolved(explainResult, hypoIndexes) {
			explainPlanTitle = " (HypoPG involved :ghost:)"
			includeHypoPG = true
		}
	}

	if err := msg.Append(fmt.Sprintf("*Plan%s:*\n```%s```", explainPlanTitle, planPreview)); err != nil {
		log.Err("Show plan: ", err)
		return err
	}

	if includeHypoPG {
		msgInitText = msg.Text
	}

	filePlanWoExec, err := chat.UploadFile("plan-wo-execution-text", explainResult, msg.ChannelID, msg.Timestamp)
	if err != nil {
		log.Err("File upload failed:", err)
		return err
	}

	detailsText = ""
	if trnd {
		detailsText = " " + CutText
	}

	err = msg.Append(fmt.Sprintf("<%s|Full plan (w/o execution)>%s", filePlanWoExec.Permalink, detailsText))
	if err != nil {
		log.Err("File: ", err)
		return err
	}

	// Explain analyze request and processing.
	explainAnalyze, err := querier.DBExplainAnalyze(db, apiCmd.Query)
	if err != nil {
		return err
	}

	apiCmd.PlanExecJson = explainAnalyze

	// Visualization.
	explain, err := pgexplain.NewExplain(explainAnalyze, explainConfig)
	if err != nil {
		log.Err("Explain parsing: ", err)

		return err
	}

	vis := explain.RenderPlanText()
	apiCmd.PlanExecText = vis

	planExecPreview, trnd := text.CutText(vis, PlanSize, SeparatorPlan)

	err = msg.Replace(msgInitText + chatapi.CHAT_APPEND_SEPARATOR +
		fmt.Sprintf("*Plan with execution:*\n```%s```", planExecPreview))
	if err != nil {
		log.Err("Show the plan with execution:", err)

		return err
	}

	_, err = chat.UploadFile("plan-json", explainAnalyze, msg.ChannelID, msg.Timestamp)
	if err != nil {
		log.Err("File upload failed:", err)
		return err
	}

	filePlan, err := chat.UploadFile("plan-text", vis, msg.ChannelID, msg.Timestamp)
	if err != nil {
		log.Err("File upload failed:", err)
		return err
	}

	detailsText = ""
	if trnd {
		detailsText = " " + CutText
	}

	err = msg.Append(fmt.Sprintf("<%s|Full execution plan>%s \n"+
		"_Other artifacts are provided in the thread_",
		filePlan.Permalink, detailsText))
	if err != nil {
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

	apiCmd.Recommendations = recommends

	if err = msg.Append("*Recommendations:*\n" + recommends); err != nil {
		log.Err("Show recommendations: ", err)
		return err
	}

	// Summary.
	stats := explain.RenderStats()
	apiCmd.Stats = stats

	if err = msg.Append(fmt.Sprintf("*Summary:*\n```%s```", stats)); err != nil {
		log.Err("Show summary: ", err)
		return err
	}

	return nil
}

func listHypoIndexes(db *sql.DB) ([]string, error) {
	rows, err := db.Query("SELECT indexname FROM hypopg_list_indexes()")
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
