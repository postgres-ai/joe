/*
2019 © Postgres.ai
*/

package command

import (
	"context"

	"gitlab.com/postgres-ai/database-lab/pkg/client/dblabapi"
	"gitlab.com/postgres-ai/database-lab/pkg/log"

	"gitlab.com/postgres-ai/joe/pkg/bot/api"
	"gitlab.com/postgres-ai/joe/pkg/chatapi"
)

func ResetSession(ctx context.Context, apiCmd *api.ApiCommand, msg *chatapi.Message, dbLab *dblabapi.Client, cloneID string) error {
	msg.Append("Resetting the state of the database...")

	// TODO(anatoly): "zfs rollback" deletes newer snapshots. Users will be able
	// to jump across snapshots if we solve it.
	if err := dbLab.ResetClone(ctx, cloneID); err != nil {
		log.Err("Reset:", err)
		return err
	}

	result := "The state of the database has been reset."
	apiCmd.Response = result

	if err := msg.Append(result); err != nil {
		log.Err("Reset:", err)
		return err
	}

	return nil
}
