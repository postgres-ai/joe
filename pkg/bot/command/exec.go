/*
2019 © Postgres.ai
*/

package command

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"gitlab.com/postgres-ai/database-lab/pkg/log"

	"gitlab.com/postgres-ai/joe/pkg/bot/api"
	"gitlab.com/postgres-ai/joe/pkg/bot/querier"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/util"
)

// MsgExecOptionReq describes an exec error.
const MsgExecOptionReq = "Use `exec` to run query, e.g. `exec drop index some_index_name`"

// ExecCmd defines the exec command.
type ExecCmd struct {
	apiCommand *api.ApiCommand
	message    *models.Message
	db         *sql.DB
	messenger  connection.Messenger
}

// NewExec return a new exec command.
func NewExec(apiCmd *api.ApiCommand, msg *models.Message, db *sql.DB, messengerSvc connection.Messenger) *ExecCmd {
	return &ExecCmd{
		apiCommand: apiCmd,
		message:    msg,
		db:         db,
		messenger:  messengerSvc,
	}
}

// Execute runs the exec command.
func (cmd ExecCmd) Execute() error {
	if cmd.apiCommand.Query == "" {
		return errors.New(MsgExecOptionReq)
	}

	start := time.Now()
	err := querier.DBExec(cmd.db, cmd.apiCommand.Query)
	elapsed := time.Since(start)
	if err != nil {
		log.Err("Exec:", err)
		return err
	}

	duration := util.DurationToString(elapsed)
	result := fmt.Sprintf("The query has been executed. Duration: %s", duration)
	cmd.apiCommand.Response = result

	cmd.message.AppendText(result)
	if err = cmd.messenger.UpdateText(cmd.message); err != nil {
		log.Err("Exec:", err)
		return err
	}

	return nil
}
