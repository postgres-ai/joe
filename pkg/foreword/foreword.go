/*
2020 © Postgres.ai
*/

// Package foreword provides structures for building foreword messages.
package foreword

import (
	"context"
	"fmt"
	"time"

	"github.com/hako/durafmt"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/database-lab/v2/pkg/log"
	"gitlab.com/postgres-ai/database-lab/v2/pkg/util"
)

// MsgSessionForewordTpl provides a template of session foreword message.
const MsgSessionForewordTpl = "Say `help` to see the full list of commands.\n" +
	"Made with :hearts: by Postgres.ai. Bug reports, ideas, and merge requests are welcome: https://gitlab.com/postgres-ai/joe \n" +
	"```" + `
Session started: %s
Idle session timeout: %s
Postgres version: %s
Joe version: %s (%s)
Database: %s
Database size: %s
Database state at: %s (%s ago)
` + "```"

// Content defines data for a foreword message.
type Content struct {
	Duration   time.Duration
	SessionID  string
	AppVersion string
	Edition    string
	DBName     string
	DSA        string
	DSADiff    string
	DBSize     string
	DBVersion  string
}

// EnrichForewordInfo adds database details to foreword data.
func (f *Content) EnrichForewordInfo(ctx context.Context, db *pgxpool.Pool) error {
	r := db.QueryRow(ctx, "select current_setting('server_version'), pg_size_pretty(pg_database_size($1))", f.DBName)

	if err := r.Scan(&f.DBVersion, &f.DBSize); err != nil {
		return errors.Wrap(err, "failed to retrieve database meta info")
	}

	dsaTime, err := time.Parse(util.DataStateAtFormat, f.DSA)
	if err != nil {
		log.Err("failed to parse the 'data state at' timestamp of the database snapshot: ", err)
		return nil
	}

	f.DSADiff = durafmt.Parse(time.Since(dsaTime).Round(time.Minute)).String()

	return nil
}

// GetForeword returns a foreword message.
func (f *Content) GetForeword() string {
	duration := durafmt.Parse(f.Duration.Round(time.Minute))
	return fmt.Sprintf(MsgSessionForewordTpl, f.SessionID, duration, f.DBVersion, f.AppVersion, f.Edition,
		f.DBName, f.DBSize, f.DSA, f.DSADiff)
}
