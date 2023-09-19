/*
2019 © Postgres.ai
*/

package command

import (
	"context"
	"time"

	"gitlab.com/postgres-ai/database-lab/v2/pkg/client/dblabapi"
	"gitlab.com/postgres-ai/database-lab/v2/pkg/log"

	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/foreword"
	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/services/platform"
	"gitlab.com/postgres-ai/joe/pkg/services/usermanager"
	"gitlab.com/postgres-ai/joe/pkg/util"
)

// ResetSession provides a command to reset a Database Lab session.
func ResetSession(ctx context.Context, cmd *platform.Command, msg *models.Message, dbLab *dblabapi.Client,
	msgSvc connection.Messenger, session *usermanager.UserSession,
	appVersion, edition string) error {
	msg.AppendText("Resetting the state of the database...")
	msgSvc.UpdateText(msg)

	clone := session.Clone

	// TODO(anatoly): "zfs rollback" deletes newer snapshots. Users will be able
	// to jump across snapshots if we solve it.
	if err := dbLab.ResetClone(ctx, clone.ID); err != nil {
		log.Err("Reset:", err)
		return err
	}

	if session.CloneConnection != nil {
		if err := session.CloneConnection.Close(ctx); err != nil {
			log.Err("Failed to close user connection:", err)
		}
	}

	allIdleConnections := session.Pool.AcquireAllIdle(ctx)
	for _, idleConnection := range allIdleConnections {
		if err := idleConnection.Conn().Close(ctx); err != nil {
			log.Err("Failed to close idle connection: ", err)
		}

		idleConnection.Release()
	}

	cloneConn, err := session.Pool.Acquire(ctx)
	if err != nil {
		log.Err("failed to acquire database connection:", err)
	}

	if cloneConn != nil {
		session.CloneConnection = cloneConn.Conn()
	}

	fwData := &foreword.Content{
		SessionID:  clone.ID,
		Duration:   time.Duration(clone.Metadata.MaxIdleMinutes) * time.Minute,
		AppVersion: appVersion,
		Edition:    edition,
		DBName:     session.ConnParams.Name,
		DSA:        clone.Snapshot.DataStateAt,
		DBSize:     util.NA,
		DSADiff:    "-",
	}

	if err := fwData.EnrichForewordInfo(ctx, session.Pool); err != nil {
		return err
	}

	cmd.Response = "The state of the database has been reset."

	msg.AppendText(fwData.GetForeword())
	if err := msgSvc.UpdateText(msg); err != nil {
		log.Err("Reset:", err)
		return err
	}

	return nil
}
