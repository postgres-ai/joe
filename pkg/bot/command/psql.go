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
	"gitlab.com/postgres-ai/joe/pkg/chatapi"
	"gitlab.com/postgres-ai/joe/pkg/transmission"
	"gitlab.com/postgres-ai/joe/pkg/util/text"
)

func Transmit(apiCmd *api.ApiCommand, msg *chatapi.Message, chat *chatapi.Chat, runner transmission.Runner) error {
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

	if err = msg.Append(fmt.Sprintf("*Command output:*\n```%s```", cmdPreview)); err != nil {
		log.Err("Show command output:", err)
		return err
	}

	fileCmd, err := chat.UploadFile("command", cmd, msg.ChannelID, msg.Timestamp)
	if err != nil {
		log.Err("File upload failed:", err)
		return err
	}

	detailsText := ""
	if trnd {
		detailsText = " " + CutText
	}

	if err = msg.Append(fmt.Sprintf("<%s|Full command output>%s\n", fileCmd.Permalink, detailsText)); err != nil {
		log.Err("File: ", err)
		return err
	}

	return nil
}
