/*
2019 © Postgres.ai
*/

package command

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"
	"gitlab.com/postgres-ai/database-lab/pkg/log"

	"gitlab.com/postgres-ai/joe/pkg/bot/api"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/transmission"
	"gitlab.com/postgres-ai/joe/pkg/util/text"
)

func Transmit(apiCmd *api.ApiCommand, msg *models.Message, msgSvc connection.Messenger, runner transmission.Runner) error {
	// See transmission.prepareCommandParam for more comments.
	if strings.ContainsAny(apiCmd.Query, "\n;\\ ") {
		err := errors.New("query should not contain semicolons, new lines, spaces, and excess backslashes")
		log.Err(err)
		return err
	}

	transmissionCmd := apiCmd.Command + " " + apiCmd.Query

	cmd, err := runner.Run(transmissionCmd)
	if err != nil {
		log.Err(err)
		return err
	}

	apiCmd.Response = cmd

	cmdPreview, trnd := text.CutText(cmd, PlanSize, SeparatorPlan)

	msg.AppendText(fmt.Sprintf("*Command output:*\n```%s```", cmdPreview))
	if err = msgSvc.UpdateText(msg); err != nil {
		log.Err("Show command output:", err)
		return err
	}

	fileCmdPermalink, err := msgSvc.AddArtifact("command", cmd, msg.ChannelID, msg.MessageID)
	if err != nil {
		log.Err("File upload failed:", err)
		return err
	}

	detailsText := ""
	if trnd {
		detailsText = " " + CutText
	}

	msg.AppendText(fmt.Sprintf("<%s|Full command output>%s\n", fileCmdPermalink, detailsText))
	if err = msgSvc.UpdateText(msg); err != nil {
		log.Err("File: ", err)
		return err
	}

	return nil
}
